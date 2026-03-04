package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"legacylens/internal/rag"
)

//go:embed static
var staticFS embed.FS

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: legacylens [flags] "query..."

Flags:
  -c            Use CozoDB backend (default: SQLite)
  -r PATH       Repository path (or LEGACYLENS_REPO)
  -k N          Top K results (default: 5)
  -e            Explain mode (LLM synthesis)
  -s            Start HTTP server
  -j            JSON output
  -reindex      Force re-ingestion
  -t DURATION   Timeout (default: 30m)

Embedder:
  -hash          Use hash embedder (testing only; default: local ONNX)
  -embed-url U   Use HTTP embedder at URL

LLM:
  -llm-key K    LLM API key (or XAI_API_KEY)
  -llm-url U    LLM endpoint (default: xAI)
  -llm-model M  LLM model (default: grok-4-1-fast-non-reasoning)

Advanced:
  -backend B    Backend: sqlite|cozo|all (overrides -c)
  -embedder M   Embedder: hash|local|http
  -addr ADDR    Listen address (default: :8080)
  -bench        Run benchmark
  -bench-runs N Benchmark repetitions (default: 5)
`)
	os.Exit(2)
}

func main() {
	// Short flags
	useCozo := flag.Bool("c", false, "Use CozoDB backend")
	repo := flag.String("r", "", "Repository path")
	topK := flag.Int("k", 5, "Top K results")
	explain := flag.Bool("e", false, "Explain mode (LLM synthesis)")
	serve := flag.Bool("s", false, "Start HTTP server")
	jsonOut := flag.Bool("j", false, "JSON output")
	reindex := flag.Bool("reindex", false, "Force re-ingestion even if index exists")
	timeout := flag.Duration("t", 30*time.Minute, "Timeout")
	useHash := flag.Bool("hash", false, "Use hash embedder (testing only)")

	// Long flags (less common)
	backend := flag.String("backend", "", "Backend override: sqlite|cozo|all")
	embedMode := flag.String("embedder", "", "Embedder override: hash|local|http")
	embedURL := flag.String("embed-url", "", "HTTP embedding endpoint")
	embedModel := flag.String("embed-model", "all-MiniLM-L6-v2", "Model name for HTTP embedder")
	embedDim := flag.Int("embed-dim", 384, "Embedding dimension")
	embedKey := flag.String("embed-key", "", "API key for HTTP embedder")
	cacheDir := flag.String("cache-dir", "", "Embedding cache directory")
	llmURL := flag.String("llm-url", "https://api.x.ai/v1/chat/completions", "LLM API endpoint")
	llmKey := flag.String("llm-key", "", "LLM API key")
	llmModel := flag.String("llm-model", "grok-4-1-fast-non-reasoning", "LLM model name")
	bench := flag.Bool("bench", false, "Run benchmark")
	benchRuns := flag.Int("bench-runs", 5, "Benchmark repetitions")
	benchReport := flag.String("bench-report", "", "Benchmark JSON report path")
	benchEval := flag.Bool("bench-eval", false, "Include relevance evaluation in benchmark")
	listenAddr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Usage = usage
	flag.Parse()

	// Resolve repo
	envDefault(repo, "LEGACYLENS_REPO")
	if *repo == "" {
		// Also accept -repo for backwards compat via a second pass
		flag.Visit(func(f *flag.Flag) {})
	}

	// Resolve backend: -c flag → cozo, -backend flag → override, env → fallback
	resolvedBackend := "sqlite"
	if *useCozo {
		resolvedBackend = "cozo"
	}
	if *backend != "" {
		resolvedBackend = *backend
	} else if v := os.Getenv("LEGACYLENS_BACKEND"); v != "" {
		if !*useCozo {
			resolvedBackend = v
		}
	}

	// Resolve embedder: default is local, -hash flag → hash (testing), -embedder → override
	resolvedEmbedder := "local"
	if *useHash {
		resolvedEmbedder = "hash"
	}
	if *embedMode != "" {
		resolvedEmbedder = *embedMode
	} else if v := os.Getenv("LEGACYLENS_EMBEDDER"); v != "" {
		if !*useHash {
			resolvedEmbedder = v
		}
	}
	envDefault(embedURL, "EMBED_URL")
	if *embedURL != "" && resolvedEmbedder == "hash" {
		resolvedEmbedder = "http"
	}

	// Resolve LLM key
	envDefault(llmKey, "XAI_API_KEY")

	// Resolve listen address
	envDefaultWithFlag(listenAddr, "PORT", "addr")
	if *listenAddr != "" && (*listenAddr)[0] != ':' {
		*listenAddr = ":" + *listenAddr
	}
	if os.Getenv("LEGACYLENS_SERVE") == "1" {
		*serve = true
	}

	// Query is positional args joined
	queryStr := strings.Join(flag.Args(), " ")

	if *repo == "" {
		fmt.Fprintln(os.Stderr, "error: -r <repo> is required (or set LEGACYLENS_REPO)")
		fmt.Fprintln(os.Stderr, "")
		usage()
	}

	embedder := mustEmbedder(resolvedEmbedder, *embedURL, *embedModel, *embedDim, *embedKey, *cacheDir)

	// Build optional LLM client
	var llm rag.LLMClient
	if *llmKey != "" {
		llm = rag.NewGrokClient(*llmURL, *llmKey, *llmModel)
	}

	if *bench {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		runBenchmark(ctx, resolvedBackend, *repo, *benchRuns, *topK, *benchReport, *benchEval, embedder)
		return
	}

	if *serve {
		runServer(*listenAddr, resolvedBackend, *repo, *topK, embedder, llm)
		return
	}

	if queryStr == "" {
		fmt.Fprintln(os.Stderr, `error: query required. Usage: legacylens [flags] "your query"`)
		fmt.Fprintln(os.Stderr, "       or use -s for HTTP server mode")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	store := mustStore(resolvedBackend, embedder.Dimension())
	defer store.Close()
	if err := store.Init(ctx); err != nil {
		log.Fatalf("store init: %v", err)
	}

	engine := rag.NewQueryEngine(store, embedder)
	if llm != nil {
		engine = engine.WithLLM(llm)
	}

	// Check if index already has data — skip ingestion if so
	n, err := store.Count(ctx)
	if err != nil {
		log.Fatalf("store count: %v", err)
	}
	var ingestDur time.Duration
	if n == 0 || *reindex {
		pipeline := rag.NewPipeline(rag.DefaultPipelineConfig(), embedder, store)
		start := time.Now()
		n, err = pipeline.IngestRepo(ctx, *repo)
		if err != nil {
			log.Fatalf("ingestion failed: %v", err)
		}
		ingestDur = time.Since(start)
		log.Printf("indexed %d chunks in %s", n, ingestDur)
	}

	if *explain {
		qStart := time.Now()
		result, err := engine.Explain(ctx, queryStr, *topK)
		if err != nil {
			log.Fatalf("explain failed: %v", err)
		}
		queryDur := time.Since(qStart)

		if *jsonOut {
			out := map[string]any{
				"query":     result.Query,
				"backend":   store.Name(),
				"answer":    result.Answer,
				"citations": result.Citations,
				"symbols":   result.Symbols,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(out)
			return
		}

		fmt.Printf("backend=%s chunks=%d ingest=%s query=%s\n", store.Name(), n, ingestDur, queryDur)
		fmt.Println(result.Answer)
		for _, sym := range result.Symbols {
			fmt.Println(sym.Frontmatter)
			fmt.Println(sym.Explanation)
			fmt.Println("---")
		}
		return
	}

	qStart := time.Now()
	results, err := engine.Search(ctx, queryStr, *topK)
	if err != nil {
		log.Fatalf("query failed: %v", err)
	}
	queryDur := time.Since(qStart)

	if *jsonOut {
		type resultJSON struct {
			ID           string  `json:"id"`
			File         string  `json:"file"`
			StartLine    int     `json:"start_line"`
			EndLine      int     `json:"end_line"`
			Name         string  `json:"name"`
			Type         string  `json:"type"`
			Description  string  `json:"description"`
			Code         string  `json:"code"`
			VectorScore  float64 `json:"vector_score"`
			KeywordScore float64 `json:"keyword_score"`
			HybridScore  float64 `json:"hybrid_score"`
		}
		outResults := make([]resultJSON, len(results))
		for i, r := range results {
			outResults[i] = resultJSON{
				ID: r.Chunk.ID, File: r.Chunk.File,
				StartLine: r.Chunk.StartLine, EndLine: r.Chunk.EndLine,
				Name: r.Chunk.Name, Type: string(r.Chunk.Type),
				Description: r.Chunk.Description, Code: r.Chunk.Code,
				VectorScore: r.VectorScore, KeywordScore: r.KeywordScore,
				HybridScore: r.HybridScore,
			}
		}
		out := map[string]any{
			"query":   queryStr,
			"backend": store.Name(),
			"results": outResults,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return
	}

	fmt.Printf("backend=%s chunks=%d ingest=%s query=%s\n\n", store.Name(), n, ingestDur, queryDur)
	for i, r := range results {
		fmt.Printf("%d) %-16s %-12s %s:%d-%d  score=%.3f\n",
			i+1, r.Chunk.Name, string(r.Chunk.Type),
			filepath.Base(r.Chunk.File),
			r.Chunk.StartLine, r.Chunk.EndLine, r.HybridScore,
		)
		if len(r.Chunk.Parameters) > 0 {
			var params []string
			for _, p := range r.Chunk.Parameters {
				s := p.Name
				if p.Type != "" || p.Intent != "" {
					s += "("
					if p.Type != "" {
						s += p.Type
					}
					if p.Intent != "" {
						if p.Type != "" {
							s += ","
						}
						s += p.Intent
					}
					s += ")"
				}
				params = append(params, s)
			}
			paramStr := strings.Join(params, " ")
			if len(paramStr) > 80 {
				paramStr = paramStr[:77] + "..."
			}
			fmt.Printf("   params: %s\n", paramStr)
		}
		if r.Chunk.Description != "" {
			desc := r.Chunk.Description
			if len(desc) > 90 {
				desc = desc[:87] + "..."
			}
			fmt.Printf("   %s\n", desc)
		}
	}
}

func envDefault(dst *string, key string) {
	if *dst == "" {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
}

func envDefaultWithFlag(dst *string, envKey, flagName string) {
	if v := os.Getenv(envKey); v != "" {
		if !flagExplicit(flagName) {
			*dst = v
		}
	}
}

func flagExplicit(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

type backendInstance struct {
	name   string
	store  rag.VectorStore
	engine *rag.QueryEngine
	chunks int
}

type serverState struct {
	mu       sync.RWMutex
	active   string
	backends map[string]*backendInstance
}

func (s *serverState) current() *backendInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.backends[s.active]
}

func (s *serverState) switchTo(name string) (*backendInstance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.backends[name]; !ok {
		return nil, false
	}
	s.active = name
	return s.backends[name], true
}

func runServer(addr, backend, repo string, defaultK int, embedder rag.Embedder, llm rag.LLMClient) {
	ctx := context.Background()
	state := &serverState{backends: make(map[string]*backendInstance)}

	backendsToLoad := []string{backend}
	if backend == "all" {
		backendsToLoad = []string{"sqlite", "cozo"}
	}

	for _, bk := range backendsToLoad {
		store := mustStore(bk, embedder.Dimension())
		if err := store.Init(ctx); err != nil {
			log.Fatalf("init %s: %v", bk, err)
		}
		engine := rag.NewQueryEngine(store, embedder)
		if llm != nil {
			engine = engine.WithLLM(llm)
		}

		n, err := store.Count(ctx)
		if err != nil {
			log.Fatalf("count %s: %v", bk, err)
		}
		if n == 0 {
			pipeline := rag.NewPipeline(rag.DefaultPipelineConfig(), embedder, store)
			log.Printf("indexing %s into %s backend...", repo, bk)
			start := time.Now()
			n, err = pipeline.IngestRepo(ctx, repo)
			if err != nil {
				log.Fatalf("ingestion into %s failed: %v", bk, err)
			}
			log.Printf("indexed %d chunks into %s in %s", n, bk, time.Since(start))
		} else {
			log.Printf("%s: %d chunks already indexed, skipping ingestion", bk, n)
		}

		state.backends[bk] = &backendInstance{
			name:   bk,
			store:  store,
			engine: engine,
			chunks: n,
		}
	}

	state.active = backendsToLoad[0]

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		cur := state.current()
		state.mu.RLock()
		available := make([]string, 0, len(state.backends))
		for k := range state.backends {
			available = append(available, k)
		}
		sort.Strings(available)
		switchable := len(available) > 1
		state.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"backend":    cur.name,
			"chunks":     cur.chunks,
			"backends":   available,
			"switchable": switchable,
		})
	})

	mux.HandleFunc("/api/backends", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()

		type backendInfo struct {
			Name   string `json:"name"`
			Chunks int    `json:"chunks"`
			Active bool   `json:"active"`
		}
		out := make([]backendInfo, 0, len(state.backends))
		for _, bi := range state.backends {
			out = append(out, backendInfo{
				Name:   bi.name,
				Chunks: bi.chunks,
				Active: bi.name == state.active,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/api/switch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Backend string `json:"backend"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Backend == "" {
			body.Backend = r.URL.Query().Get("backend")
		}
		if body.Backend == "" {
			http.Error(w, `{"error":"backend parameter required"}`, http.StatusBadRequest)
			return
		}

		bi, ok := state.switchTo(body.Backend)
		if !ok {
			http.Error(w, fmt.Sprintf(`{"error":"unknown backend %q"}`, body.Backend), http.StatusBadRequest)
			return
		}

		log.Printf("switched active backend to %s", body.Backend)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"backend": bi.name,
			"chunks":  bi.chunks,
		})
	})

	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query().Get("q")
		k := defaultK
		if q == "" && r.Method == http.MethodPost {
			var body struct {
				Query string `json:"query"`
				K     int    `json:"k"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				q = body.Query
				if body.K > 0 {
					k = body.K
				}
			}
		}
		if q == "" {
			http.Error(w, `{"error":"query parameter 'q' is required"}`, http.StatusBadRequest)
			return
		}

		if ks := r.URL.Query().Get("k"); ks != "" {
			if kv, err := strconv.Atoi(ks); err == nil && kv > 0 {
				k = kv
			}
		}

		cur := state.current()
		results, err := cur.engine.Search(r.Context(), q, k)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}

		type resultJSON struct {
			ID           string  `json:"id"`
			File         string  `json:"file"`
			StartLine    int     `json:"start_line"`
			EndLine      int     `json:"end_line"`
			Name         string  `json:"name"`
			Type         string  `json:"type"`
			VectorScore  float64 `json:"vector_score"`
			KeywordScore float64 `json:"keyword_score"`
			HybridScore  float64 `json:"hybrid_score"`
			Frontmatter  string  `json:"frontmatter"`
			Code         string  `json:"code"`
		}

		out := make([]resultJSON, len(results))
		for i, res := range results {
			out[i] = resultJSON{
				ID:           res.Chunk.ID,
				File:         res.Chunk.File,
				StartLine:    res.Chunk.StartLine,
				EndLine:      res.Chunk.EndLine,
				Name:         res.Chunk.Name,
				Type:         string(res.Chunk.Type),
				VectorScore:  res.VectorScore,
				KeywordScore: res.KeywordScore,
				HybridScore:  res.HybridScore,
				Frontmatter:  res.Chunk.Frontmatter,
				Code:         res.Chunk.Code,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"query":   q,
			"backend": cur.name,
			"results": out,
		})
	})

	mux.HandleFunc("/api/eval", func(w http.ResponseWriter, r *http.Request) {
		k := defaultK
		if ks := r.URL.Query().Get("k"); ks != "" {
			if kv, err := strconv.Atoi(ks); err == nil && kv > 0 {
				k = kv
			}
		}
		cur := state.current()
		summary, err := rag.EvalRelevance(r.Context(), cur.engine, rag.CuratedEvalQueries(), k)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(summary)
	})

	mux.HandleFunc("/api/explain", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query().Get("q")
		k := defaultK
		if q == "" && r.Method == http.MethodPost {
			var body struct {
				Query string `json:"query"`
				K     int    `json:"k"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				q = body.Query
				if body.K > 0 {
					k = body.K
				}
			}
		}
		if q == "" {
			http.Error(w, `{"error":"query parameter 'q' is required"}`, http.StatusBadRequest)
			return
		}

		if ks := r.URL.Query().Get("k"); ks != "" {
			if kv, err := strconv.Atoi(ks); err == nil && kv > 0 {
				k = kv
			}
		}

		cur := state.current()
		result, err := cur.engine.Explain(r.Context(), q, k)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}

		type citationJSON struct {
			Ref    string `json:"ref"`
			File   string `json:"file"`
			Start  int    `json:"start_line"`
			End    int    `json:"end_line"`
			Symbol string `json:"symbol"`
		}
		type symbolJSON struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			File        string `json:"file"`
			StartLine   int    `json:"start_line"`
			EndLine     int    `json:"end_line"`
			Frontmatter string `json:"frontmatter"`
			Explanation string `json:"explanation"`
		}

		citations := make([]citationJSON, len(result.Citations))
		for i, c := range result.Citations {
			citations[i] = citationJSON{
				Ref: c.Ref, File: c.File, Start: c.Start, End: c.End, Symbol: c.Symbol,
			}
		}
		symbols := make([]symbolJSON, len(result.Symbols))
		for i, s := range result.Symbols {
			symbols[i] = symbolJSON{
				Name: s.Name, Type: s.Type, File: s.File,
				StartLine: s.StartLine, EndLine: s.EndLine,
				Frontmatter: s.Frontmatter, Explanation: s.Explanation,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"query":     result.Query,
			"backend":   cur.name,
			"answer":    result.Answer,
			"citations": citations,
			"symbols":   symbols,
		})
	})

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticSub)))

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	shutdownDone := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("received %s, shutting down...", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("HTTP shutdown error: %v", err)
		}

		state.mu.RLock()
		for name, bi := range state.backends {
			if err := bi.store.Close(); err != nil {
				log.Printf("closing %s store: %v", name, err)
			}
		}
		state.mu.RUnlock()

		close(shutdownDone)
	}()

	log.Printf("listening on %s (UI at http://localhost%s)", addr, addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
	<-shutdownDone
	log.Println("shutdown complete")
}

