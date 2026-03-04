## Build and Run

- Go version: 1.22+
- Module root: repository root
- Main CLI: `go run ./cmd/legacylens`

Examples:

- `go run ./cmd/legacylens -repo ./third_party/M_blas -backend sqlite -query "How does sgemv work?"`
- `go run ./cmd/legacylens -repo ./third_party/M_blas -backend sqlite -query "How does sgemv work?" -explain`
- `go run ./cmd/legacylens -repo ./third_party/M_blas -backend sqlite -embedder http -embed-url http://localhost:8080/v1/embeddings -cache-dir .embed_cache -query "How does sgemv work?"`

HTTP Server:

- `go run ./cmd/legacylens -repo ./third_party/M_blas -serve`
- `go run ./cmd/legacylens -repo ./third_party/M_blas -backend all -serve` (multi-backend with hot-switching)
- `LEGACYLENS_REPO=./third_party/M_blas LEGACYLENS_SERVE=1 go run ./cmd/legacylens`
- Open `http://localhost:8080` for the web UI
- `curl 'http://localhost:8080/api/search?q=sgemv&k=5'`
- `curl -X POST http://localhost:8080/api/search -d '{"query":"matrix multiply","k":3}'`
- `curl 'http://localhost:8080/api/explain?q=sgemv&k=5'` (answer synthesis with citations)
- `curl -X POST http://localhost:8080/api/explain -d '{"query":"matrix multiply","k":3}'`
- `curl http://localhost:8080/api/eval?k=5` (run relevance evaluation)
- `curl -X POST http://localhost:8080/api/switch -d '{"backend":"cozo"}'` (switch active backend)
- `curl http://localhost:8080/api/backends` (list available backends)

Benchmarking:

- `go run ./cmd/legacylens -repo ./third_party/M_blas -backend sqlite -bench`
- `go run ./cmd/legacylens -repo ./third_party/M_blas -backend all -bench -bench-runs 10 -bench-report bench_report`
- `go run ./cmd/legacylens -repo ./third_party/M_blas -backend sqlite -bench -bench-eval` (includes relevance evaluation)
- `go test -bench=. -benchmem ./internal/rag/`

Docker:

- `make docker` — build container image
- `make docker-up` — start with docker compose, hash embedder (mounts `./third_party/M_blas`)
- `make docker-down` — stop containers
- `make docker-embed-up` — start with production embeddings (TEI `all-MiniLM-L6-v2` + HTTP embedder + cache)
- `make docker-embed-down` — stop embeddings stack
- `docker build -t legacylens . && docker run -p 8080:8080 -v ./third_party/M_blas:/data/repo -e LEGACYLENS_REPO=/data/repo legacylens`

## Validation Backpressure Gates

Run these commands after each implementation increment:

- `go test ./...`
- `go vet ./...`

Optional stronger gate:

- `go test -race ./...`

## Formatting

- `gofmt -w ./cmd ./internal`

## Dependencies

After adding new Go files, run:

- `go get modernc.org/sqlite@latest && go mod tidy`

## Embedder Configuration

- Default embedder is `hash` (deterministic FNV-based, for local dev/testing).
- Production embedder is `http`, calling an OpenAI-compatible `/v1/embeddings` endpoint.
- Use `-cache-dir` or `EMBED_CACHE_DIR` to enable file-based embedding cache (SHA256-keyed).
- Use `-embed-key` or `EMBED_API_KEY` for authenticated embedding services.
- Compatible servers: OpenAI API, HuggingFace TEI, local sentence-transformers.

## First-Time Setup

Before any `go build`, `go test`, or `go vet`:

```bash
make deps    # or: go get modernc.org/sqlite@latest && go mod tidy
```

This resolves `modernc.org/sqlite` and generates `go.sum`. Only needed once (or after editing `go.mod`).

## Operational Notes

