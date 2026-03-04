---
title: Architecture Decision Matrix
description: Compare CozoDB and SQLite backends for the LegacyLens MVP.
skills:
  - evaluate-storage-options
  - balance-mvp-speed-vs-extensibility
  - support-benchmark-driven-decisions
---

## Decision Matrix

| Criterion | SQLite (FTS5 + sqlite-vec) | CozoDB |
| --- | --- | --- |
| MVP setup speed | Excellent | Good |
| Operational complexity | Low | Medium |
| Embedded deployment | Excellent | Good |
| Advanced graph extensions | Limited | Excellent |
| Metadata filter expressiveness | Good | Excellent |
| Raw query latency (small dataset) | Excellent | Good |
| Future call-graph analytics | Fair | Excellent |

## Recommended Positioning

- **Default MVP backend:** SQLite for fastest path to stable public demo.
- **Strategic extension backend:** CozoDB for richer filtering and graph evolution.

## Acceptance Implication

Backend parity in interface and benchmark harness is mandatory so results are meaningful and reversible.
