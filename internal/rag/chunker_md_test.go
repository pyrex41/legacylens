package rag

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestMarkdownChunker_BasicSections(t *testing.T) {
	src := `---
tags: [blas, fortran]
topic: "BLAS overview"
---

# Main Title

Some intro text.

## Architecture

The architecture is modular.

## Testing

Tests use fpm test.
`
	c := NewMarkdownChunker(80)
	chunks := c.ChunkFile("docs/overview.md", src)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Name != "Architecture" {
		t.Errorf("chunk 0 name = %q, want Architecture", chunks[0].Name)
	}
	if chunks[1].Name != "Testing" {
		t.Errorf("chunk 1 name = %q, want Testing", chunks[1].Name)
	}
	for _, ch := range chunks {
		if ch.Type != ChunkTypeDoc {
			t.Errorf("chunk %q type = %q, want doc", ch.Name, ch.Type)
		}
	}
}

func TestMarkdownChunker_FrontmatterTags(t *testing.T) {
	src := `---
tags: [blas, fortran]
---

## Section One

Content here.
`
	c := NewMarkdownChunker(80)
	chunks := c.ChunkFile("test.md", src)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	skills := chunks[0].Skills
	has := map[string]bool{}
	for _, s := range skills {
		has[s] = true
	}
	if !has["blas"] {
		t.Error("missing skill 'blas'")
	}
	if !has["fortran"] {
		t.Error("missing skill 'fortran'")
	}
	if !has["doc"] {
		t.Error("missing skill 'doc'")
	}
	if !has["markdown"] {
		t.Error("missing skill 'markdown'")
	}
}

func TestMarkdownChunker_DeepSplit(t *testing.T) {
	// Build a large H2 section with H3 subsections.
	var b strings.Builder
	b.WriteString("## Big Section\n\n")
	b.WriteString("### Sub A\n\n")
	for i := 0; i < 50; i++ {
		b.WriteString(fmt.Sprintf("Line %d of sub A.\n", i))
	}
	b.WriteString("\n### Sub B\n\n")
	for i := 0; i < 50; i++ {
		b.WriteString(fmt.Sprintf("Line %d of sub B.\n", i))
	}

	c := NewMarkdownChunker(80) // H2 section >80 lines, so should split at H3
	chunks := c.ChunkFile("deep.md", b.String())
	if len(chunks) < 2 {
		t.Fatalf("expected >=2 chunks from deep split, got %d", len(chunks))
	}
	foundA, foundB := false, false
	for _, ch := range chunks {
		if ch.Name == "Sub A" {
			foundA = true
		}
		if ch.Name == "Sub B" {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Errorf("expected Sub A and Sub B chunks, foundA=%v foundB=%v", foundA, foundB)
	}
}

func TestMarkdownChunker_ShortSectionStaysWhole(t *testing.T) {
	src := `## Short Section

### Sub 1

A little content.

### Sub 2

More content.
`
	c := NewMarkdownChunker(80)
	chunks := c.ChunkFile("short.md", src)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for short section, got %d", len(chunks))
	}
	if chunks[0].Name != "Short Section" {
		t.Errorf("name = %q, want Short Section", chunks[0].Name)
	}
}

func TestMarkdownChunker_NoHeadingsFallback(t *testing.T) {
	src := `Just some plain text.

No headings here at all.
More lines.
`
	c := NewMarkdownChunker(80)
	chunks := c.ChunkFile("plain.md", src)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Type != ChunkTypeDoc {
		t.Errorf("type = %q, want doc", chunks[0].Type)
	}
	if chunks[0].Name != "plain" {
		t.Errorf("name = %q, want 'plain' (filename stem)", chunks[0].Name)
	}
}

func TestMarkdownChunker_IDFormat(t *testing.T) {
	src := `## Build Systems

FPM is the primary build tool.
`
	c := NewMarkdownChunker(80)
	chunks := c.ChunkFile("docs/overview.md", src)
	if len(chunks) != 1 {
		t.Fatal("expected 1 chunk")
	}
	// ID should match path#start-end:slug
	re := regexp.MustCompile(`^.+#\d+-\d+:[a-z0-9-]+$`)
	if !re.MatchString(chunks[0].ID) {
		t.Errorf("ID %q does not match expected pattern", chunks[0].ID)
	}
}

func TestMarkdownChunker_TopicInDescription(t *testing.T) {
	src := `---
topic: "What is BLAS?"
---

## Overview

BLAS is a set of routines.
`
	c := NewMarkdownChunker(80)
	chunks := c.ChunkFile("test.md", src)
	if len(chunks) != 1 {
		t.Fatal("expected 1 chunk")
	}
	if !strings.Contains(chunks[0].Description, "What is BLAS?") {
		t.Errorf("description %q should contain topic", chunks[0].Description)
	}
}
