package rag

import (
	"context"
	"fmt"
	"strings"
)

// ExplainResult holds a structured answer synthesized from retrieved chunks.
type ExplainResult struct {
	Query      string          `json:"query"`
	Answer     string          `json:"answer"`
	Citations  []Citation      `json:"citations"`
	Symbols    []SymbolExplain `json:"symbols"`
	RawResults []SearchResult  `json:"raw_results"`
}

// Citation references a specific file and line range backing a claim.
type Citation struct {
	Ref    string `json:"ref"`
	File   string `json:"file"`
	Start  int    `json:"start_line"`
	End    int    `json:"end_line"`
	Symbol string `json:"symbol"`
}

// SymbolExplain provides per-symbol explanation with skills-style frontmatter.
type SymbolExplain struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	File        string `json:"file"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	Frontmatter string `json:"frontmatter"`
	Explanation string `json:"explanation"`
}

// Explain retrieves relevant chunks and synthesizes a grounded answer with
// citations and per-symbol frontmatter. If an LLMClient is configured on the
// QueryEngine, it uses the LLM for synthesis; otherwise falls back to
// deterministic template-based synthesis.
func (q *QueryEngine) Explain(ctx context.Context, query string, k int) (*ExplainResult, error) {
	results, err := q.Search(ctx, query, k)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	if len(results) == 0 {
		return &ExplainResult{
			Query:  query,
			Answer: "No relevant code found for the given query.",
		}, nil
	}

	citations := buildCitations(results)
	symbols := buildSymbolExplanations(results)

	var answer string
	if q.llm != nil {
		prompt := buildLLMPrompt(query, results)
		llmAnswer, err := q.llm.Complete(ctx, prompt)
		if err != nil {
			// Fall back to template synthesis on LLM error
			answer = synthesizeAnswer(query, results, citations)
		} else {
			answer = llmAnswer
		}
	} else {
		answer = synthesizeAnswer(query, results, citations)
	}

	return &ExplainResult{
		Query:      query,
		Answer:     answer,
		Citations:  citations,
		Symbols:    symbols,
		RawResults: results,
	}, nil
}

func buildLLMPrompt(query string, results []SearchResult) string {
	var b strings.Builder
	b.WriteString("You are a Fortran code expert analyzing M_blas. Given these code chunks, answer the question.\n\n")

	for i, r := range results {
		fmt.Fprintf(&b, "--- Chunk %d ---\n", i+1)
		fmt.Fprintf(&b, "Name: %s\n", r.Chunk.Name)
		fmt.Fprintf(&b, "Type: %s\n", r.Chunk.Type)
		fmt.Fprintf(&b, "Location: %s\n", r.Chunk.LocationRef())
		if r.Chunk.Frontmatter != "" {
			fmt.Fprintf(&b, "Frontmatter:\n%s\n", r.Chunk.Frontmatter)
		}
		if r.Chunk.Code != "" {
			fmt.Fprintf(&b, "Code:\n%s\n", r.Chunk.Code)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "Question: %s\n\n", query)
	b.WriteString("Cite code locations as file:startLine-endLine.")

	return b.String()
}

func buildCitations(results []SearchResult) []Citation {
	seen := map[string]bool{}
	var out []Citation
	for _, r := range results {
		ref := r.Chunk.LocationRef()
		if seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, Citation{
			Ref:    ref,
			File:   r.Chunk.File,
			Start:  r.Chunk.StartLine,
			End:    r.Chunk.EndLine,
			Symbol: r.Chunk.Name,
		})
	}
	return out
}

func buildSymbolExplanations(results []SearchResult) []SymbolExplain {
	seen := map[string]bool{}
	var out []SymbolExplain
	for _, r := range results {
		key := r.Chunk.Name + "|" + r.Chunk.File
		if seen[key] {
			continue
		}
		seen[key] = true

		explanation := describeChunk(r.Chunk)

		out = append(out, SymbolExplain{
			Name:        r.Chunk.Name,
			Type:        string(r.Chunk.Type),
			File:        r.Chunk.File,
			StartLine:   r.Chunk.StartLine,
			EndLine:     r.Chunk.EndLine,
			Frontmatter: r.Chunk.Frontmatter,
			Explanation: explanation,
		})
	}
	return out
}

func describeChunk(c Chunk) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s `%s` is a Fortran %s", capitalize(string(c.Type)), c.Name, c.Type)
	if c.File != "" {
		fmt.Fprintf(&b, " defined in `%s` (lines %d–%d)", c.File, c.StartLine, c.EndLine)
	}
	b.WriteString(".")

	if len(c.Parameters) > 0 {
		b.WriteString(" It accepts the following parameters: ")
		params := make([]string, 0, len(c.Parameters))
		for _, p := range c.Parameters {
			desc := p.Name
			if p.Type != "" {
				desc += " (" + p.Type + ")"
			}
			if p.Intent != "" {
				desc += " [" + p.Intent + "]"
			}
			params = append(params, desc)
		}
		b.WriteString(strings.Join(params, ", "))
		b.WriteString(".")
	}

	if len(c.Skills) > 0 {
		b.WriteString(" Skills: ")
		b.WriteString(strings.Join(c.Skills, ", "))
		b.WriteString(".")
	}

	return b.String()
}

func synthesizeAnswer(query string, results []SearchResult, citations []Citation) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Answer for: %s\n\n", query)

	b.WriteString("Based on the retrieved code, here are the relevant findings:\n\n")

	for i, r := range results {
		ref := r.Chunk.LocationRef()
		fmt.Fprintf(&b, "%d. **%s** (`%s`, %s)", i+1, r.Chunk.Name, r.Chunk.Type, ref)

		if r.HybridScore > 0 {
			fmt.Fprintf(&b, " — relevance: %.4f", r.HybridScore)
		}
		b.WriteString("\n")

		explanation := describeChunk(r.Chunk)
		fmt.Fprintf(&b, "   %s\n\n", explanation)
	}

	if len(citations) > 0 {
		b.WriteString("### Citations\n\n")
		for i, c := range citations {
			fmt.Fprintf(&b, "[%d] `%s` — %s\n", i+1, c.Symbol, c.Ref)
		}
	}

	return b.String()
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
