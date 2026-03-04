package rag

import "testing"

func TestClassifyQuery(t *testing.T) {
	tests := []struct {
		query string
		want  QueryIntent
	}{
		{"how does xerbla error handling work", IntentTargeted},
		{"what is this codebase? What does it do?", IntentOverview},
		{"what build systems does M_blas support", IntentOverview},
		{"sgemv matrix vector multiply", IntentTargeted},
		{"overview of the architecture", IntentOverview},
		{"DGEMM parameters", IntentTargeted},
		{"getting started", IntentOverview},
		{"what does set_xerbla do", IntentTargeted},
	}
	for _, tt := range tests {
		got := ClassifyQuery(tt.query)
		if got != tt.want {
			t.Errorf("ClassifyQuery(%q) = %d, want %d", tt.query, got, tt.want)
		}
	}
}
