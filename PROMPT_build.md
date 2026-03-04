0a. Study `specs/*` and `IMPLEMENTATION_PLAN.md`.
0b. Study `AGENTS.md` to run the correct validation commands.
0c. Study existing code in `cmd/*` and `internal/*`. Do not assume missing functionality before searching.

1. Pick the highest-priority open item from `IMPLEMENTATION_PLAN.md`.
2. Implement it completely (no placeholders/stubs unless explicitly marked as fallback with rationale).
3. Run backpressure validation for your scope:
   - `go test ./...`
   - `go vet ./...`
4. If tests fail, fix or document blockers in `IMPLEMENTATION_PLAN.md`.
5. Update `IMPLEMENTATION_PLAN.md` to reflect completed work and newly discovered follow-ups.
6. Keep `AGENTS.md` current with operational learnings only.
7. Commit once validation passes.

Important constraints:

- Maintain a single source of truth (avoid duplicate utility logic).
- Keep architecture backend-agnostic where required by specs.
- Preserve skills-style YAML frontmatter behavior in chunking/retrieval outputs.
- Keep changes small enough to verify in one loop iteration.
