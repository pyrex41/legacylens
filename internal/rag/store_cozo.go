package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	cozo "github.com/cozodb/cozo-lib-go"
)

// CozoStore is a production VectorStore backed by real CozoDB with native
// HNSW vector search and FTS. Uses cozo-lib-go CGo bindings.
type CozoStore struct {
	dim    int
	db     *cozo.CozoDB
	engine string
	path   string
	mu     sync.RWMutex
}

// NewCozoStore creates a CozoDB-backed VectorStore.
// For in-memory use pass "" as dbPath. For persistent storage pass a file path.
func NewCozoStore(dim int, dbPath string) *CozoStore {
	if dim <= 0 {
		dim = 384
	}
	engine := "sqlite"
	if dbPath == "" || dbPath == ":memory:" {
		engine = "mem"
		dbPath = ""
	}
	return &CozoStore{dim: dim, engine: engine, path: dbPath}
}

func (s *CozoStore) Name() string { return "cozo" }

func (s *CozoStore) Count(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, nil
	}
	res, err := s.db.Run("?[count(id)] := *nodes[id, _, _, _, _, _, _, _, _, _]", nil)
	if err != nil {
		return 0, err
	}
	if len(res.Rows) > 0 && len(res.Rows[0]) > 0 {
		return toInt(res.Rows[0][0]), nil
	}
	return 0, nil
}

func (s *CozoStore) Init(ctx context.Context) error {
	if s.db != nil {
		return nil
	}
	db, err := cozo.New(s.engine, s.path, nil)
	if err != nil {
		return fmt.Errorf("cozo open: %w", err)
	}
	s.db = &db

	// Check if relations already exist
	res, err := db.Run("::relations", nil)
	if err != nil {
		return fmt.Errorf("cozo list relations: %w", err)
	}
	existing := map[string]bool{}
	for _, row := range res.Rows {
		if len(row) > 0 {
			if name, ok := row[0].(string); ok {
				existing[name] = true
			}
		}
	}

	if !existing["nodes"] {
		_, err := db.Run(fmt.Sprintf(`
			:create nodes {
				id: String
				=>
				file: String,
				start_line: Int,
				end_line: Int,
				name: String,
				type: String,
				description: String,
				frontmatter: String,
				code: String,
				embedding: <F32; %d>
			}
		`, s.dim), nil)
		if err != nil {
			return fmt.Errorf("cozo create nodes: %w", err)
		}
	}

	if !existing["node_params"] {
		_, err := db.Run(`
			:create node_params {
				node_id: String,
				idx: Int
				=>
				name: String,
				type: String,
				intent: String,
				description: String
			}
		`, nil)
		if err != nil {
			return fmt.Errorf("cozo create node_params: %w", err)
		}
	}

	if !existing["node_skills"] {
		_, err := db.Run(`
			:create node_skills {
				node_id: String,
				skill: String
			}
		`, nil)
		if err != nil {
			return fmt.Errorf("cozo create node_skills: %w", err)
		}
	}

	if !existing["edges"] {
		_, err := db.Run(`
			:create edges {
				src_id: String,
				dst_id: String,
				relation: String
			}
		`, nil)
		if err != nil {
			return fmt.Errorf("cozo create edges: %w", err)
		}
	}

	// Create HNSW index if it doesn't exist
	if !existing["nodes:vec_idx"] {
		_, err := db.Run(fmt.Sprintf(`
			::hnsw create nodes:vec_idx {
				fields: [embedding],
				dim: %d,
				dtype: F32,
				distance: Cosine,
				m: 32,
				ef_construction: 64
			}
		`, s.dim), nil)
		if err != nil {
			return fmt.Errorf("cozo create hnsw index: %w", err)
		}
	}

	// Create FTS index if it doesn't exist
	if !existing["nodes:text_fts"] {
		_, err := db.Run(`
			::fts create nodes:text_fts {
				extractor: name ++ " " ++ code ++ " " ++ frontmatter ++ " " ++ description,
				tokenizer: Simple,
				filters: [Lowercase]
			}
		`, nil)
		if err != nil {
			return fmt.Errorf("cozo create fts index: %w", err)
		}
	}

	return nil
}

