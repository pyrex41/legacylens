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

func TestChunkFileTypedFunction(t *testing.T) {
	src := `real function dot_product(a, b)
  real, intent(in) :: a, b
  dot_product = a * b
end function dot_product`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("math.f90", src)

	if len(chunks) == 0 {
		t.Fatalf("expected chunks for typed function, got none")
	}

	found := false
	for _, c := range chunks {
		if c.Name == "dot_product" {
			found = true
			if c.Type != ChunkTypeFunction {
				t.Errorf("expected type function, got %s", c.Type)
			}
			if len(c.Parameters) < 2 {
				t.Errorf("expected at least 2 params, got %d", len(c.Parameters))
			}
		}
	}
	if !found {
		t.Fatal("expected a chunk for dot_product")
	}
}

func TestChunkFileDoublePrecisionFunction(t *testing.T) {
	src := `double precision function daxpy_val(a, x, y)
  double precision, intent(in) :: a, x, y
  daxpy_val = a * x + y
end function daxpy_val`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("daxpy.f90", src)

	found := false
	for _, c := range chunks {
		if c.Name == "daxpy_val" {
			found = true
			if c.Type != ChunkTypeFunction {
				t.Errorf("expected function type, got %s", c.Type)
			}
		}
	}
	if !found {
		t.Fatal("expected a chunk for daxpy_val")
	}
}

func TestChunkFilePureSubroutine(t *testing.T) {
	src := `pure subroutine scale(x, factor)
  real, intent(inout) :: x
  real, intent(in) :: factor
  x = x * factor
end subroutine scale`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("scale.f90", src)

	found := false
	for _, c := range chunks {
		if c.Name == "scale" {
			found = true
			if c.Type != ChunkTypeSubroutine {
				t.Errorf("expected subroutine, got %s", c.Type)
			}
		}
	}
	if !found {
		t.Fatal("expected a chunk for pure subroutine scale")
	}
}

func TestChunkFileElementalFunction(t *testing.T) {
	src := `elemental function square(x)
  real, intent(in) :: x
  real :: square
  square = x * x
end function square`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("elem.f90", src)

	found := false
	for _, c := range chunks {
		if c.Name == "square" {
			found = true
			if c.Type != ChunkTypeFunction {
				t.Errorf("expected function, got %s", c.Type)
			}
		}
	}
	if !found {
		t.Fatal("expected a chunk for elemental function square")
	}
}

func TestChunkFileRecursiveFunction(t *testing.T) {
	src := `recursive function factorial(n) result(res)
  integer, intent(in) :: n
  integer :: res
  if (n <= 1) then
    res = 1
  else
    res = n * factorial(n - 1)
  end if
end function factorial`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("fact.f90", src)

	found := false
	for _, c := range chunks {
		if c.Name == "factorial" {
			found = true
			if c.Type != ChunkTypeFunction {
				t.Errorf("expected function, got %s", c.Type)
			}
		}
	}
	if !found {
		t.Fatal("expected a chunk for recursive function factorial")
	}
}

func TestChunkFileInterfaceBlock(t *testing.T) {
	src := `module m_ops
  interface dot
    module procedure sdot, ddot
  end interface dot
contains
  function sdot(a, b)
    real :: sdot
    real, intent(in) :: a, b
    sdot = a * b
  end function sdot
  function ddot(a, b)
    double precision :: ddot
    double precision, intent(in) :: a, b
    ddot = a * b
  end function ddot
end module m_ops`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("ops.f90", src)

	types := map[string]ChunkType{}
	for _, c := range chunks {
		types[c.Name] = c.Type
	}

	if types["dot"] != ChunkTypeInterface {
		t.Errorf("expected interface chunk for 'dot', got %v", types["dot"])
	}
	if types["sdot"] != ChunkTypeFunction {
		t.Errorf("expected function chunk for 'sdot', got %v", types["sdot"])
	}
	if types["ddot"] != ChunkTypeFunction {
		t.Errorf("expected function chunk for 'ddot', got %v", types["ddot"])
	}
}

