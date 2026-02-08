package security

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func testPairingDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Create required table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS paired_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel TEXT NOT NULL,
			user_id TEXT NOT NULL,
			paired_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME,
			UNIQUE(channel, user_id)
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func testPairingLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestPairingService_NotRequired(t *testing.T) {
	ps := NewPairingService(PairingConfig{
		Required: false,
		Logger:   testPairingLogger(),
	})

	paired, err := ps.IsPaired(context.Background(), "telegram", "user1")
	if err != nil {
		t.Fatal(err)
	}
	if !paired {
		t.Error("when pairing not required, all users should be considered paired")
	}
}

func TestPairingService_GenerateCode(t *testing.T) {
	ps := NewPairingService(PairingConfig{
		Required: true,
		Logger:   testPairingLogger(),
	})

	code := ps.GenerateCode("telegram", "user1")
	if len(code) != 6 {
		t.Errorf("expected 6-digit code, got %q", code)
	}

	// All digits
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Errorf("code contains non-digit: %c", c)
		}
	}
}

func TestPairingService_GenerateCode_Unique(t *testing.T) {
	ps := NewPairingService(PairingConfig{
		Required: true,
		Logger:   testPairingLogger(),
	})

	codes := make(map[string]bool)
	for i := 0; i < 100; i++ {
		code := ps.GenerateCode("telegram", "user1")
		codes[code] = true
	}
	// With 100 random 6-digit codes, we should see at least some variety
	if len(codes) < 5 {
		t.Error("codes seem not very random")
	}
}

func TestPairingService_VerifyCode_Success(t *testing.T) {
	db := testPairingDB(t)
	ps := NewPairingService(PairingConfig{
		Required: true,
		TTLDays:  30,
		DB:       db,
		Logger:   testPairingLogger(),
	})

	code := ps.GenerateCode("telegram", "user1")
	ok, err := ps.VerifyCode(context.Background(), "telegram", "user1", code)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("valid code should verify successfully")
	}

	// After verification, user should be paired
	paired, err := ps.IsPaired(context.Background(), "telegram", "user1")
	if err != nil {
		t.Fatal(err)
	}
	if !paired {
		t.Error("user should be paired after code verification")
	}
}

func TestPairingService_VerifyCode_WrongCode(t *testing.T) {
	ps := NewPairingService(PairingConfig{
		Required: true,
		Logger:   testPairingLogger(),
	})

	ps.GenerateCode("telegram", "user1")
	ok, err := ps.VerifyCode(context.Background(), "telegram", "user1", "000000")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("wrong code should not verify")
	}
}

func TestPairingService_VerifyCode_NoCodeGenerated(t *testing.T) {
	ps := NewPairingService(PairingConfig{
		Required: true,
		Logger:   testPairingLogger(),
	})

	ok, err := ps.VerifyCode(context.Background(), "telegram", "user1", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("should not verify when no code was generated")
	}
}

func TestPairingService_IsPaired_NotPaired(t *testing.T) {
	db := testPairingDB(t)
	ps := NewPairingService(PairingConfig{
		Required: true,
		DB:       db,
		Logger:   testPairingLogger(),
	})

	paired, err := ps.IsPaired(context.Background(), "telegram", "unknown_user")
	if err != nil {
		t.Fatal(err)
	}
	if paired {
		t.Error("unknown user should not be paired")
	}
}

func TestPairingService_Unpair(t *testing.T) {
	db := testPairingDB(t)
	ps := NewPairingService(PairingConfig{
		Required: true,
		TTLDays:  30,
		DB:       db,
		Logger:   testPairingLogger(),
	})

	// Pair user first
	code := ps.GenerateCode("telegram", "user1")
	ps.VerifyCode(context.Background(), "telegram", "user1", code)

	// Unpair
	if err := ps.Unpair(context.Background(), "telegram", "user1"); err != nil {
		t.Fatal(err)
	}

	// Should no longer be paired
	paired, err := ps.IsPaired(context.Background(), "telegram", "user1")
	if err != nil {
		t.Fatal(err)
	}
	if paired {
		t.Error("user should not be paired after unpair")
	}
}

func TestPairingService_CleanExpiredCodes(t *testing.T) {
	ps := NewPairingService(PairingConfig{
		Required: true,
		Logger:   testPairingLogger(),
	})

	// Generate a code, then clean (shouldn't remove it since it's not expired)
	ps.GenerateCode("telegram", "user1")
	ps.CleanExpiredCodes()

	// The code should still be there
	ps.mu.RLock()
	_, exists := ps.pendingCodes["telegram:user1"]
	ps.mu.RUnlock()

	if !exists {
		t.Error("non-expired code should not be cleaned")
	}
}

func TestPairingService_DefaultTTL(t *testing.T) {
	ps := NewPairingService(PairingConfig{
		Required: true,
		TTLDays:  0, // should use default (30)
		Logger:   testPairingLogger(),
	})

	if ps.ttlDays != 30 {
		t.Errorf("expected default TTL of 30 days, got %d", ps.ttlDays)
	}
}

func TestGenerateSecureCode_Length(t *testing.T) {
	for _, length := range []int{4, 6, 8, 10} {
		code := generateSecureCode(length)
		if len(code) != length {
			t.Errorf("expected code length %d, got %d", length, len(code))
		}
	}
}