func (s *CozoStore) Upsert(ctx context.Context, chunk Chunk, vector []float32) error {
	if len(vector) != s.dim {
		return errors.New("embedding dimension mismatch")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Convert float32 to float64 for CozoDB
	vec64 := make([]float64, len(vector))
	for i, v := range vector {
		vec64[i] = float64(v)
	}

	params := cozo.Map{
		"id":          chunk.ID,
		"file":        chunk.File,
		"start_line":  chunk.StartLine,
		"end_line":    chunk.EndLine,
		"name":        chunk.Name,
		"type":        string(chunk.Type),
		"description": chunk.Description,
		"frontmatter": chunk.Frontmatter,
		"code":        chunk.Code,
		"embedding":   vec64,
	}

	_, err := s.db.Run(`
		?[id, file, start_line, end_line, name, type, description, frontmatter, code, embedding] <- [[
			$id, $file, $start_line, $end_line, $name, $type, $description, $frontmatter, $code, vec($embedding)
		]]
		:put nodes {
			id
			=>
			file,
			start_line,
			end_line,
			name,
			type,
			description,
			frontmatter,
			code,
			embedding
		}
	`, params)
	if err != nil {
		return fmt.Errorf("cozo upsert node: %w", err)
	}

	// Clear old params
	_, err = s.db.Run(`
		?[node_id, idx] := *node_params[node_id, idx, _, _, _, _], node_id = $id
		:rm node_params { node_id, idx }
	`, cozo.Map{"id": chunk.ID})
	if err != nil && !strings.Contains(err.Error(), "empty") {
		return fmt.Errorf("cozo clear params: %w", err)
	}

	// Insert params
	for i, p := range chunk.Parameters {
		_, err := s.db.Run(`
			?[node_id, idx, name, type, intent, description] <- [[
				$node_id, $idx, $name, $type, $intent, $description
			]]
			:put node_params {
				node_id, idx => name, type, intent, description
			}
		`, cozo.Map{
			"node_id":     chunk.ID,
			"idx":         i,
			"name":        p.Name,
			"type":        p.Type,
			"intent":      p.Intent,
			"description": p.Description,
		})
		if err != nil {
			return fmt.Errorf("cozo insert param: %w", err)
		}
	}

	// Clear old skills
	_, err = s.db.Run(`
		?[node_id, skill] := *node_skills[node_id, skill], node_id = $id
		:rm node_skills { node_id, skill }
	`, cozo.Map{"id": chunk.ID})
	if err != nil && !strings.Contains(err.Error(), "empty") {
		return fmt.Errorf("cozo clear skills: %w", err)
	}

	// Insert skills
	for _, sk := range chunk.Skills {
		_, err := s.db.Run(`
			?[node_id, skill] <- [[$node_id, $skill]]
			:put node_skills { node_id, skill }
		`, cozo.Map{"node_id": chunk.ID, "skill": sk})
		if err != nil {
			return fmt.Errorf("cozo insert skill: %w", err)
		}
	}

	return nil
}

func (s *CozoStore) VectorSearch(ctx context.Context, query []float32, k int) ([]SearchResult, error) {
	if len(query) != s.dim {
		return nil, errors.New("query embedding dimension mismatch")
	}
	if k <= 0 {
		k = 5
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Convert float32 to float64 for CozoDB
	q64 := make([]float64, len(query))
	for i, v := range query {
		q64[i] = float64(v)
	}

	res, err := s.db.Run(fmt.Sprintf(`
		?[dist, id] := ~nodes:vec_idx {
			id |
			query: vec($q),
			k: %d,
			ef: 50,
			bind_distance: dist
		}
		:order dist
		:limit %d
	`, k, k), cozo.Map{"q": q64})
	if err != nil {
		return nil, fmt.Errorf("cozo vector search: %w", err)
	}

	type distID struct {
		dist float64
		id   string
	}
	var hits []distID
	for _, row := range res.Rows {
		if len(row) < 2 {
			continue
		}
		hits = append(hits, distID{dist: toFloat64(row[0]), id: toString(row[1])})
	}

	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.id
	}
	chunks, err := s.loadNodes(ctx, ids)
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	for _, h := range hits {
		if ch, ok := chunks[h.id]; ok {
			results = append(results, SearchResult{Chunk: ch, VectorScore: 1.0 - h.dist})
		}
	}
	return results, nil
}

func (s *CozoStore) KeywordSearch(ctx context.Context, query string, k int) ([]SearchResult, error) {
	if k <= 0 {
		k = 5
	}
	cleanQ := sanitizeFTSQuery(query)
	if cleanQ == "" {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Extract tokens for name matching
	tokens := strings.Fields(strings.ToLower(cleanQ))

	// Phase 1: Exact and substring name matches get boosted
	var nameIDs []string
	for _, tok := range tokens {
		// Exact name match (case-insensitive)
		res, err := s.db.Run(`
			?[id] := *nodes[id, _, _, _, name, _, _, _, _, _],
				lowercase(name) == $q
		`, cozo.Map{"q": tok})
		if err == nil {
			for _, row := range res.Rows {
				if len(row) > 0 {
					nameIDs = append(nameIDs, toString(row[0]))
				}
			}
		}
		// Substring name match
		res, err = s.db.Run(`
			?[id] := *nodes[id, _, _, _, name, _, _, _, _, _],
				contains(lowercase(name), $q),
				!lowercase(name) == $q
		`, cozo.Map{"q": tok})
		if err == nil {
			for _, row := range res.Rows {
				if len(row) > 0 {
					nameIDs = append(nameIDs, toString(row[0]))
				}
			}
		}
	}

	// Phase 2: FTS content query
	res, err := s.db.Run(fmt.Sprintf(`
		?[score, id] := ~nodes:text_fts {
			id |
			query: $q,
			k: %d,
			bind_score: score
		}
		:order -score
		:limit %d
	`, k*2, k*2), cozo.Map{"q": cleanQ})
	if err != nil {
		return nil, fmt.Errorf("cozo keyword search: %w", err)
	}

	// Phase 3: Merge — name matches get large boost, dedup by ID
	type scored struct {
		id    string
		score float64
	}
	byID := map[string]*scored{}

	// Name matches get a synthetic high score
	for i, id := range nameIDs {
		if _, ok := byID[id]; !ok {
			// First name matches (exact) score higher than later ones (substring)
			byID[id] = &scored{id: id, score: 1000.0 - float64(i)}
		}
	}

	// FTS results fill remaining slots
	for _, row := range res.Rows {
		if len(row) < 2 {
			continue
		}
		score := toFloat64(row[0])
		id := toString(row[1])
		if existing, ok := byID[id]; ok {
			existing.score += score // boost further if also in FTS
		} else {
			byID[id] = &scored{id: id, score: score}
		}
	}

	// Collect and sort
	all := make([]scored, 0, len(byID))
	for _, s := range byID {
		all = append(all, *s)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })
	if len(all) > k {
		all = all[:k]
	}

	// Batch load nodes
	ids := make([]string, len(all))
	for i, s := range all {
		ids[i] = s.id
	}
	chunks, err := s.loadNodes(ctx, ids)
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(all))
	for _, s := range all {
		if ch, ok := chunks[s.id]; ok {
			results = append(results, SearchResult{Chunk: ch, KeywordScore: s.score})
		}
	}
	return results, nil
}




