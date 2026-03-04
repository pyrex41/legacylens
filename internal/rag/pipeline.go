package rag

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type PipelineConfig struct {
	Workers        int
	QueueSize      int
	EnqueueTimeout time.Duration
	MaxLinesChunk  int
}

func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		Workers:        2,
		QueueSize:      256,
		EnqueueTimeout: 5 * time.Minute,
		MaxLinesChunk:  180,
	}
}

type Pipeline struct {
	cfg       PipelineConfig
	chunker   *FortranChunker
	mdChunker *MarkdownChunker
	embed     Embedder
	store     VectorStore
}

func NewPipeline(cfg PipelineConfig, embed Embedder, store VectorStore) *Pipeline {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 8
	}
	if cfg.EnqueueTimeout <= 0 {
		cfg.EnqueueTimeout = 2 * time.Second
	}
	if cfg.MaxLinesChunk <= 0 {
		cfg.MaxLinesChunk = 180
	}
	return &Pipeline{
		cfg:       cfg,
		chunker:   NewFortranChunker(cfg.MaxLinesChunk),
		mdChunker: NewMarkdownChunker(cfg.MaxLinesChunk),
		embed:     embed,
		store:     store,
	}
}

func (p *Pipeline) IngestRepo(ctx context.Context, repoPath string) (int, error) {
	if strings.TrimSpace(repoPath) == "" {
		return 0, errors.New("repoPath is required")
	}
	if err := p.store.Init(ctx); err != nil {
		return 0, err
	}
	f90Files, err := listFortranFiles(repoPath)
	if err != nil {
		return 0, err
	}
	mdFiles, err := listMarkdownFiles(repoPath)
	if err != nil {
		return 0, err
	}
	if len(f90Files) == 0 && len(mdFiles) == 0 {
		return 0, fmt.Errorf("no .f90 or .md files found under %s", repoPath)
	}

	type ingestJob struct {
		path string
		md   bool
	}
	var jobs []ingestJob
	for _, fp := range f90Files {
		jobs = append(jobs, ingestJob{path: fp})
	}
	for _, fp := range mdFiles {
		jobs = append(jobs, ingestJob{path: fp, md: true})
	}

	ch := make(chan Chunk, p.cfg.QueueSize)
	errCh := make(chan error, 1)

	var wg sync.WaitGroup
	for i := 0; i < p.cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range ch {
				select {
				case <-ctx.Done():
					return
				default:
				}
				vec := p.embed.Embed(c.EmbeddingText())
				if err := p.store.Upsert(ctx, c, vec); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		}()
	}

	count := 0
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			close(ch)
			wg.Wait()
			return count, ctx.Err()
		default:
		}
		raw, err := os.ReadFile(job.path)
		if err != nil {
			close(ch)
			wg.Wait()
			return count, err
		}
		var chunks []Chunk
		if job.md {
			chunks = p.mdChunker.ChunkFile(job.path, string(raw))
		} else {
			chunks = p.chunker.ChunkFile(job.path, string(raw))
		}
		for _, ck := range chunks {
			timer := time.NewTimer(p.cfg.EnqueueTimeout)
			select {
			case <-ctx.Done():
				timer.Stop()
				close(ch)
				wg.Wait()
				return count, ctx.Err()
			case err := <-errCh:
				timer.Stop()
				close(ch)
				wg.Wait()
				return count, err
			case ch <- ck:
				timer.Stop()
				count++
			case <-timer.C:
				close(ch)
				wg.Wait()
				return count, fmt.Errorf("backpressure timeout enqueuing chunk %s", ck.ID)
			}
		}
	}
	close(ch)
	wg.Wait()

	select {
	case err := <-errCh:
		return count, err
	default:
	}

	// Build graph edges if the store supports it
	if es, ok := p.store.(EdgeStore); ok {
		if err := es.BuildEdges(ctx); err != nil {
			return count, fmt.Errorf("build edges: %w", err)
		}
	}

	return count, nil
}

type QueryEngine struct {
	store VectorStore
	embed Embedder
	llm   LLMClient
}

func NewQueryEngine(store VectorStore, embed Embedder) *QueryEngine {
	return &QueryEngine{store: store, embed: embed}
}

// WithLLM returns a copy of the QueryEngine with the given LLM client.
func (q *QueryEngine) WithLLM(llm LLMClient) *QueryEngine {
	return &QueryEngine{store: q.store, embed: q.embed, llm: llm}
}

func (q *QueryEngine) Search(ctx context.Context, text string, k int) ([]SearchResult, error) {
	return q.store.HybridSearch(ctx, text, q.embed.Embed(text), k)
}

func listFortranFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".f90") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dedupFiles(files)
}

func listMarkdownFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// dedupFiles removes files with identical content, keeping the one with the
// shorter path. This handles repos like M_blas where src/ and docs/fpm-ford/src/
// contain the same .f90 files.
func dedupFiles(files []string) ([]string, error) {
	type entry struct {
		path string
		hash string
	}
	entries := make([]entry, 0, len(files))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		h := fmt.Sprintf("%x", sha256.Sum256(raw))
		entries = append(entries, entry{path: f, hash: h})
	}
	seen := map[string]string{} // hash → shortest path
	for _, e := range entries {
		if existing, ok := seen[e.hash]; ok {
			if len(e.path) < len(existing) {
				seen[e.hash] = e.path
			}
		} else {
			seen[e.hash] = e.path
		}
	}
	kept := make(map[string]bool, len(seen))
	for _, p := range seen {
		kept[p] = true
	}
	var deduped []string
	for _, e := range entries {
		if kept[e.path] {
			deduped = append(deduped, e.path)
			delete(kept, e.path) // only add once
		}
	}
	return deduped, nil
}
