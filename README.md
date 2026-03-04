# LegacyLens

A RAG (Retrieval-Augmented Generation) engine for legacy Fortran codebases. Point it at a repository and ask questions in plain English — it chunks Fortran source and Markdown docs, builds embeddings, and returns the most relevant code with context.

## Features

- **Fortran-aware chunking** — splits `.f90` files into modules, subroutines, functions, interfaces, and type definitions with parameter extraction
- **Markdown doc chunking** — indexes `.md` files as doc chunks with section-level granularity
- **Hybrid search** — combines HNSW vector similarity, BM25 full-text search, and name matching
- **Two backends** — SQLite (portable, zero-config) and CozoDB (graph-aware Datalog with edge traversal and skill boosting)
- **Graph edges** (CozoDB) — automatically extracts `contains`, `calls`, and `documents` relationships between chunks for 1-hop graph expansion during search
- **Query classification** — detects overview vs targeted queries; boosts doc chunks for broad questions
- **Explain mode** — optional LLM synthesis that answers your question with citations
- **HTTP server** — web UI with live backend switching, search, and evaluation
- **Benchmarking** — built-in relevance evaluation with curated queries

## Prerequisites

- Go 1.24+
- Make (for convenience targets)

## Quick Start

```bash
# Clone with submodules (includes example M_blas codebase)
git clone --recurse-submodules https://github.com/reuben/legacylens.git
cd legacylens

# Download CozoDB native library and build
make build

# Index a Fortran repo and search
./legacylens -r ./third_party/M_blas "how does xerbla error handling work"
```

## Indexing a Codebase

Point `-r` at any directory containing `.f90` and/or `.md` files:

```bash
# SQLite backend (default) — fast, portable
./legacylens -r /path/to/your/fortran/repo "your query"

# CozoDB backend — adds graph traversal and skill boosting
./legacylens -c -r /path/to/your/fortran/repo "your query"
```

On first run, LegacyLens indexes all `.f90` and `.md` files under the repo path. The index is persisted to `legacylens.db` (SQLite) or `legacylens_cozo.db` (CozoDB) and reused on subsequent runs. Use `-reindex` to force re-ingestion.

## Usage

```
Usage: legacylens [flags] "query..."

Flags:
  -c            Use CozoDB backend (default: SQLite)
  -r PATH       Repository path (or LEGACYLENS_REPO env var)
  -k N          Top K results (default: 5)
  -e            Explain mode (LLM synthesis)
  -s            Start HTTP server
  -j            JSON output
  -reindex      Force re-ingestion
  -t DURATION   Timeout (default: 30m)

Embedder:
  -hash          Use hash embedder (testing only; default: local ONNX)
  -embed-url U   Use HTTP embedder at URL

LLM (for explain mode):
  -llm-key K    LLM API key (or XAI_API_KEY env var)
  -llm-url U    LLM endpoint (default: xAI)
  -llm-model M  LLM model name
```

### Examples

```bash
# Basic search
./legacylens -r ./third_party/M_blas "matrix vector multiply"

# JSON output
./legacylens -j -r ./third_party/M_blas "what build systems does M_blas support"

# Explain mode with LLM
export XAI_API_KEY=your-key
./legacylens -e -r ./third_party/M_blas "how does error handling work in BLAS"

# HTTP server with web UI
./legacylens -s -r ./third_party/M_blas

# Run with both backends (server mode)
./legacylens -s -backend all -r ./third_party/M_blas

# Benchmark
./legacylens -bench -backend all -r ./third_party/M_blas
```

## How It Works

1. **Chunking** — Fortran files are parsed into semantic units (modules, subroutines, functions). Markdown files are split by heading. Each chunk gets YAML frontmatter with metadata (parameters, types, skills).

2. **Embedding** — Each chunk is embedded using a local ONNX model (all-MiniLM-L6-v2, 384 dimensions). An embedding cache avoids redundant computation.

3. **Indexing** — Chunks and vectors are stored in the chosen backend. CozoDB additionally builds graph edges:
   - `contains` — module/program enclosing nested subroutines by line range
   - `calls` — parses `CALL xxx` statements and links to the target chunk
   - `documents` — doc chunk mentioning a code name creates a bidirectional link

4. **Search** — Queries are classified as targeted or overview:
   - **Targeted** (e.g., "sgemv parameters"): hybrid vector + FTS + name matching
   - **Overview** (e.g., "what is this codebase"): same search with doc-type boosting
   - CozoDB adds graph expansion (1-hop neighbors) and skill-token matching via a unified Datalog query

## Development

```bash
make test        # Run all tests
make vet         # Static analysis
make build       # Build binary
make validate    # test + vet
make clean       # Remove build artifacts and databases
```

## License

See [LICENSE](LICENSE).
