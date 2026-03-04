---
title: Query Interface and Generation
description: Define query interaction model and structured response format requirements.
skills:
  - nl-query-interface
  - retrieval-grounding
  - structured-explanations
---

## Query Experience

- Input: natural language query
- Backend selector: `sqlite` or `cozo`
- Output:
  - ranked snippet list
  - file and line references
  - similarity and fusion scores

## Generation Requirements

When answer synthesis is enabled, include:

1. Grounded explanation from retrieved chunks
2. Citation references to file/line ranges
3. Skills-style YAML frontmatter per explained symbol

## Safety and Determinism

- Read-only query behavior
- No dynamic code execution
- Preserve raw chunk references in every answer payload