func TestChunkFileTypeDefinition(t *testing.T) {
	src := `module m_types
  type :: matrix_t
    integer :: rows, cols
    real, allocatable :: data(:,:)
  end type matrix_t
end module m_types`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("types.f90", src)

	found := false
	for _, c := range chunks {
		if c.Name == "matrix_t" {
			found = true
			if c.Type != ChunkTypeDef {
				t.Errorf("expected type definition, got %s", c.Type)
			}
			if !containsSkill(c.Skills, "type-definition") {
				t.Errorf("expected type-definition skill, got %v", c.Skills)
			}
		}
	}
	if !found {
		t.Fatal("expected a chunk for type matrix_t")
	}
}

func TestChunkTypeDefPlain(t *testing.T) {
	src := `type point
  real :: x, y
end type point`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("point.f90", src)

	found := false
	for _, c := range chunks {
		if c.Name == "point" {
			found = true
			if c.Type != ChunkTypeDef {
				t.Errorf("expected type, got %s", c.Type)
			}
		}
	}
	if !found {
		t.Fatal("expected a chunk for plain type point")
	}
}

func TestChunkFileProgramBlock(t *testing.T) {
	src := `program test_blas
  implicit none
  call sgemv(1.0, 2.0, 3.0)
end program test_blas`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("test.f90", src)

	found := false
	for _, c := range chunks {
		if c.Name == "test_blas" {
			found = true
			if c.Type != ChunkTypeProgram {
				t.Errorf("expected program, got %s", c.Type)
			}
			if !containsSkill(c.Skills, "program") {
				t.Errorf("expected program skill, got %v", c.Skills)
			}
		}
	}
	if !found {
		t.Fatal("expected a chunk for program test_blas")
	}
}

func TestChunkFileBareEnd(t *testing.T) {
	src := `subroutine simple(x)
  real :: x
  x = x + 1.0
end`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("simple.f90", src)

	found := false
	for _, c := range chunks {
		if c.Name == "simple" {
			found = true
			if c.Type != ChunkTypeSubroutine {
				t.Errorf("expected subroutine, got %s", c.Type)
			}
		}
	}
	if !found {
		t.Fatal("expected a chunk for subroutine closed with bare 'end'")
	}
}

func TestChunkFileContinuationLines(t *testing.T) {
	src := `subroutine long_args(alpha, &
     beta, &
     gamma)
  real, intent(in) :: alpha, beta
  real, intent(out) :: gamma
  gamma = alpha + beta
end subroutine long_args`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("cont.f90", src)

	found := false
	for _, c := range chunks {
		if c.Name == "long_args" {
			found = true
			if c.Type != ChunkTypeSubroutine {
				t.Errorf("expected subroutine, got %s", c.Type)
			}
			paramNames := make(map[string]bool)
			for _, p := range c.Parameters {
				paramNames[strings.ToLower(p.Name)] = true
			}
			for _, expected := range []string{"alpha", "beta", "gamma"} {
				if !paramNames[expected] {
					t.Errorf("expected param %s, got params %v", expected, c.Parameters)
				}
			}
		}
	}
	if !found {
		t.Fatal("expected a chunk for subroutine with continuation lines")
	}
}

func TestChunkFileSubroutineNoArgs(t *testing.T) {
	src := `subroutine init
  print *, "initialized"
end subroutine init`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("init.f90", src)

	found := false
	for _, c := range chunks {
		if c.Name == "init" {
			found = true
			if c.Type != ChunkTypeSubroutine {
				t.Errorf("expected subroutine, got %s", c.Type)
			}
		}
	}
	if !found {
		t.Fatal("expected a chunk for subroutine with no args")
	}
}

func TestChunkFileModuleProcedureNotModule(t *testing.T) {
	src := `module m_test
  interface
    module procedure foo
  end interface
contains
  subroutine foo(x)
    real, intent(in) :: x
  end subroutine foo
end module m_test`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("modproc.f90", src)

	for _, c := range chunks {
		if c.Name == "procedure" {
			t.Fatal("'module procedure' should not create a module chunk named 'procedure'")
		}
	}
}

