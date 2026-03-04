package rag

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPipelineIngestAndSearch(t *testing.T) {
	root := t.TempDir()
	src := `module mm
contains
subroutine saxpy(a,x,y)
  real, intent(in) :: a
  real, intent(in) :: x
  real, intent(out) :: y
end subroutine saxpy
end module mm`
	if err := os.WriteFile(filepath.Join(root, "m.f90"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	embed := NewHashEmbedder(384)
	store := NewProductionSQLiteStore(embed.Dimension(), ":memory:")
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("init store: %v", err)
	}
	defer store.Close()
	cfg := DefaultPipelineConfig()
	cfg.Workers = 2
	cfg.QueueSize = 4
	cfg.EnqueueTimeout = 200 * time.Millisecond
	p := NewPipeline(cfg, embed, store)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n, err := p.IngestRepo(ctx, root)
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected chunks > 0")
	}

	engine := NewQueryEngine(store, embed)
	res, err := engine.Search(ctx, "how does saxpy work", 3)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(res) == 0 {
		t.Fatalf("expected at least one result")
	}
}
