package rag

import (
	"fmt"
	"strings"
)

type ChunkType string

const (
	ChunkTypeModule     ChunkType = "module"
	ChunkTypeSubroutine ChunkType = "subroutine"
	ChunkTypeFunction   ChunkType = "function"
	ChunkTypeTypeDef    ChunkType = "type"
	ChunkTypeComment    ChunkType = "comment"
	ChunkTypeUnknown    ChunkType = "unknown"
)

type Parameter struct {
	Name        string
	Type        string
	Intent      string
	Description string
}

type Chunk struct {
	ID          string
	File        string
	StartLine   int
	EndLine     int
	Name        string
	Type        ChunkType
	Parameters  []Parameter
	Skills      []string
	Description string
	Frontmatter string
	Code        string
}

func (c Chunk) EmbeddingText() string {
	fm := strings.TrimSpace(c.Frontmatter)
	code := strings.TrimSpace(c.Code)
	if fm == "" {
		return code
	}
	if code == "" {
		return fm
	}
	return fm + "\n\n" + code
}

func (c Chunk) LocationRef() string {
	return fmt.Sprintf("%s:%d-%d", c.File, c.StartLine, c.EndLine)
}

type SearchResult struct {
	Chunk        Chunk
	VectorScore  float64
	KeywordScore float64
	HybridScore  float64
}
