package memory

import (
	"context"
	"log/slog"
	"time"

	"openbot/internal/domain"
)

// MemoryV2Config configures the enhanced memory system.
type MemoryV2Config struct {
	Store         domain.MemoryStore
	DecayEnabled  bool
	DecayDays     int // memories lose 1 importance per this many days
	MaxMemories   int // max memories to retain
	Logger        *slog.Logger
}

// MemoryV2 provides enhanced memory management with importance scoring,
// automatic decay, and category-based organization.
type MemoryV2 struct {
	store       domain.MemoryStore
	decayDays   int
	maxMemories int
	logger      *slog.Logger
}

// NewMemoryV2 creates a new enhanced memory manager.
func NewMemoryV2(cfg MemoryV2Config) *MemoryV2 {
	if cfg.DecayDays <= 0 {
		cfg.DecayDays = 7
	}
	if cfg.MaxMemories <= 0 {
		cfg.MaxMemories = 1000
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &MemoryV2{
		store:       cfg.Store,
		decayDays:   cfg.DecayDays,
		maxMemories: cfg.MaxMemories,
		logger:      cfg.Logger,
	}
}

// ExtractAndSave extracts memorable facts from a conversation exchange
// and saves them to the memory store.
func (m *MemoryV2) ExtractAndSave(ctx context.Context, userMsg, assistantMsg, conversationID string) error {
	// Extract facts (simple heuristic-based extraction).
	facts := extractFacts(userMsg, assistantMsg)

	for _, fact := range facts {
		entry := domain.MemoryEntry{
			Category:   fact.Category,
			Content:    fact.Content,
			Source:     conversationID,
			Importance: fact.Importance,
		}
		if err := m.store.SaveMemory(ctx, entry); err != nil {
			m.logger.Warn("failed to save memory", "err", err, "content", fact.Content[:min(50, len(fact.Content))])
		}
	}

	return nil
}

// SearchRelevant returns memories sorted by relevance and importance.
func (m *MemoryV2) SearchRelevant(ctx context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	return m.store.SearchMemories(ctx, query, limit)
}

// ApplyDecay reduces importance of old memories.
// Should be called periodically (e.g., daily via cron).
func (m *MemoryV2) ApplyDecay(ctx context.Context) (int, error) {
	memories, err := m.store.GetRecentMemories(ctx, m.maxMemories*2) // get more than max to find decayed ones
	if err != nil {
		return 0, err
	}

	decayed := 0
	cutoff := time.Now().AddDate(0, 0, -m.decayDays)

	for _, mem := range memories {
		if mem.CreatedAt.Before(cutoff) && mem.Importance > 1 {
			mem.Importance--
			if err := m.store.SaveMemory(ctx, mem); err != nil {
				m.logger.Warn("failed to update decayed memory", "id", mem.ID, "err", err)
				continue
			}
			decayed++
		}
	}

	m.logger.Info("memory decay applied", "decayed", decayed)
	return decayed, nil
}

// extractedFact holds an extracted fact with metadata.
type extractedFact struct {
	Category   string
	Content    string
	Importance int
}

// extractFacts uses simple heuristics to identify memorable content.
func extractFacts(userMsg, assistantMsg string) []extractedFact {
	var facts []extractedFact

	// Extract user preferences ("I like...", "I prefer...", "My favorite...")
	if containsAny(userMsg, []string{"I like", "I prefer", "my favorite", "I love", "I hate", "I don't like"}) {
		facts = append(facts, extractedFact{
			Category:   "preference",
			Content:    userMsg,
			Importance: 7,
		})
	}

	// Extract factual information ("My name is...", "I work at...", "I live in...")
	if containsAny(userMsg, []string{"my name is", "I work at", "I live in", "I am from", "my job is", "I'm a"}) {
		facts = append(facts, extractedFact{
			Category:   "fact",
			Content:    userMsg,
			Importance: 9,
		})
	}

	// Extract instructions ("Remember that...", "Always...", "Never...")
	if containsAny(userMsg, []string{"remember that", "always ", "never ", "don't forget", "keep in mind"}) {
		facts = append(facts, extractedFact{
			Category:   "instruction",
			Content:    userMsg,
			Importance: 8,
		})
	}

	return facts
}

// containsAny checks if s contains any of the patterns (case-insensitive).
func containsAny(s string, patterns []string) bool {
	lower := toLower(s)
	for _, p := range patterns {
		if idx := indexCI(lower, toLower(p)); idx >= 0 {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}

func indexCI(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	if len(s) < len(sub) {
		return -1
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