- This repository uses bounded queues in ingestion to enforce backpressure.
- Enqueue timeouts are part of correctness and should not be bypassed.
- Keep interface parity between backends (`sqlite` and `cozo`) to preserve benchmark fairness.
- SQLite backend uses `modernc.org/sqlite` (pure Go, no CGo). FTS5 for keyword search, in-process cosine kNN for vector search.
- SQLite store `Init()` is idempotent (DDL uses `IF NOT EXISTS`). Pipeline calls `Init()` internally; safe to call beforehand in tests.
- The `cozo` backend (`store_cozo.go`) uses a normalized relational schema (nodes, node_params, node_skills, edges) backed by SQLite via `modernc.org/sqlite`. This emulates CozoDB's relational model and is upgradeable to native CozoDB when Go bindings mature.
- CozoDB store uses transactional upserts (BEGIN/COMMIT) to atomically update node + params + skills.
- CozoDB store `loadNode()` reconstructs full `Chunk` from normalized tables; vector/keyword search fetch IDs first, then hydrate.
- The `edges` table is schema-ready for call-graph extensions but not yet populated by the chunker.
- `CachedEmbedder` reuses `encodeVector`/`decodeVector` from `store_sqlite.go` — do not duplicate vector encoding logic.
- `HTTPEmbedder` returns zero vectors on error; always pair with `CachedEmbedder` in production to avoid re-fetching on transient failures.
- `docker-compose.yml` uses profiles: `default` for hash embedder, `embeddings` for production TEI. `make docker-embed-up` starts HuggingFace TEI (`all-MiniLM-L6-v2`) alongside LegacyLens with HTTP embedder auto-configured. TEI health check waits up to 5 min for model download on first run. Embedding cache is persisted in a Docker volume.
- CLI auto-detects embedder mode: if `EMBED_URL` is set and neither `-embedder` flag nor `LEGACYLENS_EMBEDDER` env var is explicitly provided, automatically uses `http` embedder. Precedence: CLI flag > `LEGACYLENS_EMBEDDER` env > auto-detect from `EMBED_URL` > default `hash`.
- The `legacylens-embed` service does not set `LEGACYLENS_EMBEDDER`; it relies on `EMBED_URL` auto-detection to select the `http` embedder.
- Keep `AGENTS.md` concise and operational. Put progress/status in `IMPLEMENTATION_PLAN.md`.
- Temp files `run_go.sh`, `gorun.sh`, `validate.sh`, `run_validate.sh`, `_run_go.sh`, `_validate.sh`, `_run.sh` in repo root are gitignored and can be deleted.
- Both backends share `tokenize`, `cosine` (from `store.go`), `buildFTSQuery` (from `store_sqlite.go`), and `encodeVector`/`decodeVector` (from `store_sqlite.go`). Do not duplicate these.
- Benchmark harness (`benchmark.go`) uses `storeFactory` callback to allow callers to control DB path and backend type. `RunBenchmark` closes the store via the `VectorStore.Close()` interface method after completion.
- Go native benchmarks (`BenchmarkIngest*`, `BenchmarkHybridSearch*`) use `testing.B` and `b.TempDir()` for isolated DB paths per iteration.
- CLI `-backend all` runs benchmarks for both sqlite and cozo sequentially; report files get `_sqlite.json`/`_cozo.json` suffixes.
- CLI `-bench-eval` includes relevance evaluation (20-query curated set) in benchmark output. `RelevanceSummary` tracks mean precision/recall/hit-rate and per-query PASS/FAIL.
- `EvalRelevance()` uses `QueryEngine.Search()` — same hybrid search path as production queries. `RelevanceJudgment.MatchName` is case-insensitive and handles `_part_` suffixes from chunk splitting.
- HTTP `/api/eval` endpoint runs the curated eval set on the live server. Accepts `?k=N` query parameter.
- `CuratedEvalQueries()` returns the canonical 20-query set. Update this function (not a config file) when adding/changing eval queries.
- CLI `-serve` (or `LEGACYLENS_SERVE=1`) starts an HTTP API server. Ingests repo at startup, then serves `/health` and `/api/search`.
- Environment variables (`LEGACYLENS_REPO`, `LEGACYLENS_BACKEND`, `LEGACYLENS_EMBEDDER`, `EMBED_URL`, `PORT`) configure the CLI. Explicit flags override env vars.
- `PORT` env var is Render/Heroku-compatible (bare number like `8080`); the CLI auto-prepends `:` if missing.
- Dockerfile uses multi-stage build: Go 1.22 builder, Debian slim runtime. Binary is statically compiled (CGO_ENABLED=0). Runs as non-root `app` user.
- `render.yaml` is a Render Blueprint for one-click deploy. Uses Docker runtime with `/health` check.
- Web UI is embedded via `//go:embed static` in `cmd/legacylens/main.go`. Static assets live in `cmd/legacylens/static/`. The UI is a single `index.html` with inline CSS/JS — no build step, no external dependencies.
- UI is served at `/` when running in `-serve` mode. API endpoints (`/health`, `/api/search`) coexist on the same mux.
- Backend hot-switching: use `-backend all` with `-serve` to load both backends at startup. The UI backend selector becomes live — no restart needed. `/api/switch` POST endpoint changes the active backend. Single-backend mode (`-backend sqlite`) still works; UI shows informational selector.
- `serverState` holds all loaded backends behind a `sync.RWMutex`. `current()` returns the active backend for read paths; `switchTo()` atomically changes the active backend.
- Fortran chunker uses enhanced regex parsing (not tree-sitter) to maintain `CGO_ENABLED=0` compatibility. Handles: typed functions (`real function`), prefixed routines (`pure`/`elemental`/`recursive`), interface blocks, type definitions, program blocks, no-arg subroutines, bare `end`, and continuation lines (`&`).
- `joinContinuationLines()` merges `&`-continued lines while preserving line count (empty placeholder lines) for stable provenance tracking.
- `module procedure` lines are guarded against false module-start matches. `type(...)` expressions are guarded against false type-definition matches.
- `ChunkTypeProgram` and `ChunkTypeInterface` are valid chunk types with corresponding skill tags (`program`, `interface`).
- Type declarations (`real :: x`, `integer, intent(in) :: n`) are parsed to populate `Parameter.Type` on matching params via `mergeType()`.
- `QueryEngine.Explain()` synthesizes grounded answers from retrieved chunks. Deterministic template-based (no LLM). Returns structured `ExplainResult` with markdown answer, citations, and per-symbol frontmatter.
- CLI `-explain` flag enables answer synthesis mode alongside `-query`. Without `-explain`, the CLI outputs raw search results as before.
- HTTP `/api/explain` endpoint accepts same parameters as `/api/search` (`q`, `k`, POST body). Returns `answer`, `citations`, `symbols` JSON.
- `describeChunk()` and `synthesizeAnswer()` are internal to `explain.go`. `capitalize()` replaces deprecated `strings.Title`.
- Web UI has two modes: **Search** (raw ranked results) and **Explain** (structured answer with citations and per-symbol frontmatter). Mode toggle is in the controls bar; button and placeholder text update to match mode.
- Explain mode renders the markdown answer from `/api/explain` with inline formatting (headings, numbered lists, bold, code spans). Citations are clickable copy-to-clipboard refs. Symbol cards show collapsible frontmatter panels.
- The UI uses no external JS dependencies — markdown rendering is a minimal inline parser handling `##`, `###`, `1.` lists, `**bold**`, and `` `code` `` spans. It does not handle arbitrary markdown; it is tuned for the output format of `synthesizeAnswer()`.
- `VectorStore` interface includes `Close() error`. All store implementations must implement `Close()`. Do not use type assertions for close; call it directly on the interface.
- HTTP server uses graceful shutdown: `SIGINT`/`SIGTERM` triggers `srv.Shutdown()` (10s grace), then closes all backend stores. This prevents SQLite WAL corruption on container stop (Docker sends SIGTERM).
- CLI query mode uses `defer store.Close()`. Benchmark harness calls `store.Close()` directly via the interface after completion.
- `frontmatter_test.go` directly tests `BuildFrontmatter`, `safeYAMLValue`, and `zeroDefault`. Covers YAML delimiters, required fields, parameter sub-fields, default/custom skills, unnamed chunks, quote escaping, and spec compliance. Run alongside other tests via `go test ./...`.
