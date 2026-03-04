## Build and Run

- Go version: 1.22+
- Module root: repository root
- Main CLI: `go run ./cmd/legacylens`

Example:

- `go run ./cmd/legacylens -repo ./third_party/M_blas -backend sqlite -query "How does sgemv work?"`

## Validation Backpressure Gates

Run these commands after each implementation increment:

- `go test ./...`
- `go vet ./...`

Optional stronger gate:

- `go test -race ./...`

## Formatting

- `gofmt -w ./cmd ./internal`

## Operational Notes

- This repository uses bounded queues in ingestion to enforce backpressure.
- Enqueue timeouts are part of correctness and should not be bypassed.
- Keep interface parity between backends (`sqlite` and `cozo`) to preserve benchmark fairness.
- Keep `AGENTS.md` concise and operational. Put progress/status in `IMPLEMENTATION_PLAN.md`.
