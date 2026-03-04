package rag

import (
	"strings"
	"testing"
)

func TestBuildFrontmatterMinimalChunk(t *testing.T) {
	c := Chunk{Name: "sgemv", Type: ChunkTypeSubroutine}
	fm := BuildFrontmatter(c)

	if !strings.HasPrefix(fm, "---\n") {
		t.Error("frontmatter should start with ---")
	}
	if !strings.HasSuffix(fm, "\n---") {
		t.Error("frontmatter should end with ---")
	}
	if !strings.Contains(fm, "title: \"sgemv\"") {
		t.Errorf("expected title sgemv, got:\n%s", fm)
	}
	if !strings.Contains(fm, "description: \"Fortran semantic unit\"") {
		t.Errorf("expected default description, got:\n%s", fm)
	}
	if !strings.Contains(fm, "parameters:\n  []") {
		t.Errorf("expected empty parameters, got:\n%s", fm)
	}
	if !strings.Contains(fm, "skills:\n  - \"fortran\"\n  - \"legacy-code\"") {
		t.Errorf("expected default skills, got:\n%s", fm)
	}
}

func TestBuildFrontmatterWithParameters(t *testing.T) {
	c := Chunk{
		Name: "dgemm",
		Type: ChunkTypeSubroutine,
		Parameters: []Parameter{
			{Name: "m", Type: "integer", Intent: "in", Description: "number of rows"},
			{Name: "alpha", Type: "real", Intent: "in"},
			{Name: "result", Intent: "out"},
		},
		Skills: []string{"fortran", "blas", "subroutine"},
	}
	fm := BuildFrontmatter(c)

	if !strings.Contains(fm, "- name: \"m\"") {
		t.Errorf("expected param m, got:\n%s", fm)
	}
	if !strings.Contains(fm, "type: \"integer\"") {
		t.Errorf("expected type integer, got:\n%s", fm)
	}
	if !strings.Contains(fm, "intent: \"in\"") {
		t.Errorf("expected intent in, got:\n%s", fm)
	}
	if !strings.Contains(fm, "description: \"number of rows\"") {
		t.Errorf("expected description, got:\n%s", fm)
	}
	if !strings.Contains(fm, "description: \"n/a\"") {
		t.Errorf("expected n/a for missing description on alpha, got:\n%s", fm)
	}
	if !strings.Contains(fm, "type: \"unknown\"") {
		t.Errorf("expected unknown for missing type on result, got:\n%s", fm)
	}
	if !strings.Contains(fm, "- \"blas\"") {
		t.Errorf("expected blas skill, got:\n%s", fm)
	}
}

func TestBuildFrontmatterWithDescription(t *testing.T) {
	c := Chunk{
		Name:        "saxpy",
		Description: "scalar alpha x plus y",
	}
	fm := BuildFrontmatter(c)

	if !strings.Contains(fm, "description: \"scalar alpha x plus y\"") {
		t.Errorf("expected custom description, got:\n%s", fm)
	}
}

func TestBuildFrontmatterUnnamedChunk(t *testing.T) {
	c := Chunk{Type: ChunkTypeUnknown}
	fm := BuildFrontmatter(c)

	if !strings.Contains(fm, "title: unnamed") {
		t.Errorf("expected unnamed title, got:\n%s", fm)
	}
}

func TestBuildFrontmatterYAMLStructure(t *testing.T) {
	c := Chunk{
		Name: "test",
		Type: ChunkTypeFunction,
		Parameters: []Parameter{
			{Name: "x", Type: "real", Intent: "in", Description: "input"},
		},
		Skills: []string{"fortran", "blas"},
	}
	fm := BuildFrontmatter(c)

	lines := strings.Split(fm, "\n")
	if lines[0] != "---" {
		t.Errorf("first line should be ---, got %q", lines[0])
	}
	if lines[len(lines)-1] != "---" {
		t.Errorf("last line should be ---, got %q", lines[len(lines)-1])
	}

	requiredFields := []string{"title:", "description:", "parameters:", "skills:"}
	for _, field := range requiredFields {
		if !strings.Contains(fm, field) {
			t.Errorf("frontmatter missing required field %q", field)
		}
	}
}

func TestSafeYAMLValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", `"hello"`},
		{"", "unknown"},
		{"  ", "unknown"},
		{`has "quotes"`, `"has \"quotes\""`},
		{" leading space ", `"leading space"`},
	}
	for _, tt := range tests {
		got := safeYAMLValue(tt.input)
		if got != tt.want {
			t.Errorf("safeYAMLValue(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestZeroDefault(t *testing.T) {
	tests := []struct {
		value, def, want string
	}{
		{"real", "unknown", "real"},
		{"", "unknown", "unknown"},
		{"  ", "n/a", "n/a"},
		{"integer", "default", "integer"},
	}
	for _, tt := range tests {
		got := zeroDefault(tt.value, tt.def)
		if got != tt.want {
			t.Errorf("zeroDefault(%q, %q) = %q, want %q", tt.value, tt.def, got, tt.want)
		}
	}
}

func TestBuildFrontmatterParameterFields(t *testing.T) {
	c := Chunk{
		Name: "test_sub",
		Parameters: []Parameter{
			{Name: "x", Type: "real", Intent: "in", Description: "input value"},
		},
		Skills: []string{"fortran"},
	}
	fm := BuildFrontmatter(c)

	paramSection := fm[strings.Index(fm, "parameters:"):]
	paramSection = paramSection[:strings.Index(paramSection, "skills:")]

	for _, field := range []string{"name:", "type:", "intent:", "description:"} {
		if !strings.Contains(paramSection, field) {
			t.Errorf("parameter section missing field %q in:\n%s", field, paramSection)
		}
	}
}

func TestBuildFrontmatterDefaultSkills(t *testing.T) {
	c := Chunk{Name: "bare"}
	fm := BuildFrontmatter(c)

	if !strings.Contains(fm, "- \"fortran\"") {
		t.Error("expected default fortran skill")
	}
	if !strings.Contains(fm, "- \"legacy-code\"") {
		t.Error("expected default legacy-code skill")
	}
}

func TestBuildFrontmatterCustomSkills(t *testing.T) {
	c := Chunk{
		Name:   "custom",
		Skills: []string{"fortran", "blas", "linear-algebra"},
	}
	fm := BuildFrontmatter(c)

	if !strings.Contains(fm, "- \"blas\"") {
		t.Errorf("expected blas skill, got:\n%s", fm)
	}
	if !strings.Contains(fm, "- \"linear-algebra\"") {
		t.Errorf("expected linear-algebra skill, got:\n%s", fm)
	}
	if strings.Contains(fm, "- \"legacy-code\"") {
		t.Error("custom skills should replace defaults, not append")
	}
}

func TestBuildFrontmatterSpecCompliance(t *testing.T) {
	c := Chunk{
		Name:        "sgemv",
		Description: "single precision general matrix-vector multiply",
		Parameters: []Parameter{
			{Name: "x", Type: "real", Intent: "in", Description: "input value"},
		},
		Skills: []string{"fortran", "blas"},
	}
	fm := BuildFrontmatter(c)

	if !strings.HasPrefix(fm, "---") || !strings.HasSuffix(fm, "---") {
		t.Error("frontmatter must be delimited by --- markers (spec requirement)")
	}
	if !strings.Contains(fm, "title:") {
		t.Error("frontmatter must contain title (spec requirement)")
	}
	if !strings.Contains(fm, "description:") {
		t.Error("frontmatter must contain description (spec requirement)")
	}
	if !strings.Contains(fm, "parameters:") {
		t.Error("frontmatter must contain parameters (spec requirement)")
	}
	if !strings.Contains(fm, "skills:") {
		t.Error("frontmatter must contain skills (spec requirement)")
	}
}
