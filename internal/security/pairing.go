package security

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"
)

// PairingConfig configures the DM pairing system.
type PairingConfig struct {
	Required   bool
	TTLDays    int
	DB         *sql.DB
	Logger     *slog.Logger
}

// PairingService manages user pairing for channel security.
// Unpaired users must provide a one-time code before they can interact with the bot.
type PairingService struct {
	required bool
	ttlDays  int
	db       *sql.DB
	logger   *slog.Logger

	// pendingCodes maps "channel:userID" -> code for pending pairings.
	mu           sync.RWMutex
	pendingCodes map[string]pendingCode
}

type pendingCode struct {
	Code      string
	ExpiresAt time.Time
}

// NewPairingService creates a new PairingService.
func NewPairingService(cfg PairingConfig) *PairingService {
	ttl := cfg.TTLDays
	if ttl <= 0 {
		ttl = 30
	}
	return &PairingService{
		required:     cfg.Required,
		ttlDays:      ttl,
		db:           cfg.DB,
		logger:       cfg.Logger,
		pendingCodes: make(map[string]pendingCode),
	}
}

// IsRequired returns whether pairing is required.
func (ps *PairingService) IsRequired() bool {
	return ps.required
}

// IsPaired checks if a user is paired for the given channel.
func (ps *PairingService) IsPaired(ctx context.Context, channel, userID string) (bool, error) {
	if !ps.required {
		return true, nil
	}
	if ps.db == nil {
		return true, nil
	}

	var count int
	err := ps.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM paired_users 
		 WHERE channel = ? AND user_id = ? AND (expires_at IS NULL OR expires_at > ?)`,
		channel, userID, time.Now(),
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check pairing: %w", err)
	}

	return count > 0, nil
}

// GenerateCode creates a 6-digit pairing code for the user.
// The code expires after 10 minutes.
func (ps *PairingService) GenerateCode(channel, userID string) string {
	code := generateSecureCode(6)
	key := fmt.Sprintf("%s:%s", channel, userID)

	ps.mu.Lock()
	ps.pendingCodes[key] = pendingCode{
		Code:      code,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	ps.mu.Unlock()

	ps.logger.Info("pairing code generated", "channel", channel, "user_id", userID)
	return code
}

// VerifyCode verifies a pairing code and, if valid, pairs the user.
func (ps *PairingService) VerifyCode(ctx context.Context, channel, userID, code string) (bool, error) {
	key := fmt.Sprintf("%s:%s", channel, userID)

	ps.mu.RLock()
	pending, exists := ps.pendingCodes[key]
	ps.mu.RUnlock()

	if !exists {
		return false, nil
	}

	if time.Now().After(pending.ExpiresAt) {
		ps.mu.Lock()
		delete(ps.pendingCodes, key)
		ps.mu.Unlock()
		return false, nil
	}

	if pending.Code != code {
		return false, nil
	}

	// Code matches — pair the user.
	ps.mu.Lock()
	delete(ps.pendingCodes, key)
	ps.mu.Unlock()

	if err := ps.pairUser(ctx, channel, userID); err != nil {
		return false, err
	}

	ps.logger.Info("user paired", "channel", channel, "user_id", userID)
	return true, nil
}

// Unpair removes a user's pairing.
func (ps *PairingService) Unpair(ctx context.Context, channel, userID string) error {
	if ps.db == nil {
		return nil
	}
	_, err := ps.db.ExecContext(ctx,
		"DELETE FROM paired_users WHERE channel = ? AND user_id = ?",
		channel, userID,
	)
	return err
}

func (ps *PairingService) pairUser(ctx context.Context, channel, userID string) error {
	if ps.db == nil {
		return nil
	}

	var expiresAt *time.Time
	if ps.ttlDays > 0 {
		t := time.Now().AddDate(0, 0, ps.ttlDays)
		expiresAt = &t
	}

	_, err := ps.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO paired_users (channel, user_id, paired_at, expires_at)
		 VALUES (?, ?, ?, ?)`,
		channel, userID, time.Now(), expiresAt,
	)
	return err
}

// generateSecureCode generates a cryptographically random numeric code of the given length.
func generateSecureCode(length int) string {
	code := make([]byte, length)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			// Fallback to less secure but still functional
			code[i] = '0'
			continue
		}
		code[i] = byte('0') + byte(n.Int64())
	}
	return string(code)
}

// CleanExpiredCodes removes expired pending codes. Call periodically.
func (ps *PairingService) CleanExpiredCodes() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	now := time.Now()
	for key, pc := range ps.pendingCodes {
		if now.After(pc.ExpiresAt) {
			delete(ps.pendingCodes, key)
		}
	}
}