func runBenchmark(ctx context.Context, backend, repo string, runs, topK int, reportPath string, evalRelevance bool, embedder rag.Embedder) {
	backends := []string{backend}
	if backend == "all" {
		backends = []string{"sqlite", "cozo"}
	}

	cfg := rag.BenchmarkConfig{
		Runs:     runs,
		TopK:     topK,
		Queries:  rag.DefaultBenchmarkConfig().Queries,
		RepoPath: repo,
	}
	if evalRelevance {
		cfg.EvalQueries = rag.CuratedEvalQueries()
	}

	for _, b := range backends {
		bk := b
		report, err := rag.RunBenchmark(ctx, cfg, embedder, func() (rag.VectorStore, string) {
			dbPath := fmt.Sprintf("legacylens_bench_%s.db", bk)
			return mustBenchStore(bk, embedder.Dimension(), dbPath), dbPath
		})
		if err != nil {
			log.Fatalf("benchmark %s failed: %v", bk, err)
		}

		fmt.Println(rag.FormatReportSummary(report))

		if reportPath != "" {
			outPath := reportPath
			if len(backends) > 1 {
				outPath = fmt.Sprintf("%s_%s.json", reportPath, bk)
			}
			if err := rag.WriteReport(report, outPath); err != nil {
				log.Fatalf("write report: %v", err)
			}
			fmt.Printf("Report written to %s\n", outPath)
		}
	}
}