// UpsertEdges batch-inserts edges into the CozoDB edges relation.
func (s *CozoStore) UpsertEdges(ctx context.Context, edges []Edge) error {
	if len(edges) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build rows for batch insert
	rows := make([]any, len(edges))
	for i, e := range edges {
		rows[i] = []any{e.SrcID, e.DstID, e.Relation}
	}

	_, err := s.db.Run(`
		?[src_id, dst_id, relation] <- $rows
		:put edges { src_id, dst_id, relation }
	`, cozo.Map{"rows": rows})
	if err != nil {
		return fmt.Errorf("cozo upsert edges: %w", err)
	}
	return nil
}

// BuildEdges queries all nodes from CozoDB, runs extraction functions,
// and batch-inserts the resulting edges.
func (s *CozoStore) BuildEdges(ctx context.Context) error {
	s.mu.RLock()
	// Load all chunks for edge extraction
	res, err := s.db.Run(`
		?[id, file, start_line, end_line, name, type, description, frontmatter, code] :=
			*nodes[id, file, start_line, end_line, name, type, description, frontmatter, code, _]
	`, nil)
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("cozo load all nodes for edges: %w", err)
	}

	var chunks []Chunk
	for _, row := range res.Rows {
		if len(row) < 9 {
			continue
		}
		chunks = append(chunks, Chunk{
			ID:          toString(row[0]),
			File:        toString(row[1]),
			StartLine:   toInt(row[2]),
			EndLine:     toInt(row[3]),
			Name:        toString(row[4]),
			Type:        ChunkType(toString(row[5])),
			Description: toString(row[6]),
			Frontmatter: toString(row[7]),
			Code:        toString(row[8]),
		})
	}

	if len(chunks) == 0 {
		return nil
	}

	nameIndex := BuildNameIndex(chunks)

	var allEdges []Edge
	allEdges = append(allEdges, ExtractContainmentEdges(chunks)...)
	allEdges = append(allEdges, ExtractCallEdges(chunks, nameIndex)...)
	allEdges = append(allEdges, ExtractDocEdges(chunks, nameIndex)...)

	return s.UpsertEdges(ctx, allEdges)
}

