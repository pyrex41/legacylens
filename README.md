# LegacyLens (Go) - Dual-Backend RAG for M_blas

LegacyLens is a Retrieval-Augmented Generation (RAG) system for legacy Fortran codebases, starting with `M_blas`.

This repository is structured for a Ralph Wiggum workflow:

- Specs in `specs/`
- Execution loops in `loop.sh` + `PROMPT_*.md`
- Operational runbook in `AGENTS.md`
- Prioritized work queue in `IMPLEMENTATION_PLAN.md`

The current implementation is a Go-first scaffold with:

- Syntax-aware Fortran chunk extraction
- Skills-style YAML frontmatter generation for chunks
- Dual backend abstraction (`sqlite`, `cozo`) with compatible retrieval API
- Hybrid retrieval scoring via Reciprocal Rank Fusion (RRF)
- Backpressure-aware ingestion pipeline (bounded queues + worker limits + enqueue timeout)

## Quick Start

1. Ensure Go 1.22+ is installed.
2. Run quality gates:
   - `go test ./...`
3. Run the scaffold CLI:
   - `go run ./cmd/legacylens -repo ./third_party/M_blas -backend sqlite -query "How does matrix-vector multiplication work?"`

## Ralph Loop

The loop is configured to run the LLM tool in headless mode with:

- `cursor-agent -p`

Usage:

- `./loop.sh` - build mode, unlimited iterations
- `./loop.sh plan` - planning mode, unlimited iterations
- `./loop.sh 20` - build mode, stop after 20 iterations
- `./loop.sh plan 3` - planning mode, stop after 3 iterations

Before first use:

- `chmod +x loop.sh`

## Status

This is an MVP scaffold intended to rapidly converge with loop-driven implementation. The current store implementations are in-memory compatibility layers that emulate dual-backend behavior and make it safe to iterate the architecture before wiring production-grade CozoDB and SQLite integrations.
