package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"legacylens/internal/rag"
)

func main() {
	repo := flag.String("repo", "", "Path to Fortran repository (e.g. M_blas)")
	backend := flag.String("backend", "sqlite", "Backend: sqlite|cozo")
	query := flag.String("query", "", "Natural language query")
	topK := flag.Int("k", 5, "Top K results")
	timeout := flag.Duration("timeout", 30*time.Second, "End-to-end timeout")
	flag.Parse()

	if *repo == "" {
		fmt.Fprintln(os.Stderr, "-repo is required")
		os.Exit(2)
	}
	if *query == "" {
		fmt.Fprintln(os.Stderr, "-query is required")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	embedder := rag.NewHashEmbedder(384)
	store := mustStore(*backend, embedder.Dimension())
	pipeline := rag.NewPipeline(rag.DefaultPipelineConfig(), embedder, store)
	engine := rag.NewQueryEngine(store, embedder)

	start := time.Now()
	n, err := pipeline.IngestRepo(ctx, *repo)
	if err != nil {
		log.Fatalf("ingestion failed: %v", err)
	}
	ingestDur := time.Since(start)

	qStart := time.Now()
	results, err := engine.Search(ctx, *query, *topK)
	if err != nil {
		log.Fatalf("query failed: %v", err)
	}
	queryDur := time.Since(qStart)

	fmt.Printf("backend=%s chunks=%d ingest=%s query=%s\n", store.Name(), n, ingestDur, queryDur)
	for i, r := range results {
		fmt.Printf(
			"%d) %s | vec=%.4f key=%.4f hybrid=%.6f\n",
			i+1, r.Chunk.LocationRef(), r.VectorScore, r.KeywordScore, r.HybridScore,
		)
		fmt.Println(r.Chunk.Frontmatter)
		fmt.Println("---")
	}
}

func mustStore(name string, dim int) rag.VectorStore {
	switch name {
	case "sqlite":
		return rag.NewSQLiteStore(dim)
	case "cozo":
		return rag.NewCozoStore(dim)
	default:
		log.Fatalf("unsupported backend: %s", name)
		return nil
	}
}
