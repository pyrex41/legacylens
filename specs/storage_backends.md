---
title: Dual Storage Backends
description: Define schema parity and retrieval behavior for CozoDB and SQLite implementations.
skills:
  - backend-abstraction
  - hybrid-search
  - benchmark-fairness
---

## Backend Abstraction

All backends must implement a single `VectorStore` interface with parity for:

- initialization
- upsert
- vector search
- keyword search
- hybrid search

## SQLite Backend Requirements

- FTS5 for keyword retrieval
- sqlite-vec for vector kNN
- single-file database deployment path
- identical metadata payload contract as other backends

## CozoDB Backend Requirements

- vector index support (HNSW)
- relational metadata filtering
- compatibility with future graph-level extensions

## Hybrid Search Requirements

- Run vector and keyword retrieval independently
- Fuse with Reciprocal Rank Fusion (RRF)
- Return top-k with component score visibility:
  - vector score
  - keyword score
  - hybrid score
