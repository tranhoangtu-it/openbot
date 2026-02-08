package agent

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"openbot/internal/domain"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestEstimateTokens_EmptyMessages(t *testing.T) {
	tokens := EstimateTokens(nil)
	if tokens != 0 {
		t.Errorf("expected 0 tokens for nil messages, got %d", tokens)
	}
}

func TestEstimateTokens_SingleMessage(t *testing.T) {
	msgs := []domain.Message{
		{Role: "user", Content: "Hello world"},
	}
	tokens := EstimateTokens(msgs)
	if tokens < 1 {
		t.Errorf("expected at least 1 token, got %d", tokens)
	}
}

func TestEstimateTokens_MultipleMessages(t *testing.T) {
	msgs := []domain.Message{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "What is the weather today"},
		{Role: "assistant", Content: "I can help you with that. Let me check the weather for you."},
	}
	tokens := EstimateTokens(msgs)
	// Should be a reasonable number for ~20 words
	if tokens < 10 || tokens > 100 {
		t.Errorf("unexpected token count for multi-message: %d", tokens)
	}
}

func TestEstimateTokens_WithToolCalls(t *testing.T) {
	msgs := []domain.Message{
		{
			Role: "assistant",
			ToolCalls: []domain.ToolCall{
				{
					Name:      "shell",
					Arguments: map[string]any{"command": "ls -la /home/user/documents"},
				},
			},
		},
	}
	tokens := EstimateTokens(msgs)
	if tokens < 1 {
		t.Errorf("expected tokens for tool call arguments, got %d", tokens)
	}
}

func TestEstimateStringTokens_EmptyString(t *testing.T) {
	if got := estimateStringTokens(""); got != 0 {
		t.Errorf("expected 0 for empty string, got %d", got)
	}
}

func TestEstimateStringTokens_SingleWord(t *testing.T) {
	if got := estimateStringTokens("hello"); got < 1 {
		t.Errorf("expected at least 1 for single word, got %d", got)
	}
}

func TestEstimateStringTokens_LongText(t *testing.T) {
	text := "This is a longer text with multiple words that should produce a reasonable token estimate for testing purposes"
	tokens := estimateStringTokens(text)
	if tokens < 5 {
		t.Errorf("expected more tokens for long text, got %d", tokens)
	}
}

// mockProvider for testing compaction summarization.
type mockProvider struct {
	chatResp    *domain.ChatResponse
	chatErr     error
	callCount   int
}

func (m *mockProvider) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	m.callCount++
	return m.chatResp, m.chatErr
}
func (m *mockProvider) Name() string                     { return "mock" }
func (m *mockProvider) Mode() domain.ProviderMode        { return domain.ModeAPI }
func (m *mockProvider) Models() []string                 { return []string{"mock-model"} }
func (m *mockProvider) SupportsToolCalling() bool        { return false }
func (m *mockProvider) Healthy(_ context.Context) error  { return nil }

func TestCompactor_NoCompactionNeeded(t *testing.T) {
	mp := &mockProvider{}
	c := NewCompactor(CompactorConfig{
		Provider:  mp,
		MaxTokens: 100000, // very large budget
	})

	msgs := []domain.Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}

	result := c.Compact(context.Background(), msgs)
	if len(result) != len(msgs) {
		t.Errorf("expected no compaction, got %d messages (was %d)", len(result), len(msgs))
	}
	if mp.callCount != 0 {
		t.Errorf("expected no LLM calls, got %d", mp.callCount)
	}
}

func TestCompactor_TooFewMessages(t *testing.T) {
	mp := &mockProvider{}
	c := NewCompactor(CompactorConfig{
		Provider:  mp,
		MaxTokens: 1, // tiny budget
	})

	msgs := []domain.Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Hello"},
	}

	result := c.Compact(context.Background(), msgs)
	if len(result) != len(msgs) {
		t.Errorf("should not compact too few messages")
	}
}

func TestCompactor_TriggersCompaction(t *testing.T) {
	mp := &mockProvider{
		chatResp: &domain.ChatResponse{Content: "Summary of the conversation"},
	}
	c := NewCompactor(CompactorConfig{
		Provider:  mp,
		MaxTokens: 10, // small budget to force compaction
	})

	msgs := []domain.Message{
		{Role: "system", Content: "You are a very helpful assistant with many instructions"},
		{Role: "user", Content: "First question about many things in the world"},
		{Role: "assistant", Content: "First answer with a lot of detail and information"},
		{Role: "user", Content: "Second question about different topics and subjects"},
		{Role: "assistant", Content: "Second answer with even more information and detail"},
		{Role: "user", Content: "Third question to push tokens over the limit"},
		{Role: "assistant", Content: "Third detailed answer"},
		{Role: "user", Content: "Latest question"},
	}

	result := c.Compact(context.Background(), msgs)

	// Should be system + summary + minRecentMessages
	if len(result) > len(msgs) {
		t.Errorf("compacted result should have fewer messages, got %d (was %d)", len(result), len(msgs))
	}
	if mp.callCount != 1 {
		t.Errorf("expected exactly 1 LLM summarization call, got %d", mp.callCount)
	}

	// Check that the first message is still the system prompt
	if result[0].Role != "system" || result[0].Content != msgs[0].Content {
		t.Error("first message should be the original system prompt")
	}

	// Check that the second message is the summary
	if result[1].Role != "system" || result[1].Content == "" {
		t.Error("second message should be the conversation summary")
	}
}

func TestCompactor_DefaultMaxTokens(t *testing.T) {
	mp := &mockProvider{}
	c := NewCompactor(CompactorConfig{
		Provider:  mp,
		MaxTokens: 0, // should use default
	})

	if c.maxTokens != defaultMaxContextTokens {
		t.Errorf("expected default maxTokens=%d, got %d", defaultMaxContextTokens, c.maxTokens)
	}
}

func TestCompactor_SummarizationError(t *testing.T) {
	mp := &mockProvider{
		chatErr: context.DeadlineExceeded,
	}
	c := NewCompactor(CompactorConfig{
		Provider:  mp,
		MaxTokens: 5, // tiny budget
		Logger:    testLogger(),
	})

	msgs := []domain.Message{
		{Role: "system", Content: "You are a very helpful assistant"},
		{Role: "user", Content: "First question with many words"},
		{Role: "assistant", Content: "First answer with many words"},
		{Role: "user", Content: "Second question with many words"},
		{Role: "assistant", Content: "Second answer with many words"},
		{Role: "user", Content: "Latest question"},
	}

	result := c.Compact(context.Background(), msgs)
	// On summarization error, should return original messages
	if len(result) != len(msgs) {
		t.Errorf("expected original messages on error, got %d (was %d)", len(result), len(msgs))
	}
}

func TestToolNames(t *testing.T) {
	calls := []domain.ToolCall{
		{Name: "shell"},
		{Name: "web_fetch"},
		{Name: "read_file"},
	}
	result := toolNames(calls)
	expected := "shell, web_fetch, read_file"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestToolNames_Empty(t *testing.T) {
	result := toolNames(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
