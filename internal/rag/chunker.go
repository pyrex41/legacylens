package rag

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// Module: "module foo" but not "module procedure"
	reModuleStart = regexp.MustCompile(`(?i)^\s*module\s+([a-z_][a-z0-9_]*)`)
	// Program: "program main"
	reProgramStart = regexp.MustCompile(`(?i)^\s*program\s+([a-z_][a-z0-9_]*)`)
	// Subroutine with optional prefixes: "[pure|elemental|recursive] subroutine foo(a,b)"
	reSubroutineStart = regexp.MustCompile(`(?i)^\s*(?:(?:pure|elemental|recursive|impure)\s+)*subroutine\s+([a-z_][a-z0-9_]*)\s*\(([^)]*)\)`)
	// Subroutine with no args: "[pure] subroutine foo"
	reSubroutineNoArgs = regexp.MustCompile(`(?i)^\s*(?:(?:pure|elemental|recursive|impure)\s+)*subroutine\s+([a-z_][a-z0-9_]*)\s*$`)
	// Function with optional type prefix and qualifiers:
	// "[real|integer|...] [pure|elemental|recursive] function foo(x)"
	// Also handles "double precision function foo(x)"
	reFunctionStart = regexp.MustCompile(`(?i)^\s*(?:(?:integer|real|double\s+precision|complex|logical|character)\s+)?(?:(?:pure|elemental|recursive|impure)\s+)*function\s+([a-z_][a-z0-9_]*)\s*\(([^)]*)\)`)
	// Function with no args: "function foo()" or "real function foo"
	reFunctionNoArgs = regexp.MustCompile(`(?i)^\s*(?:(?:integer|real|double\s+precision|complex|logical|character)\s+)?(?:(?:pure|elemental|recursive|impure)\s+)*function\s+([a-z_][a-z0-9_]*)\s*(?:\(\s*\))?\s*$`)
	// Interface block: "interface [name]" or "abstract interface"
	reInterfaceStart = regexp.MustCompile(`(?i)^\s*(?:abstract\s+)?interface\b\s*([a-z_][a-z0-9_]*)?`)
	// Type definition: "type [:: name]" or "type, extends(base) :: name"
	reTypeDefStart = regexp.MustCompile(`(?i)^\s*type\b(?:\s*,\s*[^:]+)?(?:\s*::\s*)([a-z_][a-z0-9_]*)`)
	// Plain "type name" without ::
	reTypeDefPlain = regexp.MustCompile(`(?i)^\s*type\s+([a-z_][a-z0-9_]*)\s*$`)
	// End statements: "end [keyword [name]]" or bare "end"
	reEndUnit = regexp.MustCompile(`(?i)^\s*end\s*(?:module|subroutine|function|program|interface|type)\b`)
	reBareEnd = regexp.MustCompile(`(?i)^\s*end\s*$`)
	// Intent declarations for parameter metadata
	reIntentDecl = regexp.MustCompile(`(?i)\bintent\s*\(\s*(in|out|inout)\s*\)\s*(?:.*?::\s*)(.*)$`)
	// Type declarations: "real :: x", "integer, intent(in) :: n"
	reTypeDecl = regexp.MustCompile(`(?i)^\s*(integer|real|double\s+precision|complex|logical|character)(?:\s*\([^)]*\))?\s*(?:,\s*[^:]+)*\s*::\s*(.+)$`)
	// Continuation line marker
	reContinuation = regexp.MustCompile(`&\s*$`)
	// "module procedure" is not a module start
	reModuleProcedure = regexp.MustCompile(`(?i)^\s*module\s+procedure\b`)
	// "type(" is a type-cast/constructor, not a type definition
	reTypeGuard = regexp.MustCompile(`(?i)^\s*type\s*\(`)
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
	rawLines := strings.Split(src, "\n")
	lines := joinContinuationLines(rawLines)
	var chunks []Chunk
	var stack []chunkScope

	for i, line := range lines {
		ln := i + 1
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		if strings.HasPrefix(lower, "end ") || lower == "end" {
			goto checkEnd
		}

		if reModuleProcedure.MatchString(line) {
			goto checkIntent
		}

		if m := reProgramStart.FindStringSubmatch(line); m != nil {
			stack = append(stack, chunkScope{
				t: ChunkTypeProgram, name: m[1], start: ln, headLine: line,
			})
			continue
		}

		if m := reModuleStart.FindStringSubmatch(line); m != nil {
			stack = append(stack, chunkScope{
				t: ChunkTypeModule, name: m[1], start: ln, headLine: line,
			})
			continue
		}

		if m := reSubroutineStart.FindStringSubmatch(line); m != nil {
			stack = append(stack, chunkScope{
				t: ChunkTypeSubroutine, name: m[1], params: parseParams(m[2]),
				start: ln, headLine: line,
			})
			continue
		}
		if m := reSubroutineNoArgs.FindStringSubmatch(line); m != nil {
			stack = append(stack, chunkScope{
				t: ChunkTypeSubroutine, name: m[1],
				start: ln, headLine: line,
			})
			continue
		}

		if m := reFunctionStart.FindStringSubmatch(line); m != nil {
			stack = append(stack, chunkScope{
				t: ChunkTypeFunction, name: m[1], params: parseParams(m[2]),
				start: ln, headLine: line,
			})
			continue
		}
		if m := reFunctionNoArgs.FindStringSubmatch(line); m != nil {
			stack = append(stack, chunkScope{
				t: ChunkTypeFunction, name: m[1],
				start: ln, headLine: line,
			})
			continue
		}

		if !reTypeGuard.MatchString(line) {
			if m := reTypeDefStart.FindStringSubmatch(line); m != nil {
				stack = append(stack, chunkScope{
					t: ChunkTypeDef, name: m[1],
					start: ln, headLine: line,
				})
				continue
			}
			if m := reTypeDefPlain.FindStringSubmatch(line); m != nil {
				stack = append(stack, chunkScope{
					t: ChunkTypeDef, name: m[1],
					start: ln, headLine: line,
				})
				continue
			}
		}

		if m := reInterfaceStart.FindStringSubmatch(line); m != nil {
			name := "interface"
			if m[1] != "" {
				name = m[1]
			}
			stack = append(stack, chunkScope{
				t: ChunkTypeInterface, name: name,
				start: ln, headLine: line,
			})
			continue
		}

	checkIntent:
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

		if m := reTypeDecl.FindStringSubmatch(line); m != nil {
			typeName := strings.TrimSpace(m[1])
			varNames := splitNames(m[2])
			for j := len(stack) - 1; j >= 0; j-- {
				if stack[j].t == ChunkTypeSubroutine || stack[j].t == ChunkTypeFunction {
					stack[j].params = mergeType(stack[j].params, varNames, typeName)
					break
				}
			}
		}
		continue

	checkEnd:
		if reEndUnit.MatchString(line) && len(stack) > 0 {
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			chunk := buildChunkFromLines(path, lines, open, ln, c.MaxLinesPerChunk)
			chunks = append(chunks, chunk...)
		} else if reBareEnd.MatchString(line) && len(stack) > 0 {
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			chunk := buildChunkFromLines(path, lines, open, ln, c.MaxLinesPerChunk)
			chunks = append(chunks, chunk...)
		}
	}

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

