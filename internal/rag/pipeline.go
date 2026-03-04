package rag

import (
	"context"
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
		Workers:        4,
		QueueSize:      64,
		EnqueueTimeout: 2 * time.Second,
		MaxLinesChunk:  180,
	}
}

type Pipeline struct {
	cfg     PipelineConfig
	chunker *FortranChunker
	embed   Embedder
	store   VectorStore
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
		cfg:     cfg,
		chunker: NewFortranChunker(cfg.MaxLinesChunk),
		embed:   embed,
		store:   store,
	}
}

func (p *Pipeline) IngestRepo(ctx context.Context, repoPath string) (int, error) {
	if strings.TrimSpace(repoPath) == "" {
		return 0, errors.New("repoPath is required")
	}
	if err := p.store.Init(ctx); err != nil {
		return 0, err
	}
	files, err := listFortranFiles(repoPath)
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, fmt.Errorf("no .f90 files found under %s", repoPath)
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
	for _, fp := range files {
		select {
		case <-ctx.Done():
			close(ch)
			wg.Wait()
			return count, ctx.Err()
		default:
		}
		raw, err := os.ReadFile(fp)
		if err != nil {
			close(ch)
			wg.Wait()
			return count, err
		}
		chunks := p.chunker.ChunkFile(fp, string(raw))
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
	return count, nil
}

type QueryEngine struct {
	store VectorStore
	embed Embedder
}

func NewQueryEngine(store VectorStore, embed Embedder) *QueryEngine {
	return &QueryEngine{store: store, embed: embed}
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
	return files, err
}
