package memory

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestRunMigrations_FreshDB(t *testing.T) {
	db := testDB(t)
	logger := testLogger()

	if err := RunMigrations(db, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	// Verify schema version
	version, err := GetSchemaVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Errorf("expected schema version %d, got %d", schemaVersion, version)
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	db := testDB(t)
	logger := testLogger()

	// Run twice — should not fail
	if err := RunMigrations(db, logger); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}
	if err := RunMigrations(db, logger); err != nil {
		t.Fatalf("second migration (idempotent) failed: %v", err)
	}

	version, err := GetSchemaVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Errorf("expected schema version %d, got %d", schemaVersion, version)
	}
}

func TestRunMigrations_CreatesExpectedTables(t *testing.T) {
	db := testDB(t)
	logger := testLogger()

	if err := RunMigrations(db, logger); err != nil {
		t.Fatal(err)
	}

	expectedTables := []string{
		"conversations", "messages", "memories", "audit_log",
		"documents", "document_chunks", "token_usage",
		"paired_users", "attachments", "schema_version",
	}

	for _, table := range expectedTables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestRunMigrations_V3_TokenUsage(t *testing.T) {
	db := testDB(t)
	logger := testLogger()

	if err := RunMigrations(db, logger); err != nil {
		t.Fatal(err)
	}

	// Insert into token_usage
	_, err := db.Exec(
		"INSERT INTO token_usage (provider, model, tokens_in, tokens_out) VALUES (?, ?, ?, ?)",
		"openai", "gpt-4", 100, 50,
	)
	if err != nil {
		t.Fatalf("insert into token_usage failed: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM token_usage").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestRunMigrations_V3_PairedUsers(t *testing.T) {
	db := testDB(t)
	logger := testLogger()

	if err := RunMigrations(db, logger); err != nil {
		t.Fatal(err)
	}

	// Insert into paired_users
	_, err := db.Exec(
		"INSERT INTO paired_users (channel, user_id) VALUES (?, ?)",
		"telegram", "user123",
	)
	if err != nil {
		t.Fatalf("insert into paired_users failed: %v", err)
	}

	var userID string
	db.QueryRow("SELECT user_id FROM paired_users WHERE channel=?", "telegram").Scan(&userID)
	if userID != "user123" {
		t.Errorf("expected user123, got %s", userID)
	}
}

func TestRunMigrations_V3_Attachments(t *testing.T) {
	db := testDB(t)
	logger := testLogger()

	if err := RunMigrations(db, logger); err != nil {
		t.Fatal(err)
	}

	// Need a conversation first
	_, err := db.Exec("INSERT INTO conversations (id, title) VALUES (?, ?)", "conv-1", "Test")
	if err != nil {
		t.Fatal(err)
	}

	// Insert into attachments
	_, err = db.Exec(
		"INSERT INTO attachments (id, conversation_id, filename, storage_path) VALUES (?, ?, ?, ?)",
		"att-1", "conv-1", "test.pdf", "/tmp/test.pdf",
	)
	if err != nil {
		t.Fatalf("insert into attachments failed: %v", err)
	}
}

func TestGetSchemaVersion_NoTable(t *testing.T) {
	db := testDB(t)
	version, err := GetSchemaVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Errorf("expected version 0 for empty db, got %d", version)
	}
}

func TestSplitSQL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty", "", 0},
		{"single", "CREATE TABLE t (id INT)", 1},
		{"multiple", "CREATE TABLE t1 (id INT); CREATE TABLE t2 (id INT)", 2},
		{"trailing semicolon", "CREATE TABLE t (id INT);", 1},
		{"whitespace", "  CREATE TABLE t (id INT)  ;  ", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitSQL(tt.input)
			if len(result) != tt.expected {
				t.Errorf("expected %d statements, got %d: %v", tt.expected, len(result), result)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		s, sub string
		want   bool
	}{
		{"duplicate column", "duplicate column", true},
		{"DUPLICATE COLUMN", "duplicate column", true},
		{"already exists", "already exists", true},
		{"something else", "duplicate column", false},
		{"", "", true},
		{"abc", "", true},
	}
	for _, tt := range tests {
		got := contains(tt.s, tt.sub)
		if got != tt.want {
			t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.sub, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("expected 'hello...', got %q", got)
	}
}
