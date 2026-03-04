package rag

import "sort"

// FuseRRF combines ranked lists using Reciprocal Rank Fusion.
func FuseRRF(vectorResults, keywordResults []SearchResult, k int) []SearchResult {
	if k <= 0 {
		k = 5
	}
	const rrfK = 60.0
	type agg struct {
		res SearchResult
	}
	byID := map[string]agg{}

	for i, r := range vectorResults {
		a := byID[r.Chunk.ID]
		a.res.Chunk = r.Chunk
		a.res.VectorScore = r.VectorScore
		a.res.HybridScore += 1.0 / (rrfK + float64(i+1))
		byID[r.Chunk.ID] = a
	}
	for i, r := range keywordResults {
		a := byID[r.Chunk.ID]
		a.res.Chunk = r.Chunk
		a.res.KeywordScore = r.KeywordScore
		a.res.HybridScore += 1.0 / (rrfK + float64(i+1))
		byID[r.Chunk.ID] = a
	}

	out := make([]SearchResult, 0, len(byID))
	for _, a := range byID {
		out = append(out, a.res)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].HybridScore > out[j].HybridScore
	})
	if len(out) > k {
		out = out[:k]
	}
	return out
}