// joinContinuationLines merges Fortran continuation lines (trailing &) into
// single logical lines, preserving line count by inserting empty lines for
// consumed continuations. This ensures line numbers remain stable for chunk
// boundaries and provenance tracking.
func joinContinuationLines(raw []string) []string {
	out := make([]string, 0, len(raw))
	i := 0
	for i < len(raw) {
		line := raw[i]
		if reContinuation.MatchString(line) {
			merged := strings.TrimRight(line, " \t")
			merged = strings.TrimSuffix(merged, "&")
			i++
			for i < len(raw) {
				next := strings.TrimSpace(raw[i])
				next = strings.TrimPrefix(next, "&")
				merged += " " + next
				if !reContinuation.MatchString(raw[i]) {
					i++
					break
				}
				merged = strings.TrimRight(merged, " \t")
				merged = strings.TrimSuffix(merged, "&")
				i++
			}
			out = append(out, merged)
			for len(out) < i {
				out = append(out, "")
			}
		} else {
			out = append(out, line)
			i++
		}
	}
	return out
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
		Description: extractDescription(segment, open.t, open.name),
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

func mergeType(params []Parameter, names []string, typeName string) []Parameter {
	seen := map[string]int{}
	for i, p := range params {
		seen[strings.ToLower(p.Name)] = i
	}
	for _, n := range names {
		clean := strings.TrimSpace(n)
		if eqIdx := strings.Index(clean, "="); eqIdx >= 0 {
			clean = strings.TrimSpace(clean[:eqIdx])
		}
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if idx, ok := seen[key]; ok {
			params[idx].Type = typeName
		}
	}
	return params
}

func skillTagsFor(t ChunkType, name string) []string {
	base := []string{"fortran", "legacy-code", "blas"}
	switch t {
	case ChunkTypeModule:
		return append(base, "module")
	case ChunkTypeProgram:
		return append(base, "program")
	case ChunkTypeSubroutine:
		return append(base, "subroutine")
	case ChunkTypeFunction:
		return append(base, "function")
	case ChunkTypeDef:
		return append(base, "type-definition")
	case ChunkTypeInterface:
		return append(base, "interface")
	default:
		return base
	}
}

// extractDescription pulls the leading comment block from a chunk's code.
// BLAS routines typically have header comments like "!> SGEMV performs ...".
// Falls back to a generic description if no comments found.
func extractDescription(code string, t ChunkType, name string) string {
	lines := strings.Split(code, "\n")
	var comment []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(comment) > 0 {
				break // blank line after comments ends the block
			}
			continue
		}
		if strings.HasPrefix(trimmed, "!>") {
			text := strings.TrimSpace(strings.TrimPrefix(trimmed, "!>"))
			if text != "" {
				comment = append(comment, text)
			}
		} else if strings.HasPrefix(trimmed, "!") && !strings.HasPrefix(trimmed, "!$") {
			text := strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
			if text != "" {
				comment = append(comment, text)
			}
		} else {
			if len(comment) > 0 {
				break // non-comment line after comments
			}
			// Skip non-comment lines at start (subroutine declaration etc.)
		}
	}
	if len(comment) > 0 {
		// Cap at 3 lines to keep it concise
		if len(comment) > 3 {
			comment = comment[:3]
		}
		return strings.Join(comment, " ")
	}
	return fmt.Sprintf("Fortran %s %s extracted from source.", string(t), name)
}

func cleanPath(p string) string {
	return strings.TrimPrefix(filepath.ToSlash(p), "./")
}
