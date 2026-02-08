package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"openbot/internal/domain"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements domain.MemoryStore using SQLite with read/write connection splitting.
type SQLiteStore struct {
	writer *sql.DB // single-writer connection
	reader *sql.DB // reader pool (for concurrent reads)
	logger *slog.Logger
}

func NewSQLiteStore(dbPath string, logger *slog.Logger) (*SQLiteStore, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create database directory %s: %w", dir, err)
	}

	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"

	// Writer: single connection for serialized writes
	writer, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("cannot open writer database: %w", err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)

	// Reader: multiple connections for concurrent reads
	reader, err := sql.Open("sqlite", dsn+"&mode=ro")
	if err != nil {
		writer.Close()
		return nil, fmt.Errorf("cannot open reader database: %w", err)
	}
	reader.SetMaxOpenConns(4)
	reader.SetMaxIdleConns(4)

	// Apply PRAGMA optimizations on writer
	pragmas := []string{
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -8000",   // 8MB cache
		"PRAGMA mmap_size = 268435456", // 256MB mmap
		"PRAGMA temp_store = MEMORY",
	}
	for _, p := range pragmas {
		if _, err := writer.Exec(p); err != nil {
			logger.Warn("pragma failed", "pragma", p, "err", err)
		}
	}

	store := &SQLiteStore{writer: writer, reader: reader, logger: logger}

	// Run versioned migrations via the new migration system.
	if err := RunMigrations(writer, logger); err != nil {
		writer.Close()
		reader.Close()
		return nil, fmt.Errorf("database migration failed: %w", err)
	}

	// Legacy compatibility: ensure old FTS5 tables and metrics tables still exist
	// (these were in the old v2 migration but not in the new versioned migrations
	// as they use a different schema). Apply idempotently.
	legacyDDLs := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS chunks USING fts5(
			document_id, chunk_index, content,
			tokenize='porter unicode61'
		)`,
		`CREATE TABLE IF NOT EXISTS metrics_hourly (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			metric_name TEXT NOT NULL,
			value       REAL DEFAULT 0,
			labels      TEXT DEFAULT '{}',
			bucket_time DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_bucket ON metrics_hourly(metric_name, bucket_time)`,
		`CREATE TABLE IF NOT EXISTS skills (
			name        TEXT PRIMARY KEY,
			description TEXT DEFAULT '',
			version     TEXT DEFAULT '1.0',
			definition  TEXT NOT NULL,
			built_in    INTEGER DEFAULT 0,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, ddl := range legacyDDLs {
		if _, err := writer.Exec(ddl); err != nil {
			logger.Debug("legacy DDL skipped", "err", err)
		}
	}

	return store, nil
}

func (s *SQLiteStore) CreateConversation(ctx context.Context, conv domain.Conversation) error {
	now := time.Now()
	if conv.CreatedAt.IsZero() {
		conv.CreatedAt = now
	}
	if conv.UpdatedAt.IsZero() {
		conv.UpdatedAt = now
	}
	_, err := s.writer.ExecContext(ctx,
		`INSERT OR IGNORE INTO conversations (id, title, provider, model, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		conv.ID, conv.Title, conv.Provider, conv.Model, conv.CreatedAt, conv.UpdatedAt,
	)
	return err
}

func (s *SQLiteStore) GetConversation(ctx context.Context, id string) (*domain.Conversation, error) {
	var conv domain.Conversation
	err := s.reader.QueryRowContext(ctx,
		`SELECT id, title, provider, model, created_at, updated_at FROM conversations WHERE id = ?`, id,
	).Scan(&conv.ID, &conv.Title, &conv.Provider, &conv.Model, &conv.CreatedAt, &conv.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &conv, nil
}

func (s *SQLiteStore) UpdateConversation(ctx context.Context, conv domain.Conversation) error {
	conv.UpdatedAt = time.Now()
	_, err := s.writer.ExecContext(ctx,
		`UPDATE conversations SET title=?, provider=?, model=?, updated_at=? WHERE id=?`,
		conv.Title, conv.Provider, conv.Model, conv.UpdatedAt, conv.ID,
	)
	return err
}

func (s *SQLiteStore) ListConversations(ctx context.Context, limit int) ([]domain.Conversation, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.reader.QueryContext(ctx,
		`SELECT id, title, provider, model, created_at, updated_at
		 FROM conversations ORDER BY updated_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []domain.Conversation
	for rows.Next() {
		var c domain.Conversation
		if err := rows.Scan(&c.ID, &c.Title, &c.Provider, &c.Model, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		convs = append(convs, c)
	}
	return convs, rows.Err()
}

func (s *SQLiteStore) AddMessage(ctx context.Context, convID string, msg domain.MessageRecord) error {
	now := time.Now()
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = now
	}
	_, err := s.writer.ExecContext(ctx,
		`INSERT INTO messages (conversation_id, role, content, tool_calls, tool_call_id, tool_name, tokens_in, tokens_out, provider, model, latency_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		convID, msg.Role, msg.Content, msg.ToolCalls, msg.ToolCallID, msg.ToolName,
		msg.TokensIn, msg.TokensOut, msg.Provider, msg.Model, msg.LatencyMs, msg.CreatedAt,
	)
	if err != nil {
		return err
	}

	if _, err := s.writer.ExecContext(ctx,
		`UPDATE conversations SET updated_at = ? WHERE id = ?`, now, convID,
	); err != nil {
		s.logger.Warn("failed to update conversation timestamp", "convID", convID, "err", err)
	}
	return nil
}

func (s *SQLiteStore) GetMessages(ctx context.Context, convID string, limit int) ([]domain.MessageRecord, error) {
	if limit <= 0 {
		limit = 100
	}

	// Get last N messages, ordered oldest first
	rows, err := s.reader.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, tool_calls, tool_call_id, tool_name,
		        tokens_in, tokens_out, provider, model, latency_ms, created_at
		 FROM messages WHERE conversation_id = ?
		 ORDER BY created_at DESC LIMIT ?`, convID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []domain.MessageRecord
	for rows.Next() {
		var m domain.MessageRecord
		var toolCalls, toolCallID, toolName, provider, model sql.NullString
		var latencyMs sql.NullInt64
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content,
			&toolCalls, &toolCallID, &toolName,
			&m.TokensIn, &m.TokensOut, &provider, &model, &latencyMs, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.ToolCalls = toolCalls.String
		m.ToolCallID = toolCallID.String
		m.ToolName = toolName.String
		m.Provider = provider.String
		m.Model = model.String
		m.LatencyMs = latencyMs.Int64
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse to chronological order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

func (s *SQLiteStore) SaveMemory(ctx context.Context, mem domain.MemoryEntry) error {
	now := time.Now()
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = now
	}
	_, err := s.writer.ExecContext(ctx,
		`INSERT INTO memories (category, content, source, importance, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		mem.Category, mem.Content, mem.Source, mem.Importance, mem.CreatedAt, mem.ExpiresAt,
	)
	return err
}

func (s *SQLiteStore) SearchMemories(ctx context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	if limit <= 0 {
		limit = 10
	}

	// Simple keyword search using LIKE
	pattern := "%" + query + "%"
	rows, err := s.reader.QueryContext(ctx,
		`SELECT id, category, content, source, importance, created_at, expires_at
		 FROM memories
		 WHERE content LIKE ? AND (expires_at IS NULL OR expires_at > ?)
		 ORDER BY importance DESC, created_at DESC
		 LIMIT ?`,
		pattern, time.Now(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMemories(rows)
}

func (s *SQLiteStore) GetRecentMemories(ctx context.Context, limit int) ([]domain.MemoryEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.reader.QueryContext(ctx,
		`SELECT id, category, content, source, importance, created_at, expires_at
		 FROM memories
		 WHERE expires_at IS NULL OR expires_at > ?
		 ORDER BY created_at DESC LIMIT ?`,
		time.Now(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMemories(rows)
}

func scanMemories(rows *sql.Rows) ([]domain.MemoryEntry, error) {
	var mems []domain.MemoryEntry
	for rows.Next() {
		var m domain.MemoryEntry
		var expiresAt sql.NullTime
		if err := rows.Scan(&m.ID, &m.Category, &m.Content, &m.Source,
			&m.Importance, &m.CreatedAt, &expiresAt); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			m.ExpiresAt = &expiresAt.Time
		}
		mems = append(mems, m)
	}
	return mems, rows.Err()
}

// DeleteConversation removes a conversation and all its messages.
func (s *SQLiteStore) DeleteConversation(ctx context.Context, id string) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// MessageCount returns the total number of messages.
func (s *SQLiteStore) MessageCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&count)
	return count, err
}

// ConversationCount returns the total number of conversations.
func (s *SQLiteStore) ConversationCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversations`).Scan(&count)
	return count, err
}

// --- Knowledge Store methods ---

func (s *SQLiteStore) AddDocument(ctx context.Context, doc domain.Document, chunks []domain.DocumentChunk) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO documents (id, name, mime_type, size, chunk_count, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.Name, doc.MimeType, doc.Size, doc.ChunkCount, doc.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}

	for _, c := range chunks {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO chunks (document_id, chunk_index, content) VALUES (?, ?, ?)`,
			c.DocumentID, c.ChunkIndex, c.Content,
		)
		if err != nil {
			return fmt.Errorf("insert chunk %d: %w", c.ChunkIndex, err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) SearchKnowledge(ctx context.Context, query string, topK int) ([]domain.KnowledgeSearchResult, error) {
	if topK <= 0 {
		topK = 5
	}
	rows, err := s.reader.QueryContext(ctx,
		`SELECT c.document_id, c.chunk_index, c.content, d.name,
		        rank
		 FROM chunks c
		 JOIN documents d ON d.id = c.document_id
		 WHERE chunks MATCH ?
		 ORDER BY rank
		 LIMIT ?`, query, topK,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []domain.KnowledgeSearchResult
	for rows.Next() {
		var r domain.KnowledgeSearchResult
		var rank float64
		if err := rows.Scan(&r.Chunk.DocumentID, &r.Chunk.ChunkIndex, &r.Chunk.Content, &r.DocName, &rank); err != nil {
			return nil, err
		}
		r.Score = -rank // FTS5 rank is negative (lower = better)
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *SQLiteStore) ListDocuments(ctx context.Context) ([]domain.Document, error) {
	rows, err := s.reader.QueryContext(ctx,
		`SELECT id, name, mime_type, size, chunk_count, created_at FROM documents ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []domain.Document
	for rows.Next() {
		var d domain.Document
		if err := rows.Scan(&d.ID, &d.Name, &d.MimeType, &d.Size, &d.ChunkCount, &d.CreatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

func (s *SQLiteStore) DeleteDocument(ctx context.Context, id string) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) LogAudit(ctx context.Context, entry domain.AuditEntry) error {
	_, err := s.writer.ExecContext(ctx,
		`INSERT INTO audit_log (action, tool_name, command, result, details)
		 VALUES (?, ?, ?, ?, ?)`,
		entry.Action, entry.ToolName, entry.Command, entry.Result, entry.Details,
	)
	return err
}

func (s *SQLiteStore) Close() error {
	var firstErr error
	if err := s.reader.Close(); err != nil {
		firstErr = err
	}
	if err := s.writer.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// WriterDB returns the writer *sql.DB for use by components that need to write to the same
// database (e.g. attachments table). Callers must not hold the connection for long.
func (s *SQLiteStore) WriterDB() *sql.DB {
	return s.writer
}
