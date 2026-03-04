---
title: LegacyLens Dual-Backend RAG Overview
description: Build a Go-based RAG system for the M_blas Fortran codebase with pluggable storage backends and structured retrieval output.
skills:
  - define-mvp-scope
  - establish-backend-abstraction
  - enforce-structured-agent-readable-output
---

## Objective

Deliver a production-oriented MVP that ingests and indexes `M_blas` Fortran code, supports natural-language queries, and compares dual backend behavior under identical retrieval logic.

## Scope

- Codebase target: `https://github.com/urbanjost/M_blas`
- Language target: free-format Fortran (`.f90`)
- Core flow: ingest -> chunk -> embed -> store -> retrieve -> answer
- Backends: SQLite (FTS5 + sqlite-vec) and CozoDB
- Runtime stack: Go for orchestration and pipeline control

## Hard Requirements

1. Preserve semantic code units during chunking (module, subroutine, function).
2. Attach skills-style YAML frontmatter to chunk payloads and explanation outputs.
3. Offer backend selector with parity in retrieval behavior.
4. Provide hybrid search (vector + keyword) with score fusion.
5. Instrument ingestion/query performance and persist benchmark artifacts.

## Non-Goals (MVP)

- Full multi-repo support beyond `M_blas`
- Interactive refactoring of source code
- Call graph analytics as mandatory feature (deferred; Cozo future extension)
