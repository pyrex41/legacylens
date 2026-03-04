---
title: Benchmarking and Deployment
description: Define performance evaluation methodology and deployable runtime constraints.
skills:
  - benchmark-design
  - observability
  - reproducible-deployment
---

## Metrics

Track per backend on identical hardware:

- ingestion wall-clock time
- query p50/p95 latency
- memory profile during ingestion
- on-disk storage footprint
- relevance quality on fixed query set

## Benchmark Protocol

- Minimum 5 repeated runs per scenario
- Compare:
  - vector-only
  - hybrid vector+keyword
- Maintain same chunking and embedding inputs for both backends

## MVP Success Targets

- p95 query latency under 200ms on standard local hardware (dataset permitting)
- relevance at or above 85% manual usefulness on curated 20-query set
- complete retrieval metadata and structured frontmatter in result payloads

## Deployment Requirements

- container-friendly runtime
- publicly accessible demo endpoint
- configurable backend selector via environment variable and UI control