func mustEmbedder(mode, url, model string, dim int, apiKey, cache string) rag.Embedder {
	if apiKey == "" {
		apiKey = os.Getenv("EMBED_API_KEY")
	}
	if cache == "" {
		cache = os.Getenv("EMBED_CACHE_DIR")
	}

	var base rag.Embedder
	switch mode {
	case "hash":
		base = rag.NewHashEmbedder(dim)
	case "local":
		fe, err := rag.NewFastEmbedder(cache)
		if err != nil {
			log.Fatalf("init local embedder: %v", err)
		}
		return fe // fastembed has its own model cache, skip CachedEmbedder
	case "http":
		if url == "" {
			log.Fatal("-embed-url is required when -embedder=http")
		}
		base = rag.NewHTTPEmbedder(rag.HTTPEmbedderConfig{
			Endpoint: url,
			Model:    model,
			Dim:      dim,
			APIKey:   apiKey,
		})
	default:
		log.Fatalf("unsupported embedder: %s (use hash, local, or http)", mode)
	}

	if cache != "" {
		cached, err := rag.NewCachedEmbedder(base, filepath.Clean(cache))
		if err != nil {
			log.Fatalf("init embedding cache: %v", err)
		}
		return cached
	}
	return base
}

func mustStore(name string, dim int) rag.VectorStore {
	switch name {
	case "sqlite":
		return rag.NewProductionSQLiteStore(dim, "legacylens.db")
	case "cozo":
		return rag.NewCozoStore(dim, "legacylens_cozo.db")
	default:
		log.Fatalf("unsupported backend: %s", name)
		return nil
	}
}

func mustBenchStore(name string, dim int, dbPath string) rag.VectorStore {
	switch name {
	case "sqlite":
		return rag.NewProductionSQLiteStore(dim, dbPath)
	case "cozo":
		return rag.NewCozoStore(dim, dbPath)
	default:
		log.Fatalf("unsupported backend: %s", name)
		return nil
	}
}
