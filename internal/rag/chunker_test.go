package rag

import (
	"strings"
	"testing"
)

func TestChunkFileExtractsSubroutineAndFrontmatter(t *testing.T) {
	src := `
module m_blas_sample
contains
subroutine sgemv(a, x, y)
  real, intent(in) :: a
  real, intent(in) :: x
  real, intent(out) :: y
end subroutine sgemv
end module m_blas_sample
`
	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("src/m_blas_sample.f90", src)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks, got none")
	}
	var found bool
	for _, c := range chunks {
		if strings.Contains(strings.ToLower(c.Name), "sgemv") {
			found = true
			if c.Frontmatter == "" || !strings.Contains(c.Frontmatter, "title:") {
				t.Fatalf("expected frontmatter with title, got: %q", c.Frontmatter)
			}
			if c.StartLine <= 0 || c.EndLine < c.StartLine {
				t.Fatalf("invalid line range: %d-%d", c.StartLine, c.EndLine)
			}
		}
	}
	if !found {
		t.Fatalf("expected a chunk for sgemv")
	}
}