func TestChunkFileTypeGuardNotTypeDef(t *testing.T) {
	src := `subroutine check(x)
  class(*), intent(in) :: x
  select type(x)
  type is (integer)
    print *, "int"
  end select
end subroutine check`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("guard.f90", src)

	for _, c := range chunks {
		if c.Type == ChunkTypeDef {
			t.Fatal("type guard (type is/type()) should not create a type definition chunk")
		}
	}
}

func TestChunkFileTypeDeclarationMerge(t *testing.T) {
	src := `subroutine compute(a, b, result)
  integer, intent(in) :: a
  real, intent(in) :: b
  real, intent(out) :: result
  result = real(a) * b
end subroutine compute`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("compute.f90", src)

	for _, c := range chunks {
		if c.Name == "compute" {
			paramTypes := map[string]string{}
			for _, p := range c.Parameters {
				paramTypes[strings.ToLower(p.Name)] = strings.ToLower(p.Type)
			}
			if paramTypes["a"] != "integer" {
				t.Errorf("expected param 'a' type=integer, got %q", paramTypes["a"])
			}
			if paramTypes["b"] != "real" {
				t.Errorf("expected param 'b' type=real, got %q", paramTypes["b"])
			}
			if paramTypes["result"] != "real" {
				t.Errorf("expected param 'result' type=real, got %q", paramTypes["result"])
			}
		}
	}
}

func TestChunkFileFallbackWholeFile(t *testing.T) {
	src := `! This is just a comment file
! with no Fortran structures`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("comments.f90", src)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 fallback chunk, got %d", len(chunks))
	}
	if chunks[0].Type != ChunkTypeUnknown {
		t.Errorf("expected unknown type for fallback, got %s", chunks[0].Type)
	}
}

func TestChunkFileSplitLargeUnit(t *testing.T) {
	var lines []string
	lines = append(lines, "subroutine big(x)")
	lines = append(lines, "  real, intent(inout) :: x")
	for i := 0; i < 50; i++ {
		lines = append(lines, "  x = x + 1.0")
	}
	lines = append(lines, "end subroutine big")
	src := strings.Join(lines, "\n")

	chunker := NewFortranChunker(20)
	chunks := chunker.ChunkFile("big.f90", src)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple parts for large subroutine, got %d", len(chunks))
	}
	for _, c := range chunks {
		if !strings.Contains(c.Name, "big") {
			t.Errorf("expected name containing 'big', got %s", c.Name)
		}
	}
}

func TestJoinContinuationLines(t *testing.T) {
	raw := []string{
		"subroutine foo(a, &",
		"     b, &",
		"     c)",
		"  real :: a",
	}
	joined := joinContinuationLines(raw)
	if len(joined) != len(raw) {
		t.Fatalf("expected %d lines (preserving count), got %d", len(raw), len(joined))
	}
	if !strings.Contains(joined[0], "a,") || !strings.Contains(joined[0], "c)") {
		t.Errorf("expected merged continuation, got %q", joined[0])
	}
}

func TestChunkFileNestedModuleSubroutine(t *testing.T) {
	src := `module m_nested
contains
  subroutine inner(x)
    real, intent(in) :: x
  end subroutine inner
end module m_nested`

	chunker := NewFortranChunker(200)
	chunks := chunker.ChunkFile("nested.f90", src)

	names := map[string]ChunkType{}
	for _, c := range chunks {
		names[c.Name] = c.Type
	}
	if names["inner"] != ChunkTypeSubroutine {
		t.Errorf("expected subroutine 'inner', got %v", names)
	}
	if names["m_nested"] != ChunkTypeModule {
		t.Errorf("expected module 'm_nested', got %v", names)
	}
}

func containsSkill(skills []string, target string) bool {
	for _, s := range skills {
		if s == target {
			return true
		}
	}
	return false
}
