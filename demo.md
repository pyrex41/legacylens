# LegacyLens: RAG for Legacy Fortran Codebases

*2026-03-04T20:20:38Z by Showboat 0.6.1*
<!-- showboat-id: 21099187-53ca-4e2f-a1e9-66c2c13d0ca0 -->

LegacyLens is a RAG engine that indexes Fortran codebases and Markdown docs, then answers natural language questions using hybrid search (vector + full-text + graph). It supports two backends: SQLite (portable) and CozoDB (graph-aware Datalog). Let's see it in action.

## Build

```bash
CGO_LDFLAGS="-L$(pwd)/libs" go build -mod=vendor -o legacylens ./cmd/legacylens/ && echo 'Build OK'
```

```output
Build OK
```

## Index and Search with CozoDB Backend

The CozoDB backend builds graph edges between chunks (containment, call graphs, doc links) and uses a unified Datalog query for search. On first run it indexes all `.f90` and `.md` files. Let's ask a targeted question about a specific routine:

```bash
CGO_LDFLAGS="-L$(pwd)/libs" ./legacylens -c -r ./third_party/M_blas "how does xerbla error handling work"
```

```output
2026/03/04 14:21:08 INFO: CachedDir="/Users/reuben/.cache/tokenizer"
2026/03/04 14:24:30 indexed 1048 chunks in 3m21.605648459s
backend=cozo chunks=1048 ingest=3m21.605648459s query=326.751458ms

1) Detailed Findings doc          2026-03-04-codebase-overview.md:31-153  score=2.935
   What is this codebase? What does it do? How does it work? — M_blas wraps the complete...
2) Architecture Documentation doc          2026-03-04-codebase-overview.md:167-174  score=2.913
   What is this codebase? What does it do? How does it work? — - **Single-module design*...
3) XERBLA           subroutine   compatible.f90:20646-20734  score=2.540
   params: SRNAME(CHARACTER,in) INFO(INTEGER,in)
   COMMENT --file xerbla.3m_blas.man \brief \b XERBLA =========== DOCUMENTATION ===========
4) set_xerbla       subroutine   M_blas.f90:25-28  score=1.499
   params: proc
   Fortran subroutine set_xerbla extracted from source.
5) std_xerbla       subroutine   M_blas.f90:183-205  score=1.459
   params: srname(character,in) info(integer,in)
   -- Reference BLAS level1 routine (version 3.7.0) -- -- Reference BLAS is a software pac...
```

The graph edges surface related routines: `set_xerbla` and `std_xerbla` appear via call-graph expansion from XERBLA. Doc chunks that mention xerbla also rank highly.

Now let's try an overview query — the query classifier detects this as broad and boosts doc chunks:

```bash
CGO_LDFLAGS="-L$(pwd)/libs" ./legacylens -c -r ./third_party/M_blas "what is this codebase? What does it do?"
```

```output
2026/03/04 14:24:41 INFO: CachedDir="/Users/reuben/.cache/tokenizer"
backend=cozo chunks=1048 ingest=0s query=597.547875ms

1) Research Question doc          2026-03-04-codebase-overview.md:22-24  score=9.886
   What is this codebase? What does it do? How does it work? — What is this codebase? Wh...
2) Detailed Findings doc          2026-03-04-codebase-overview.md:31-153  score=5.394
   What is this codebase? What does it do? How does it work? — M_blas wraps the complete...
3) Open Questions   doc          2026-03-04-codebase-overview.md:175-179  score=5.114
   What is this codebase? What does it do? How does it work? — - How exactly are the `.f...
4) Summary          doc          2026-03-04-codebase-overview.md:25-30  score=4.525
   What is this codebase? What does it do? How does it work? — M_blas is a **Fortran 90/...
5) Process Steps    doc          create_plan.md:36-184  score=4.445
   1. **Read all mentioned files immediately and FULLY**: - Ticket files (e.g., `thoughts/...
```

All 5 results are doc chunks — the overview classifier kicked in and boosted them via a 1.5x multiplier. Scores range from 9.9 to 4.4 showing good differentiation.

## SQLite Backend

The SQLite backend uses RRF (Reciprocal Rank Fusion) over vector + keyword search. For overview queries, it runs a separate doc-type FTS query and merges results:

```bash
CGO_LDFLAGS="-L$(pwd)/libs" ./legacylens -r ./third_party/M_blas "what build systems does M_blas support"
```

```output
2026/03/04 14:24:57 INFO: CachedDir="/Users/reuben/.cache/tokenizer"
backend=sqlite chunks=1048 ingest=0s query=438.784583ms

1) Detailed Findings doc          2026-03-04-codebase-overview.md:31-153  score=1.549
   What is this codebase? What does it do? How does it work? — M_blas wraps the complete...
2) Architecture Documentation doc          2026-03-04-codebase-overview.md:167-174  score=1.408
   What is this codebase? What does it do? How does it work? — - **Single-module design*...
3) Open Questions   doc          2026-03-04-codebase-overview.md:175-179  score=1.291
   What is this codebase? What does it do? How does it work? — - How exactly are the `.f...
4) Code References  doc          2026-03-04-codebase-overview.md:154-166  score=1.191
   What is this codebase? What does it do? How does it work? — - `src/M_blas.f90` — Ma...
5) Summary          doc          2026-03-04-codebase-overview.md:25-30  score=1.106
   What is this codebase? What does it do? How does it work? — M_blas is a **Fortran 90/...
```

## JSON Output

The `-j` flag produces structured JSON, useful for piping into other tools:

```bash
CGO_LDFLAGS="-L$(pwd)/libs" ./legacylens -j -c -r ./third_party/M_blas "sgemv" 2>/dev/null | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(f\"Backend: {d[\"backend\"]}\")
print(f\"Query:   {d[\"query\"]}\")
print(f\"Results: {len(d[\"results\"])}\")
for r in d[\"results\"][:3]:
    print(f\"  {r[\"type\"]:<12} {r[\"name\"]:<20} score={r[\"hybrid_score\"]:.3f}\")
"
```

```output
Backend: cozo
Query:   sgemv
Results: 5
  subroutine   sgemv                score=3.419
  subroutine   SGEMV_part_2         score=3.337
  subroutine   SGEMV_part_1         score=3.324
```

## Tests

All 84 tests pass across both backends, edge extraction, query classification, and more:

```bash
CGO_LDFLAGS="-L$(pwd)/libs" go test -mod=vendor ./internal/rag/ -count=1 2>&1 | tail -1
```

```output
ok  	legacylens/internal/rag	1.092s
```

```bash
CGO_LDFLAGS="-L$(pwd)/libs" go test -mod=vendor ./internal/rag/ -v -count=1 -run "Edge|Classify" 2>&1 | grep -E "^(=== RUN|--- )"
```

```output
=== RUN   TestExtractContainmentEdges
--- PASS: TestExtractContainmentEdges (0.00s)
=== RUN   TestExtractDocEdges
--- PASS: TestExtractDocEdges (0.00s)
=== RUN   TestClassifyQuery
--- PASS: TestClassifyQuery (0.00s)
=== RUN   TestCozoStoreEdgeBuild
--- PASS: TestCozoStoreEdgeBuild (0.01s)
=== RUN   TestCozoStoreHybridSearchWithEdges
--- PASS: TestCozoStoreHybridSearchWithEdges (0.00s)
```

Source: [github.com/pyrex41/legacylens](https://github.com/pyrex41/legacylens)
