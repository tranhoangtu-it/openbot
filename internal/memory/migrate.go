package memory

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// schemaVersion is the current expected schema version.
const schemaVersion = 3

// migration represents a single schema migration step.
type migration struct {
	Version     int
	Description string
	SQL         string
}

// migrations is the ordered list of schema migrations.
// Each migration is applied exactly once, tracked in the schema_version table.
var migrations = []migration{
	{
		Version:     1,
		Description: "base schema: conversations, messages, memories, audit_log",
		SQL: `
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
		`,
	},
	{
		Version:     2,
		Description: "v2: message metadata columns, documents, document_chunks, knowledge FTS",
		SQL: `
		ALTER TABLE messages ADD COLUMN provider TEXT DEFAULT '';
		ALTER TABLE messages ADD COLUMN model TEXT DEFAULT '';
		ALTER TABLE messages ADD COLUMN latency_ms INTEGER DEFAULT 0;

		CREATE TABLE IF NOT EXISTS documents (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			mime_type   TEXT DEFAULT '',
			size        INTEGER DEFAULT 0,
			content     TEXT,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS document_chunks (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			chunk_index INTEGER NOT NULL,
			content     TEXT NOT NULL,
			tokens      INTEGER DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_chunks_doc ON document_chunks(document_id, chunk_index);

		CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
			content,
			content='document_chunks',
			content_rowid='id'
		);
		`,
	},
	{
		Version:     3,
		Description: "v3: token_usage tracking, paired_users, attachments",
		SQL: `
		CREATE TABLE IF NOT EXISTS token_usage (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			provider    TEXT NOT NULL,
			model       TEXT DEFAULT '',
			tokens_in   INTEGER DEFAULT 0,
			tokens_out  INTEGER DEFAULT 0,
			cost_usd    REAL DEFAULT 0,
			conversation_id TEXT,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_token_usage_time ON token_usage(created_at);
		CREATE INDEX IF NOT EXISTS idx_token_usage_prov ON token_usage(provider);

		CREATE TABLE IF NOT EXISTS paired_users (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			channel     TEXT NOT NULL,
			user_id     TEXT NOT NULL,
			paired_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at  DATETIME,
			UNIQUE(channel, user_id)
		);

		CREATE TABLE IF NOT EXISTS attachments (
			id              TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			filename        TEXT NOT NULL,
			mime_type       TEXT DEFAULT '',
			size            INTEGER DEFAULT 0,
			storage_path    TEXT NOT NULL,
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_attachments_conv ON attachments(conversation_id);
		`,
	},
}

// RunMigrations applies all pending schema migrations.
// It uses a schema_version table to track which migrations have been applied.
func RunMigrations(db *sql.DB, logger *slog.Logger) error {
	// Ensure schema_version table exists.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version     INTEGER PRIMARY KEY,
			description TEXT,
			applied_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	// Get current version.
	currentVersion := 0
	row := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&currentVersion); err != nil {
		return fmt.Errorf("query schema version: %w", err)
	}

	// Apply pending migrations.
	for _, m := range migrations {
		if m.Version <= currentVersion {
			continue
		}

		logger.Info("applying migration",
			"version", m.Version,
			"description", m.Description,
		)

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration v%d: %w", m.Version, err)
		}

		// Execute migration SQL (may contain multiple statements).
		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			// Some ALTER TABLE ADD COLUMN may fail if column already exists.
			// This is expected for upgrades from the old schema.
			logger.Warn("migration SQL partially failed (may be expected for upgrades)",
				"version", m.Version,
				"err", err,
			)
			// Re-try each statement individually to handle partial failures.
			tx.Rollback()
			if err := applyMigrationStatements(db, m, logger); err != nil {
				return err
			}
		} else {
			// Record migration version.
			if _, err := tx.Exec(
				"INSERT OR REPLACE INTO schema_version (version, description) VALUES (?, ?)",
				m.Version, m.Description,
			); err != nil {
				tx.Rollback()
				return fmt.Errorf("record migration v%d: %w", m.Version, err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit migration v%d: %w", m.Version, err)
			}
		}

		logger.Info("migration applied", "version", m.Version)
	}

	return nil
}

// applyMigrationStatements applies each SQL statement individually, ignoring
// "duplicate column" or "table already exists" errors for idempotency.
func applyMigrationStatements(db *sql.DB, m migration, logger *slog.Logger) error {
	statements := splitSQL(m.SQL)
	for _, stmt := range statements {
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			// Ignore "duplicate column" and "table already exists" errors
			errStr := err.Error()
			if contains(errStr, "duplicate column") ||
				contains(errStr, "already exists") {
				logger.Debug("migration statement skipped (already applied)", "stmt_prefix", truncate(stmt, 60))
				continue
			}
			return fmt.Errorf("migration v%d statement failed: %w\nSQL: %s", m.Version, err, truncate(stmt, 200))
		}
	}

	// Record migration version.
	if _, err := db.Exec(
		"INSERT OR REPLACE INTO schema_version (version, description) VALUES (?, ?)",
		m.Version, m.Description,
	); err != nil {
		return fmt.Errorf("record migration v%d: %w", m.Version, err)
	}
	return nil
}

// splitSQL splits a multi-statement SQL string on semicolons.
func splitSQL(sql string) []string {
	var result []string
	for _, s := range splitOnSemicolon(sql) {
		s = trimSpace(s)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

// contains checks if s contains substr (case-insensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(substr) == 0 ||
			findCI(s, substr))
}

func findCI(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if eqFoldSlice(s[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

func eqFoldSlice(a, b string) bool {
	for i := range a {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func splitOnSemicolon(s string) []string {
	var parts []string
	current := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ';' {
			parts = append(parts, s[current:i])
			current = i + 1
		}
	}
	if current < len(s) {
		parts = append(parts, s[current:])
	}
	return parts
}

// GetSchemaVersion returns the current schema version from the database.
func GetSchemaVersion(db *sql.DB) (int, error) {
	// Check if schema_version table exists.
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version'").Scan(&tableName)
	if err != nil {
		return 0, nil // Table doesn't exist => version 0
	}

	var version int
	err = db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}
