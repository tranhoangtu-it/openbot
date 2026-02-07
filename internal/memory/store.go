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

// SQLiteStore implements domain.MemoryStore using SQLite.
type SQLiteStore struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewSQLiteStore(dbPath string, logger *slog.Logger) (*SQLiteStore, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create database directory %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("cannot open database: %w", err)
	}

	// Set connection pool (single connection for SQLite)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &SQLiteStore{db: db, logger: logger}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("database migration failed: %w", err)
	}

	return store, nil
}

func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS conversations (
		id          TEXT PRIMARY KEY,
		title       TEXT,
		provider    TEXT,
		model       TEXT,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS messages (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
		role            TEXT NOT NULL,
		content         TEXT,
		tool_calls      TEXT,
		tool_call_id    TEXT,
		tool_name       TEXT,
		tokens_in       INTEGER DEFAULT 0,
		tokens_out      INTEGER DEFAULT 0,
		created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages(conversation_id, created_at);

	CREATE TABLE IF NOT EXISTS memories (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		category    TEXT NOT NULL,
		content     TEXT NOT NULL,
		source      TEXT,
		importance  INTEGER DEFAULT 5,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at  DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_memories_cat ON memories(category);

	CREATE TABLE IF NOT EXISTS audit_log (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		action      TEXT NOT NULL,
		tool_name   TEXT,
		command     TEXT,
		result      TEXT,
		details     TEXT,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_audit_time ON audit_log(created_at);
	`

	_, err := s.db.Exec(schema)
	return err
}

func (s *SQLiteStore) CreateConversation(ctx context.Context, conv domain.Conversation) error {
	now := time.Now()
	if conv.CreatedAt.IsZero() {
		conv.CreatedAt = now
	}
	if conv.UpdatedAt.IsZero() {
		conv.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO conversations (id, title, provider, model, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		conv.ID, conv.Title, conv.Provider, conv.Model, conv.CreatedAt, conv.UpdatedAt,
	)
	return err
}

func (s *SQLiteStore) GetConversation(ctx context.Context, id string) (*domain.Conversation, error) {
	var conv domain.Conversation
	err := s.db.QueryRowContext(ctx,
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
	_, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET title=?, provider=?, model=?, updated_at=? WHERE id=?`,
		conv.Title, conv.Provider, conv.Model, conv.UpdatedAt, conv.ID,
	)
	return err
}

func (s *SQLiteStore) ListConversations(ctx context.Context, limit int) ([]domain.Conversation, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
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
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (conversation_id, role, content, tool_calls, tool_call_id, tool_name, tokens_in, tokens_out, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		convID, msg.Role, msg.Content, msg.ToolCalls, msg.ToolCallID, msg.ToolName, msg.TokensIn, msg.TokensOut, msg.CreatedAt,
	)
	if err != nil {
		return err
	}

	_, _ = s.db.ExecContext(ctx,
		`UPDATE conversations SET updated_at = ? WHERE id = ?`, now, convID,
	)
	return nil
}

func (s *SQLiteStore) GetMessages(ctx context.Context, convID string, limit int) ([]domain.MessageRecord, error) {
	if limit <= 0 {
		limit = 100
	}

	// Get last N messages, ordered oldest first
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, tool_calls, tool_call_id, tool_name, tokens_in, tokens_out, created_at
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
		var toolCalls, toolCallID, toolName sql.NullString
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content,
			&toolCalls, &toolCallID, &toolName,
			&m.TokensIn, &m.TokensOut, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.ToolCalls = toolCalls.String
		m.ToolCallID = toolCallID.String
		m.ToolName = toolName.String
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
	_, err := s.db.ExecContext(ctx,
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
	rows, err := s.db.QueryContext(ctx,
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
	rows, err := s.db.QueryContext(ctx,
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

func (s *SQLiteStore) LogAudit(ctx context.Context, entry domain.AuditEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (action, tool_name, command, result, details)
		 VALUES (?, ?, ?, ?, ?)`,
		entry.Action, entry.ToolName, entry.Command, entry.Result, entry.Details,
	)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