func (s *CozoStore) HybridSearch(ctx context.Context, queryText string, queryVec []float32, k int) ([]SearchResult, error) {
	if len(queryVec) != s.dim {
		return nil, errors.New("query embedding dimension mismatch")
	}
	if k <= 0 {
		k = 5
	}

	cleanQ := sanitizeFTSQuery(queryText)
	tokens := strings.Fields(strings.ToLower(cleanQ))
	if len(tokens) == 0 {
		// Fall back to pure vector search
		return s.VectorSearch(ctx, queryVec, k)
	}

	pool := k * 5
	if pool < 20 {
		pool = 20
	}

	// Convert float32 to float64 for CozoDB
	q64 := make([]float64, len(queryVec))
	for i, v := range queryVec {
		q64[i] = float64(v)
	}

	// Build token relation rows for Datalog
	tokRows := make([]any, len(tokens))
	for i, t := range tokens {
		tokRows[i] = []any{t}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Unified Datalog query: vector + FTS + name matching + graph expansion + skill boost
	//
	// Strategy: collect all score contributions as (id, tag, score) tuples,
	// aggregate max per (id, tag), then sum across tags for final score.
	// CozoDB requires variables (not literals) in rule heads, so we bind
	// tag strings via inline relations.
	query := fmt.Sprintf(`
		tok_rel[tok] <- $tokens
		tag_v[t] <- [["v"]]
		tag_f[t] <- [["f"]]
		tag_n[t] <- [["n"]]
		tag_s[t] <- [["s"]]

		# Vector candidates
		scored[id, t, score] := ~nodes:vec_idx {
			id |
			query: vec($q),
			k: %d,
			ef: 50,
			bind_distance: dist
		}, score = 1.0 - dist, tag_v[t]

		# FTS candidates (BM25 ~1-20, scale to ~0-1)
		scored[id, t, score] := ~nodes:text_fts {
			id |
			query: $fts_q,
			k: %d,
			bind_score: raw
		}, score = raw * 0.05, tag_f[t]

		# Name matching: exact name match gets 1.0
		scored[id, t, score] := *nodes[id, _, _, _, name, _, _, _, _, _],
			tok_rel[tok], lowercase(name) == tok, score = 1.0, tag_n[t]

		# Skill boost: +0.15 for matching skills (exact match)
		scored[id, t, sb] := scored[id, _, _],
			*node_skills[id, skill],
			tok_rel[tok], lowercase(skill) == tok, sb = 0.15, tag_s[t]

		# Max score per (id, tag), then sum across tags = direct score
		best[id, tag, max(s)] := scored[id, tag, s]
		direct[id, sum(s)] := best[id, _, s]

		# Graph expansion: 1-hop neighbors via edges (bidirectional)
		neighbor[nb, src] := direct[src, _], *edges[src, nb, _]
		neighbor[nb, src] := direct[src, _], *edges[nb, src, _]

		# Graph bonus: max(parent_score * 0.3) to prevent hub domination
		graph_bonus[nb, max(bonus)] := neighbor[nb, src],
			direct[src, ds], bonus = ds * 0.3

		# Final: direct + optional graph bonus; graph-only neighbors
		final[id, total] := direct[id, ds], graph_bonus[id, g], total = ds + g
		final[id, total] := direct[id, ds], not graph_bonus[id, _], total = ds
		final[id, total] := graph_bonus[id, g], not direct[id, _], total = g

		?[total, id] := final[id, total]
		:order -total
		:limit %d
	`, pool, pool, k)

	res, err := s.db.Run(query, cozo.Map{
		"q":      q64,
		"fts_q":  cleanQ,
		"tokens": tokRows,
	})
	if err != nil {
		// Fall back to RRF fusion if Datalog query fails
		vec, verr := s.VectorSearch(ctx, queryVec, k*3)
		if verr != nil {
			return nil, fmt.Errorf("cozo hybrid query failed (%v), vector fallback also failed: %w", err, verr)
		}
		key, kerr := s.KeywordSearch(ctx, queryText, k*3)
		if kerr != nil {
			return nil, fmt.Errorf("cozo hybrid query failed (%v), keyword fallback also failed: %w", err, kerr)
		}
		results := FuseRRF(vec, key, k)
		if ClassifyQuery(queryText) == IntentOverview {
			results = applyDocBoost(results, k)
		}
		return results, nil
	}

	// Extract IDs and scores
	type hit struct {
		score float64
		id    string
	}
	var hits []hit
	for _, row := range res.Rows {
		if len(row) < 2 {
			continue
		}
		hits = append(hits, hit{score: toFloat64(row[0]), id: toString(row[1])})
	}

	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.id
	}
	chunks, err := s.loadNodes(ctx, ids)
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	for _, h := range hits {
		if ch, ok := chunks[h.id]; ok {
			results = append(results, SearchResult{Chunk: ch, HybridScore: h.score})
		}
	}

	// Apply doc-type boost for overview queries, then re-sort and trim
	if ClassifyQuery(queryText) == IntentOverview {
		results = applyDocBoost(results, k)
	}

	return results, nil
}

