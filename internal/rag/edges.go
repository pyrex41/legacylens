package rag

import (
	"regexp"
	"strings"
)

// Edge represents a relationship between two chunks.
type Edge struct {
	SrcID    string
	DstID    string
	Relation string // "contains", "calls", "documents"
}

var reCallStmt = regexp.MustCompile(`(?i)\bcall\s+([a-z_][a-z0-9_]*)`)

// ExtractCallTargets parses Fortran CALL statements and returns unique target
// names in lowercase.
func ExtractCallTargets(code string) []string {
	matches := reCallStmt.FindAllStringSubmatch(code, -1)
	seen := map[string]bool{}
	var targets []string
	for _, m := range matches {
		name := strings.ToLower(m[1])
		if !seen[name] {
			seen[name] = true
			targets = append(targets, name)
		}
	}
	return targets
}

// ExtractContainmentEdges finds module/program chunks whose line range
// encloses subroutines/functions in the same file.
func ExtractContainmentEdges(chunks []Chunk) []Edge {
	// Group chunks by file
	byFile := map[string][]Chunk{}
	for _, c := range chunks {
		byFile[c.File] = append(byFile[c.File], c)
	}

	var edges []Edge
	for _, fileChunks := range byFile {
		// Find containers (modules, programs)
		var containers []Chunk
		var children []Chunk
		for _, c := range fileChunks {
			switch c.Type {
			case ChunkTypeModule, ChunkTypeProgram:
				containers = append(containers, c)
			case ChunkTypeSubroutine, ChunkTypeFunction:
				children = append(children, c)
			}
		}
		for _, parent := range containers {
			for _, child := range children {
				if child.StartLine > parent.StartLine && child.EndLine <= parent.EndLine {
					edges = append(edges, Edge{
						SrcID:    parent.ID,
						DstID:    child.ID,
						Relation: "contains",
					})
				}
			}
		}
	}
	return edges
}

// ExtractCallEdges matches CALL targets in code chunks to chunks by name.
func ExtractCallEdges(chunks []Chunk, nameIndex map[string]string) []Edge {
	var edges []Edge
	for _, c := range chunks {
		if c.Type == ChunkTypeDoc {
			continue
		}
		targets := ExtractCallTargets(c.Code)
		for _, target := range targets {
			if dstID, ok := nameIndex[target]; ok && dstID != c.ID {
				edges = append(edges, Edge{
					SrcID:    c.ID,
					DstID:    dstID,
					Relation: "calls",
				})
			}
		}
	}
	return edges
}

// ExtractDocEdges links doc chunks to code chunks whose name (≥3 chars)
// appears in the doc text. Creates bidirectional edges.
func ExtractDocEdges(chunks []Chunk, nameIndex map[string]string) []Edge {
	var docs []Chunk
	for _, c := range chunks {
		if c.Type == ChunkTypeDoc {
			docs = append(docs, c)
		}
	}

	var edges []Edge
	for _, doc := range docs {
		text := strings.ToLower(doc.Code + " " + doc.Frontmatter + " " + doc.Description)
		for name, id := range nameIndex {
			if len(name) < 3 || id == doc.ID {
				continue
			}
			if strings.Contains(text, name) {
				edges = append(edges, Edge{
					SrcID:    doc.ID,
					DstID:    id,
					Relation: "documents",
				})
				edges = append(edges, Edge{
					SrcID:    id,
					DstID:    doc.ID,
					Relation: "documents",
				})
			}
		}
	}
	return edges
}

// BuildNameIndex creates a lowercase name → chunk ID mapping for code chunks.
func BuildNameIndex(chunks []Chunk) map[string]string {
	idx := make(map[string]string, len(chunks))
	for _, c := range chunks {
		if c.Type == ChunkTypeDoc || c.Name == "" {
			continue
		}
		name := strings.ToLower(c.Name)
		idx[name] = c.ID
	}
	return idx
}
