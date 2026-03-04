package rag

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// SQLiteStore is a production VectorStore backed by SQLite with FTS5 for
// keyword search and in-process cosine-similarity kNN for vector search.
// Vectors are stored as little-endian float32 BLOBs alongside chunk metadata.
type SQLiteStore struct {
	dim  int
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// NewProductionSQLiteStore creates a SQLite-backed VectorStore.
// Pass ":memory:" for an ephemeral in-process database or a file path for
// persistent storage.
func NewProductionSQLiteStore(dim int, dbPath string) *SQLiteStore {
	if dim <= 0 {
		dim = 384
	}
	if dbPath == "" {
		dbPath = ":memory:"
	}
	return &SQLiteStore{dim: dim, path: dbPath}
}

func (s *SQLiteStore) Name() string { return "sqlite" }

func (s *SQLiteStore) Count(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chunks").Scan(&n)
	return n, err
}

func (s *SQLiteStore) Init(ctx context.Context) error {
	if s.db != nil {
		return nil
	}
	dsn := s.path
	if dsn != ":memory:" && !strings.Contains(dsn, "?") {
		dsn += "?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("sqlite open: %w", err)
	}
	db.SetMaxOpenConns(1)
	s.db = db

	ddl := `
		CREATE TABLE IF NOT EXISTS chunks (
			id         TEXT PRIMARY KEY,
			file       TEXT NOT NULL,
			start_line INTEGER NOT NULL,
			end_line   INTEGER NOT NULL,
			name       TEXT NOT NULL,
			type       TEXT NOT NULL,
			params     TEXT NOT NULL DEFAULT '',
			skills     TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			frontmatter TEXT NOT NULL DEFAULT '',
			code       TEXT NOT NULL DEFAULT '',
			vector     BLOB
		);

		CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
			name, code, frontmatter, description,
			content=chunks,
			content_rowid=rowid
		);

		CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
			INSERT INTO chunks_fts(rowid, name, code, frontmatter, description)
			VALUES (new.rowid, new.name, new.code, new.frontmatter, new.description);
		END;

		CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
			INSERT INTO chunks_fts(chunks_fts, rowid, name, code, frontmatter, description)
			VALUES ('delete', old.rowid, old.name, old.code, old.frontmatter, old.description);
		END;

		CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
			INSERT INTO chunks_fts(chunks_fts, rowid, name, code, frontmatter, description)
			VALUES ('delete', old.rowid, old.name, old.code, old.frontmatter, old.description);
			INSERT INTO chunks_fts(rowid, name, code, frontmatter, description)
			VALUES (new.rowid, new.name, new.code, new.frontmatter, new.description);
		END;
	`
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("sqlite ddl: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Upsert(ctx context.Context, chunk Chunk, vector []float32) error {
	if len(vector) != s.dim {
		return errors.New("embedding dimension mismatch")
	}
	blob := encodeVector(vector)
	params := encodeStringList(paramStrings(chunk.Parameters))
	skills := encodeStringList(chunk.Skills)

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chunks (id, file, start_line, end_line, name, type, params, skills, description, frontmatter, code, vector)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			file=excluded.file, start_line=excluded.start_line, end_line=excluded.end_line,
			name=excluded.name, type=excluded.type, params=excluded.params, skills=excluded.skills,
			description=excluded.description, frontmatter=excluded.frontmatter,
			code=excluded.code, vector=excluded.vector
	`,
		chunk.ID, chunk.File, chunk.StartLine, chunk.EndLine,
		chunk.Name, string(chunk.Type), params, skills,
		chunk.Description, chunk.Frontmatter, chunk.Code, blob,
	)
	if err != nil {
		return fmt.Errorf("sqlite upsert: %w", err)
	}
	return nil
}

func (s *SQLiteStore) VectorSearch(ctx context.Context, query []float32, k int) ([]SearchResult, error) {
	if len(query) != s.dim {
		return nil, errors.New("query embedding dimension mismatch")
	}
	if k <= 0 {
		k = 5
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, file, start_line, end_line, name, type, params, skills,
		       description, frontmatter, code, vector
		FROM chunks WHERE vector IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("sqlite vector scan: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		ch, vec, err := scanChunkRow(rows)
		if err != nil {
			return nil, err
		}
		score := cosine(query, vec)
		results = append(results, SearchResult{Chunk: ch, VectorScore: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool { return results[i].VectorScore > results[j].VectorScore })
	if len(results) > k {
		results = results[:k]
	}
	return results, nil
}

func (s *SQLiteStore) KeywordSearch(ctx context.Context, query string, k int) ([]SearchResult, error) {
	if k <= 0 {
		k = 5
	}
	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.file, c.start_line, c.end_line, c.name, c.type,
		       c.params, c.skills, c.description, c.frontmatter, c.code,
		       c.vector, bm25(chunks_fts, 10.0, 1.0, 2.0, 1.0) AS score
		FROM chunks_fts f
		JOIN chunks c ON c.rowid = f.rowid
		WHERE chunks_fts MATCH ?
		ORDER BY score
		LIMIT ?
	`, ftsQuery, k)
	if err != nil {
		return nil, fmt.Errorf("sqlite keyword search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var (
			id, file, name, typ, params, skills string
			desc, fm, code                      string
			startLine, endLine                  int
			vecBlob                             []byte
			bm25Score                           float64
		)
		if err := rows.Scan(&id, &file, &startLine, &endLine, &name, &typ,
			&params, &skills, &desc, &fm, &code, &vecBlob, &bm25Score); err != nil {
			return nil, fmt.Errorf("sqlite keyword scan: %w", err)
		}
		ch := Chunk{
			ID: id, File: file, StartLine: startLine, EndLine: endLine,
			Name: name, Type: ChunkType(typ),
			Parameters:  decodeParams(params),
			Skills:      decodeStringList(skills),
			Description: desc, Frontmatter: fm, Code: code,
		}
		results = append(results, SearchResult{
			Chunk:        ch,
			KeywordScore: -bm25Score, // bm25() returns negative; negate for ranking
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *SQLiteStore) HybridSearch(ctx context.Context, queryText string, queryVec []float32, k int) ([]SearchResult, error) {
	vec, err := s.VectorSearch(ctx, queryVec, k*3)
	if err != nil {
		return nil, err
	}
	key, err := s.KeywordSearch(ctx, queryText, k*3)
	if err != nil {
		return nil, err
	}
	results := FuseRRF(vec, key, k)
	if ClassifyQuery(queryText) == IntentOverview {
		docResults, derr := s.docSearch(ctx, queryText, k)
		if derr == nil && len(docResults) > 0 {
			results = mergeDocResults(results, docResults, k)
		}
	}
	return results, nil
}

// docSearch finds doc-type chunks matching the query via FTS, for overview queries.
func (s *SQLiteStore) docSearch(ctx context.Context, query string, k int) ([]SearchResult, error) {
	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.file, c.start_line, c.end_line, c.name, c.type,
		       c.params, c.skills, c.description, c.frontmatter, c.code,
		       c.vector, bm25(chunks_fts, 10.0, 1.0, 2.0, 1.0) AS score
		FROM chunks_fts f
		JOIN chunks c ON c.rowid = f.rowid
		WHERE chunks_fts MATCH ?
		  AND c.type = 'doc'
		ORDER BY score
		LIMIT ?
	`, ftsQuery, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var (
			id, file, name, typ, params, skills string
			desc, fm, code                      string
			startLine, endLine                  int
			vecBlob                             []byte
			bm25Score                           float64
		)
		if err := rows.Scan(&id, &file, &startLine, &endLine, &name, &typ,
			&params, &skills, &desc, &fm, &code, &vecBlob, &bm25Score); err != nil {
			return nil, err
		}
		ch := Chunk{
			ID: id, File: file, StartLine: startLine, EndLine: endLine,
			Name: name, Type: ChunkType(typ),
			Parameters:  decodeParams(params),
			Skills:      decodeStringList(skills),
			Description: desc, Frontmatter: fm, Code: code,
		}
		results = append(results, SearchResult{Chunk: ch, KeywordScore: -bm25Score})
	}
	return results, rows.Err()
}

// mergeDocResults inserts doc results into the top of existing results.
// Doc results get a boosted hybrid score to rank above non-doc results.
func mergeDocResults(existing, docs []SearchResult, k int) []SearchResult {
	// Find max existing score for baseline
	maxScore := 0.0
	for _, r := range existing {
		if r.HybridScore > maxScore {
			maxScore = r.HybridScore
		}
	}

	// Doc results get score above the max existing, preserving their relative order
	seen := map[string]bool{}
	var merged []SearchResult
	for i, d := range docs {
		d.HybridScore = (maxScore + 1.0) * DocBoost / (1.0 + float64(i)*0.1)
		merged = append(merged, d)
		seen[d.Chunk.ID] = true
	}
	for _, r := range existing {
		if !seen[r.Chunk.ID] {
			merged = append(merged, r)
		}
	}
	if len(merged) > k {
		merged = merged[:k]
	}
	return merged
}

func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// --- encoding helpers ---

func encodeVector(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeVector(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func encodeStringList(ss []string) string {
	return strings.Join(ss, "\x1f")
}

func decodeStringList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x1f")
}

func paramStrings(ps []Parameter) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name + "\x1e" + p.Type + "\x1e" + p.Intent + "\x1e" + p.Description
	}
	return out
}

func decodeParams(s string) []Parameter {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\x1f")
	out := make([]Parameter, 0, len(parts))
	for _, p := range parts {
		fields := strings.SplitN(p, "\x1e", 4)
		param := Parameter{}
		if len(fields) > 0 {
			param.Name = fields[0]
		}
		if len(fields) > 1 {
			param.Type = fields[1]
		}
		if len(fields) > 2 {
			param.Intent = fields[2]
		}
		if len(fields) > 3 {
			param.Description = fields[3]
		}
		out = append(out, param)
	}
	return out
}

func scanChunkRow(rows *sql.Rows) (Chunk, []float32, error) {
	var (
		id, file, name, typ, params, skills string
		desc, fm, code                      string
		startLine, endLine                  int
		vecBlob                             []byte
	)
	if err := rows.Scan(&id, &file, &startLine, &endLine, &name, &typ,
		&params, &skills, &desc, &fm, &code, &vecBlob); err != nil {
		return Chunk{}, nil, fmt.Errorf("sqlite scan: %w", err)
	}
	ch := Chunk{
		ID: id, File: file, StartLine: startLine, EndLine: endLine,
		Name: name, Type: ChunkType(typ),
		Parameters:  decodeParams(params),
		Skills:      decodeStringList(skills),
		Description: desc, Frontmatter: fm, Code: code,
	}
	return ch, decodeVector(vecBlob), nil
}

// buildFTSQuery converts a natural-language query into an FTS5 query.
// Each token is OR-joined with prefix matching for partial matches.
func buildFTSQuery(q string) string {
	tokens := tokenize(q)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, t := range tokens {
		cleaned := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
				return r
			}
			return -1
		}, t)
		if cleaned != "" {
			parts = append(parts, cleaned+"*")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " OR ")
}
