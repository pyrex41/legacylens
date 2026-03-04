package rag

import (
	"regexp"
	"strings"
)

// QueryIntent classifies how a query should be handled.
type QueryIntent int

const (
	// IntentTargeted is a query for a specific function, routine, or concept.
	IntentTargeted QueryIntent = iota
	// IntentOverview is a broad query about the codebase, architecture, or purpose.
	IntentOverview
)

// overviewSignals are phrases that indicate a broad/overview query.
var overviewSignals = []string{
	"what is this",
	"what does this",
	"what does it",
	"what is the codebase",
	"what does the codebase",
	"overview",
	"architecture",
	"purpose of the",
	"purpose of this",
	"build system",
	"getting started",
	"introduction",
	"summary",
	"codebase",
	"project structure",
	"repository",
	"design of",
	"what are the",
	"what build",
	"documentation",
}

// reIdentifier matches tokens that look like code identifiers:
// - lowercase underscore names: set_xerbla, node_skills (not M_blas — project names)
// - ALL-CAPS 3+ chars: DGEMM, XERBLA (but not "What", "The")
// - common BLAS routine names: sgemv, dgemm, xerbla, etc.
var reIdentifier = regexp.MustCompile(`\b[a-z][a-z0-9]*_[a-z0-9_]*\b|\b[A-Z][A-Z0-9]{2,}\b|\b[sdcz](?i:gemm|gemv|axpy|copy|scal|swap|dot|nrm2|asum|trsm|trsv|ger|syr|her)\b|\b(?i:xerbla)\b`)

// ClassifyQuery determines whether a query is targeted or broad/overview.
// Queries mentioning specific identifiers (function names, routines) are
// always treated as targeted, even if they contain overview-like phrases.
func ClassifyQuery(query string) QueryIntent {
	q := strings.ToLower(query)

	// If query contains what looks like a code identifier, it's targeted
	if reIdentifier.MatchString(query) {
		return IntentTargeted
	}

	for _, signal := range overviewSignals {
		if strings.Contains(q, signal) {
			return IntentOverview
		}
	}
	return IntentTargeted
}

// DocBoost is the score multiplier applied to doc chunks for overview queries.
const DocBoost = 1.5