func (s *CozoStore) Close() error {
	if s.db != nil {
		s.db.Close()
		s.db = nil
	}
	return nil
}

// loadNode reconstructs a full Chunk from the CozoDB relations.
func (s *CozoStore) loadNode(ctx context.Context, id string) (Chunk, error) {
	m, err := s.loadNodes(ctx, []string{id})
	if err != nil {
		return Chunk{}, err
	}
	ch, ok := m[id]
	if !ok {
		return Chunk{}, fmt.Errorf("cozo node not found: %s", id)
	}
	return ch, nil
}

// loadNodes batch-loads multiple chunks in 3 queries instead of 3*N.
func (s *CozoStore) loadNodes(ctx context.Context, ids []string) (map[string]Chunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build ID list for CozoScript
	idList := make([]any, len(ids))
	for i, id := range ids {
		idList[i] = id
	}

	// Batch load nodes
	res, err := s.db.Run(`
		?[id, file, start_line, end_line, name, type, description, frontmatter, code] :=
			*nodes[id, file, start_line, end_line, name, type, description, frontmatter, code, _],
			id in $ids
	`, cozo.Map{"ids": idList})
	if err != nil {
		return nil, fmt.Errorf("cozo batch load nodes: %w", err)
	}

	chunks := make(map[string]Chunk, len(res.Rows))
	for _, row := range res.Rows {
		if len(row) < 9 {
			continue
		}
		id := toString(row[0])
		chunks[id] = Chunk{
			ID:          id,
			File:        toString(row[1]),
			StartLine:   toInt(row[2]),
			EndLine:     toInt(row[3]),
			Name:        toString(row[4]),
			Type:        ChunkType(toString(row[5])),
			Description: toString(row[6]),
			Frontmatter: toString(row[7]),
			Code:        toString(row[8]),
		}
	}

	// Batch load params
	paramRes, err := s.db.Run(`
		?[node_id, idx, name, type, intent, description] :=
			*node_params[node_id, idx, name, type, intent, description],
			node_id in $ids
		:order node_id, idx
	`, cozo.Map{"ids": idList})
	if err != nil {
		return nil, fmt.Errorf("cozo batch load params: %w", err)
	}
	for _, pr := range paramRes.Rows {
		if len(pr) < 6 {
			continue
		}
		nid := toString(pr[0])
		if ch, ok := chunks[nid]; ok {
			ch.Parameters = append(ch.Parameters, Parameter{
				Name:        toString(pr[2]),
				Type:        toString(pr[3]),
				Intent:      toString(pr[4]),
				Description: toString(pr[5]),
			})
			chunks[nid] = ch
		}
	}

	// Batch load skills
	skillRes, err := s.db.Run(`
		?[node_id, skill] :=
			*node_skills[node_id, skill],
			node_id in $ids
		:order node_id, skill
	`, cozo.Map{"ids": idList})
	if err != nil {
		return nil, fmt.Errorf("cozo batch load skills: %w", err)
	}
	for _, sr := range skillRes.Rows {
		if len(sr) < 2 {
			continue
		}
		nid := toString(sr[0])
		if ch, ok := chunks[nid]; ok {
			ch.Skills = append(ch.Skills, toString(sr[1]))
			chunks[nid] = ch
		}
	}

	return chunks, nil
}

// sanitizeFTSQuery strips non-alphanumeric characters from a query string
// to produce valid CozoDB FTS query tokens.
func sanitizeFTSQuery(q string) string {
	tokens := strings.Fields(q)
	var clean []string
	for _, t := range tokens {
		t = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				return r
			}
			return -1
		}, t)
		if t != "" {
			clean = append(clean, t)
		}
	}
	return strings.Join(clean, " ")
}

// Type conversion helpers for CozoDB result rows.
func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	case int:
		return float64(val)
	default:
		return 0
	}
}

func toInt(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	case json.Number:
		n, _ := val.Int64()
		return int(n)
	default:
		return 0
	}
}

