package rag

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reModuleStart     = regexp.MustCompile(`(?i)^\s*module\s+([a-z_][a-z0-9_]*)`)
	reSubroutineStart = regexp.MustCompile(`(?i)^\s*subroutine\s+([a-z_][a-z0-9_]*)\s*\((.*)\)`)
	reFunctionStart   = regexp.MustCompile(`(?i)^\s*function\s+([a-z_][a-z0-9_]*)\s*\((.*)\)`)
	reEndUnit         = regexp.MustCompile(`(?i)^\s*end\s+(module|subroutine|function)\b`)
	reIntentDecl      = regexp.MustCompile(`(?i)\bintent\s*\(\s*(in|out|inout)\s*\)\s*::\s*(.*)$`)
)

type FortranChunker struct {
	MaxLinesPerChunk int
}

type chunkScope struct {
	t        ChunkType
	name     string
	params   []Parameter
	start    int
	headLine string
}

func NewFortranChunker(maxLines int) *FortranChunker {
	if maxLines <= 0 {
		maxLines = 180
	}
	return &FortranChunker{MaxLinesPerChunk: maxLines}
}

func (c *FortranChunker) ChunkFile(path string, src string) []Chunk {
	lines := strings.Split(src, "\n")
	var chunks []Chunk

	var stack []chunkScope

	for i, line := range lines {
		ln := i + 1
		trimmed := strings.TrimSpace(line)

		if m := reModuleStart.FindStringSubmatch(line); m != nil && !strings.HasPrefix(strings.ToLower(trimmed), "end ") {
			stack = append(stack, chunkScope{
				t:        ChunkTypeModule,
				name:     m[1],
				params:   nil,
				start:    ln,
				headLine: line,
			})
			continue
		}
		if m := reSubroutineStart.FindStringSubmatch(line); m != nil && !strings.HasPrefix(strings.ToLower(trimmed), "end ") {
			stack = append(stack, chunkScope{
				t:        ChunkTypeSubroutine,
				name:     m[1],
				params:   parseParams(m[2]),
				start:    ln,
				headLine: line,
			})
			continue
		}
		if m := reFunctionStart.FindStringSubmatch(line); m != nil && !strings.HasPrefix(strings.ToLower(trimmed), "end ") {
			stack = append(stack, chunkScope{
				t:        ChunkTypeFunction,
				name:     m[1],
				params:   parseParams(m[2]),
				start:    ln,
				headLine: line,
			})
			continue
		}

		// Attach INTENT metadata to nearest routine in stack.
		if m := reIntentDecl.FindStringSubmatch(line); m != nil {
			intent := strings.ToLower(strings.TrimSpace(m[1]))
			names := splitNames(m[2])
			for j := len(stack) - 1; j >= 0; j-- {
				if stack[j].t == ChunkTypeSubroutine || stack[j].t == ChunkTypeFunction {
					stack[j].params = mergeIntent(stack[j].params, names, intent)
					break
				}
			}
		}

		if reEndUnit.MatchString(line) && len(stack) > 0 {
			// Pop nearest opened unit.
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			chunk := buildChunkFromLines(path, lines, open, ln, c.MaxLinesPerChunk)
			chunks = append(chunks, chunk...)
		}
	}

	// Fallback: if nothing matched, return whole file in one chunk.
	if len(chunks) == 0 {
		ch := Chunk{
			ID:        fmt.Sprintf("%s#1-#%d", cleanPath(path), len(lines)),
			File:      cleanPath(path),
			StartLine: 1,
			EndLine:   len(lines),
			Name:      filepath.Base(path),
			Type:      ChunkTypeUnknown,
			Skills:    []string{"fortran", "legacy-code"},
			Code:      src,
		}
		ch.Frontmatter = BuildFrontmatter(ch)
		return []Chunk{ch}
	}
	return chunks
}

func buildChunkFromLines(path string, all []string, open chunkScope, endLine, maxLines int) []Chunk {
	if endLine < open.start {
		endLine = open.start
	}
	segment := strings.Join(all[open.start-1:endLine], "\n")
	base := Chunk{
		File:       cleanPath(path),
		StartLine:  open.start,
		EndLine:    endLine,
		Name:       open.name,
		Type:       open.t,
		Parameters: open.params,
		Skills:     skillTagsFor(open.t, open.name),
		Description: fmt.Sprintf(
			"Fortran %s %s extracted from source.",
			string(open.t), open.name,
		),
		Code: segment,
	}

	parts := splitChunkByLines(base, maxLines)
	for i := range parts {
		parts[i].ID = fmt.Sprintf("%s#%d-%d:%s", parts[i].File, parts[i].StartLine, parts[i].EndLine, parts[i].Name)
		parts[i].Frontmatter = BuildFrontmatter(parts[i])
	}
	return parts
}

func splitChunkByLines(c Chunk, maxLines int) []Chunk {
	if maxLines <= 0 {
		return []Chunk{c}
	}
	lines := strings.Split(c.Code, "\n")
	if len(lines) <= maxLines {
		return []Chunk{c}
	}
	var out []Chunk
	for i := 0; i < len(lines); i += maxLines {
		j := i + maxLines
		if j > len(lines) {
			j = len(lines)
		}
		start := c.StartLine + i
		end := c.StartLine + j - 1
		part := c
		part.StartLine = start
		part.EndLine = end
		part.Code = strings.Join(lines[i:j], "\n")
		part.Name = fmt.Sprintf("%s_part_%d", c.Name, len(out)+1)
		out = append(out, part)
	}
	return out
}

func parseParams(raw string) []Parameter {
	names := splitNames(raw)
	out := make([]Parameter, 0, len(names))
	for _, n := range names {
		out = append(out, Parameter{Name: n})
	}
	return out
}

func splitNames(raw string) []string {
	parts := strings.Split(raw, ",")
	var out []string
	for _, p := range parts {
		s := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(p, "&"), "&"))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func mergeIntent(params []Parameter, names []string, intent string) []Parameter {
	seen := map[string]int{}
	for i, p := range params {
		seen[strings.ToLower(p.Name)] = i
	}
	for _, n := range names {
		key := strings.ToLower(n)
		if idx, ok := seen[key]; ok {
			params[idx].Intent = intent
		} else {
			params = append(params, Parameter{Name: n, Intent: intent})
		}
	}
	return params
}

func skillTagsFor(t ChunkType, name string) []string {
	base := []string{"fortran", "legacy-code", "blas"}
	switch t {
	case ChunkTypeModule:
		return append(base, "module")
	case ChunkTypeSubroutine:
		return append(base, "subroutine")
	case ChunkTypeFunction:
		return append(base, "function")
	default:
		return base
	}
}

func cleanPath(p string) string {
	return strings.TrimPrefix(filepath.ToSlash(p), "./")
}
