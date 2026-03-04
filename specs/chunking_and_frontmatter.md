---
title: Fortran Chunking and Frontmatter
description: Define syntax-aware chunk extraction and frontmatter generation for agent-readable context.
skills:
  - parse-fortran-structures
  - generate-yaml-frontmatter
  - preserve-semantic-units
---

## Chunking Strategy

Use Fortran syntax-aware extraction to segment files by:

- module
- subroutine
- function
- nested units when present

Fallback behavior is required when parser confidence is low:

- recursive token or line splitting with stable references
- never lose file/line provenance

## Metadata Contract (per chunk)

- `id`
- `file`
- `start_line`
- `end_line`
- `name`
- `type`
- `parameters` (name/type/intent/description)
- `frontmatter` (skills-style YAML)
- `code` (original chunk text)

## Skills-Style Frontmatter Requirements

Every chunk includes frontmatter with minimum fields:

```yaml
---
title: symbol_name
description: concise purpose summary
parameters:
  - name: x
    type: real
    intent: in
    description: input value
skills:
  - fortran
  - blas
---
```

## Size Control

- Target: 512-1024 tokens/chunk
- If unit exceeds target, split recursively while preserving symbol lineage and line ranges
- Emit deterministic part suffixes (`_part_1`, `_part_2`, ...)
