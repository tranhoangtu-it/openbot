package provider

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"openbot/internal/domain"
)

// FailoverProvider tries multiple providers in order, falling back to the next
// one when the current fails. It implements both Provider and StreamingProvider.
type FailoverProvider struct {
	providers []domain.Provider
	logger    *slog.Logger
}

// NewFailoverProvider creates a failover chain from the given providers.
// At least one provider is required.
func NewFailoverProvider(providers []domain.Provider, logger *slog.Logger) *FailoverProvider {
	return &FailoverProvider{
		providers: providers,
		logger:    logger,
	}
}

func (fp *FailoverProvider) Name() string {
	names := make([]string, len(fp.providers))
	for i, p := range fp.providers {
		names[i] = p.Name()
	}
	return "failover(" + strings.Join(names, "→") + ")"
}

func (fp *FailoverProvider) Mode() domain.ProviderMode {
	if len(fp.providers) > 0 {
		return fp.providers[0].Mode()
	}
	return domain.ModeAPI
}

func (fp *FailoverProvider) Models() []string {
	var all []string
	seen := make(map[string]bool)
	for _, p := range fp.providers {
		for _, m := range p.Models() {
			if !seen[m] {
				seen[m] = true
				all = append(all, m)
			}
		}
	}
	return all
}

func (fp *FailoverProvider) SupportsToolCalling() bool {
	for _, p := range fp.providers {
		if p.SupportsToolCalling() {
			return true
		}
	}
	return false
}

func (fp *FailoverProvider) Healthy(ctx context.Context) error {
	for _, p := range fp.providers {
		if err := p.Healthy(ctx); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no healthy provider in failover chain")
}

// Chat tries each provider in order. Returns the first successful response.
func (fp *FailoverProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	var lastErr error
	for i, p := range fp.providers {
		resp, err := p.Chat(ctx, req)
		if err == nil {
			if i > 0 {
				fp.logger.Info("failover: used fallback provider",
					"provider", p.Name(),
					"attempt", i+1,
				)
			}
			return resp, nil
		}
		lastErr = err
		fp.logger.Warn("failover: provider failed, trying next",
			"provider", p.Name(),
			"attempt", i+1,
			"error", err,
		)
	}
	return nil, fmt.Errorf("all providers in failover chain failed: %w", lastErr)
}

// ChatStream uses the first available streaming provider.
//
// IMPORTANT: Streaming failover with retry is unsafe because each provider's
// ChatStream closes the output channel on return (via defer close). If we passed
// the same channel to a second provider after the first failed, writing to the
// already-closed channel would panic. Therefore, we use the first streaming
// provider directly without retry. Non-streaming Chat() still does full failover.
func (fp *FailoverProvider) ChatStream(ctx context.Context, req domain.ChatRequest, out chan<- domain.StreamEvent) error {
	for _, p := range fp.providers {
		sp, ok := p.(domain.StreamingProvider)
		if !ok {
			continue
		}
		// Use the first streaming provider found — no retry to avoid close-channel panic.
		return sp.ChatStream(ctx, req, out)
	}

	// No streaming provider found — fall back to non-streaming Chat and emit result.
	defer close(out)
	resp, err := fp.Chat(ctx, req)
	if err != nil {
		return err
	}
	if resp.Content != "" {
		out <- domain.StreamEvent{Type: domain.StreamToken, Content: resp.Content}
	}
	out <- domain.StreamEvent{
		Type:      domain.StreamDone,
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
	}
	return nil
}
