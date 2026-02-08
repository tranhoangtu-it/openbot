package provider

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"openbot/internal/domain"
)

// mockProvider implements domain.Provider for testing.
type mockProvider struct {
	name       string
	healthy    bool
	chatErr    error
	chatResp   *domain.ChatResponse
	toolCalls  bool
}

func (m *mockProvider) Name() string                    { return m.name }
func (m *mockProvider) Mode() domain.ProviderMode       { return domain.ModeAPI }
func (m *mockProvider) Models() []string                { return []string{"test-model"} }
func (m *mockProvider) SupportsToolCalling() bool       { return m.toolCalls }

func (m *mockProvider) Healthy(ctx context.Context) error {
	if !m.healthy {
		return errors.New("unhealthy")
	}
	return nil
}

func (m *mockProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	if m.chatErr != nil {
		return nil, m.chatErr
	}
	return m.chatResp, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- Logic đúng ---

func TestFailoverProvider_UsesFirstHealthyProvider(t *testing.T) {
	p1 := &mockProvider{name: "primary", healthy: true, chatResp: &domain.ChatResponse{Content: "from-primary"}}
	p2 := &mockProvider{name: "secondary", healthy: true, chatResp: &domain.ChatResponse{Content: "from-secondary"}}
	fp := NewFailoverProvider([]domain.Provider{p1, p2}, testLogger())

	resp, err := fp.Chat(context.Background(), domain.ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "from-primary" {
		t.Fatalf("expected 'from-primary', got %q", resp.Content)
	}
}

func TestFailoverProvider_FallsBackOnError(t *testing.T) {
	p1 := &mockProvider{name: "primary", healthy: true, chatErr: errors.New("api error")}
	p2 := &mockProvider{name: "secondary", healthy: true, chatResp: &domain.ChatResponse{Content: "from-secondary"}}
	fp := NewFailoverProvider([]domain.Provider{p1, p2}, testLogger())

	resp, err := fp.Chat(context.Background(), domain.ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "from-secondary" {
		t.Fatalf("expected 'from-secondary', got %q", resp.Content)
	}
}

// --- Điều kiện rẽ nhánh ---

func TestFailoverProvider_AllProvidersFail(t *testing.T) {
	p1 := &mockProvider{name: "p1", healthy: true, chatErr: errors.New("fail 1")}
	p2 := &mockProvider{name: "p2", healthy: true, chatErr: errors.New("fail 2")}
	fp := NewFailoverProvider([]domain.Provider{p1, p2}, testLogger())

	_, err := fp.Chat(context.Background(), domain.ChatRequest{})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

// --- Giá trị biên ---

func TestFailoverProvider_SingleProvider(t *testing.T) {
	p1 := &mockProvider{name: "only", healthy: true, chatResp: &domain.ChatResponse{Content: "only-one"}}
	fp := NewFailoverProvider([]domain.Provider{p1}, testLogger())

	resp, err := fp.Chat(context.Background(), domain.ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "only-one" {
		t.Fatalf("expected 'only-one', got %q", resp.Content)
	}
}

// --- Health check ---

func TestFailoverProvider_Healthy_AtLeastOneHealthy(t *testing.T) {
	p1 := &mockProvider{name: "sick", healthy: false}
	p2 := &mockProvider{name: "well", healthy: true}
	fp := NewFailoverProvider([]domain.Provider{p1, p2}, testLogger())

	if err := fp.Healthy(context.Background()); err != nil {
		t.Fatalf("expected healthy, got: %v", err)
	}
}

func TestFailoverProvider_Healthy_NoneHealthy(t *testing.T) {
	p1 := &mockProvider{name: "sick1", healthy: false}
	p2 := &mockProvider{name: "sick2", healthy: false}
	fp := NewFailoverProvider([]domain.Provider{p1, p2}, testLogger())

	if err := fp.Healthy(context.Background()); err == nil {
		t.Fatal("expected unhealthy error")
	}
}

// --- Name ---

func TestFailoverProvider_Name(t *testing.T) {
	p1 := &mockProvider{name: "ollama"}
	p2 := &mockProvider{name: "openai"}
	fp := NewFailoverProvider([]domain.Provider{p1, p2}, testLogger())

	name := fp.Name()
	if name != "failover(ollama→openai)" {
		t.Fatalf("expected 'failover(ollama→openai)', got %q", name)
	}
}

// --- SupportsToolCalling ---

func TestFailoverProvider_ToolCalling_AtLeastOne(t *testing.T) {
	p1 := &mockProvider{name: "no-tools", toolCalls: false}
	p2 := &mockProvider{name: "has-tools", toolCalls: true}
	fp := NewFailoverProvider([]domain.Provider{p1, p2}, testLogger())

	if !fp.SupportsToolCalling() {
		t.Fatal("expected SupportsToolCalling=true")
	}
}

func TestFailoverProvider_ToolCalling_None(t *testing.T) {
	p1 := &mockProvider{name: "no-tools1", toolCalls: false}
	p2 := &mockProvider{name: "no-tools2", toolCalls: false}
	fp := NewFailoverProvider([]domain.Provider{p1, p2}, testLogger())

	if fp.SupportsToolCalling() {
		t.Fatal("expected SupportsToolCalling=false")
	}
}

// --- Models ---

func TestFailoverProvider_Models_Deduplicated(t *testing.T) {
	p1 := &mockProvider{name: "p1"} // returns ["test-model"]
	p2 := &mockProvider{name: "p2"} // returns ["test-model"]
	fp := NewFailoverProvider([]domain.Provider{p1, p2}, testLogger())

	models := fp.Models()
	if len(models) != 1 {
		t.Fatalf("expected 1 unique model, got %d: %v", len(models), models)
	}
}

// --- ChatStream safety ---

// mockStreamProvider implements StreamingProvider for testing.
type mockStreamProvider struct {
	mockProvider
	streamErr  error
	streamResp string
}

func (m *mockStreamProvider) ChatStream(ctx context.Context, req domain.ChatRequest, out chan<- domain.StreamEvent) error {
	defer close(out)
	if m.streamErr != nil {
		return m.streamErr
	}
	out <- domain.StreamEvent{Type: domain.StreamToken, Content: m.streamResp}
	out <- domain.StreamEvent{Type: domain.StreamDone, Content: m.streamResp}
	return nil
}

func TestFailoverProvider_ChatStream_UsesFirstStreamingProvider(t *testing.T) {
	p1 := &mockStreamProvider{
		mockProvider: mockProvider{name: "primary", healthy: true},
		streamResp:   "streamed-from-primary",
	}
	p2 := &mockStreamProvider{
		mockProvider: mockProvider{name: "secondary", healthy: true},
		streamResp:   "streamed-from-secondary",
	}
	fp := NewFailoverProvider([]domain.Provider{p1, p2}, testLogger())

	out := make(chan domain.StreamEvent, 64)
	err := fp.ChatStream(context.Background(), domain.ChatRequest{}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var content string
	for evt := range out {
		if evt.Type == domain.StreamDone {
			content = evt.Content
		}
	}
	if content != "streamed-from-primary" {
		t.Fatalf("expected 'streamed-from-primary', got %q", content)
	}
}

func TestFailoverProvider_ChatStream_NoStreamingProvider_FallsBackToChat(t *testing.T) {
	// Non-streaming providers only
	p1 := &mockProvider{name: "non-streaming", healthy: true, chatResp: &domain.ChatResponse{Content: "chat-response"}}
	fp := NewFailoverProvider([]domain.Provider{p1}, testLogger())

	out := make(chan domain.StreamEvent, 64)
	err := fp.ChatStream(context.Background(), domain.ChatRequest{}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var content string
	for evt := range out {
		if evt.Type == domain.StreamDone {
			content = evt.Content
		}
	}
	if content != "chat-response" {
		t.Fatalf("expected 'chat-response', got %q", content)
	}
}

func TestFailoverProvider_ChatStream_SkipsNonStreamingProviders(t *testing.T) {
	// First is non-streaming, second is streaming
	p1 := &mockProvider{name: "non-streaming", healthy: true}
	p2 := &mockStreamProvider{
		mockProvider: mockProvider{name: "streaming", healthy: true},
		streamResp:   "from-streaming",
	}
	fp := NewFailoverProvider([]domain.Provider{p1, p2}, testLogger())

	out := make(chan domain.StreamEvent, 64)
	err := fp.ChatStream(context.Background(), domain.ChatRequest{}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var content string
	for evt := range out {
		if evt.Type == domain.StreamDone {
			content = evt.Content
		}
	}
	if content != "from-streaming" {
		t.Fatalf("expected 'from-streaming', got %q", content)
	}
}
