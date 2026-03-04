package rag

import (
	"encoding/binary"
	"hash/fnv"
	"math"
)

type Embedder interface {
	Dimension() int
	Embed(text string) []float32
}

// HashEmbedder is a deterministic fallback embedder used for local scaffold
// behavior until production embeddings are integrated.
type HashEmbedder struct {
	dim int
}

func NewHashEmbedder(dim int) *HashEmbedder {
	if dim <= 0 {
		dim = 384
	}
	return &HashEmbedder{dim: dim}
}

func (e *HashEmbedder) Dimension() int {
	return e.dim
}

func (e *HashEmbedder) Embed(text string) []float32 {
	vec := make([]float32, e.dim)
	if text == "" {
		return vec
	}
	// Stable pseudo-random projection via FNV.
	for i := 0; i < e.dim; i++ {
		h := fnv.New64a()
		_, _ = h.Write([]byte(text))
		var salt [8]byte
		binary.LittleEndian.PutUint64(salt[:], uint64(i*1315423911))
		_, _ = h.Write(salt[:])
		v := float64(h.Sum64()%1000000)/1000000.0 - 0.5
		vec[i] = float32(v)
	}
	normalize(vec)
	return vec
}

func normalize(v []float32) {
	var s float64
	for _, x := range v {
		s += float64(x * x)
	}
	if s == 0 {
		return
	}
	n := float32(math.Sqrt(s))
	for i := range v {
		v[i] /= n
	}
}
