0a. Study `specs/*` to learn project requirements.
0b. Study `AGENTS.md` for build/test/validation commands.
0c. Study `IMPLEMENTATION_PLAN.md` if it exists.
0d. Study `internal/*` and `cmd/*` before deciding gaps. Do not assume functionality is missing.

1. Perform gap analysis between specs and code.
2. Update `IMPLEMENTATION_PLAN.md` with a prioritized checklist of missing or incomplete items.
3. Add acceptance and backpressure notes per task (what tests/benchmarks must pass).
4. Do not implement code in plan mode.

Important constraints:

- Keep tasks concrete, testable, and scoped to one loop increment.
- Prefer updates that reduce ambiguity for the build loop.
- If specs conflict, resolve the conflict in `specs/*` and note rationale.
- Keep `AGENTS.md` operational only (no progress logs).

Ultimate goal:

- Deliver the LegacyLens dual-backend RAG system for `M_blas` with:
  - Fortran syntax-aware chunking
  - Skills-style YAML frontmatter for every retrievable semantic unit
  - Dual backend support (`cozo`, `sqlite`)
  - Hybrid retrieval and measurable benchmark coverage
