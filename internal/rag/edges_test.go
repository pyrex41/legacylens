package rag

import (
	"sort"
	"testing"
)

func TestExtractCallTargets(t *testing.T) {
	code := `
      subroutine sgemv(trans, m, n)
        call xerbla('SGEMV', info)
        call slassq(n, x, incx, scale, ssq)
        call XERBLA('SGEMV', info)
      end subroutine sgemv
`
	targets := ExtractCallTargets(code)
	sort.Strings(targets)

	if len(targets) != 2 {
		t.Fatalf("expected 2 unique targets, got %d: %v", len(targets), targets)
	}
	if targets[0] != "slassq" || targets[1] != "xerbla" {
		t.Fatalf("unexpected targets: %v", targets)
	}
}

func TestExtractCallTargetsEmpty(t *testing.T) {
	targets := ExtractCallTargets("! just a comment, no calls")
	if len(targets) != 0 {
		t.Fatalf("expected 0 targets, got %d", len(targets))
	}
}

func TestExtractContainmentEdges(t *testing.T) {
	chunks := []Chunk{
		{ID: "mod1", File: "a.f90", StartLine: 1, EndLine: 50, Name: "blas_module", Type: ChunkTypeModule},
		{ID: "sub1", File: "a.f90", StartLine: 5, EndLine: 20, Name: "sgemv", Type: ChunkTypeSubroutine},
		{ID: "sub2", File: "a.f90", StartLine: 25, EndLine: 45, Name: "dgemv", Type: ChunkTypeSubroutine},
		{ID: "sub3", File: "b.f90", StartLine: 1, EndLine: 10, Name: "other", Type: ChunkTypeSubroutine}, // different file
	}

	edges := ExtractContainmentEdges(chunks)
	if len(edges) != 2 {
		t.Fatalf("expected 2 containment edges, got %d: %+v", len(edges), edges)
	}
	for _, e := range edges {
		if e.SrcID != "mod1" {
			t.Fatalf("expected parent mod1, got %s", e.SrcID)
		}
		if e.Relation != "contains" {
			t.Fatalf("expected 'contains' relation, got %s", e.Relation)
		}
	}
}

func TestExtractDocEdges(t *testing.T) {
	chunks := []Chunk{
		{ID: "doc1", File: "readme.md", StartLine: 1, EndLine: 10, Name: "readme",
			Type: ChunkTypeDoc, Code: "The xerbla routine handles error reporting for BLAS"},
		{ID: "code1", File: "a.f90", StartLine: 1, EndLine: 20, Name: "xerbla",
			Type: ChunkTypeSubroutine, Code: "subroutine xerbla(srname, info)"},
		{ID: "code2", File: "b.f90", StartLine: 1, EndLine: 10, Name: "ab",
			Type: ChunkTypeFunction, Code: "function ab()"}, // name too short (2 chars)
	}

	nameIndex := BuildNameIndex(chunks)
	edges := ExtractDocEdges(chunks, nameIndex)

	// Should create bidirectional doc↔xerbla edges, skip "ab" (< 3 chars)
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges (bidirectional), got %d: %+v", len(edges), edges)
	}

	hasDocToCode := false
	hasCodeToDoc := false
	for _, e := range edges {
		if e.SrcID == "doc1" && e.DstID == "code1" {
			hasDocToCode = true
		}
		if e.SrcID == "code1" && e.DstID == "doc1" {
			hasCodeToDoc = true
		}
	}
	if !hasDocToCode || !hasCodeToDoc {
		t.Fatalf("expected bidirectional edges between doc1 and code1, got: %+v", edges)
	}
}

func TestBuildNameIndex(t *testing.T) {
	chunks := []Chunk{
		{ID: "a", Name: "Sgemv", Type: ChunkTypeSubroutine},
		{ID: "b", Name: "DGEMM", Type: ChunkTypeFunction},
		{ID: "c", Name: "", Type: ChunkTypeUnknown},    // no name
		{ID: "d", Name: "readme", Type: ChunkTypeDoc},  // doc excluded
	}

	idx := BuildNameIndex(chunks)
	if len(idx) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(idx), idx)
	}
	if idx["sgemv"] != "a" {
		t.Fatalf("expected sgemv→a, got %s", idx["sgemv"])
	}
	if idx["dgemm"] != "b" {
		t.Fatalf("expected dgemm→b, got %s", idx["dgemm"])
	}
}
