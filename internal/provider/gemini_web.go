package provider

import (
	"context"
	"fmt"
	"log/slog"
	"openbot/internal/browser"
	"openbot/internal/domain"
)

// GeminiWeb implements domain.Provider using browser automation to gemini.google.com.
type GeminiWeb struct {
	bridge    *browser.Bridge
	selectors browser.SelectorSet
	logger    *slog.Logger
}

type GeminiWebConfig struct {
	ProfileDir string
	Selectors  map[string]string
	Logger     *slog.Logger
}

func NewGeminiWeb(cfg GeminiWebConfig) *GeminiWeb {
	bridge := browser.NewBridge(browser.BridgeConfig{
		ProfileDir: cfg.ProfileDir,
		Headless:   true,
		Logger:     cfg.Logger,
	})

	sel := browser.GeminiSelectors()
	if v, ok := cfg.Selectors["url"]; ok && v != "" {
		sel.URL = v
	}
	if v, ok := cfg.Selectors["input"]; ok && v != "" {
		sel.Input = v
	}
	if v, ok := cfg.Selectors["submit"]; ok && v != "" {
		sel.Submit = v
	}
	if v, ok := cfg.Selectors["response"]; ok && v != "" {
		sel.Response = v
	}
	if v, ok := cfg.Selectors["loading"]; ok && v != "" {
		sel.Loading = v
	}

	return &GeminiWeb{
		bridge:    bridge,
		selectors: sel,
		logger:    cfg.Logger,
	}
}

func (p *GeminiWeb) Name() string                  { return "gemini" }
func (p *GeminiWeb) Mode() domain.ProviderMode     { return domain.ModeBrowser }
func (p *GeminiWeb) Models() []string               { return []string{"gemini-pro", "gemini-ultra"} }
func (p *GeminiWeb) SupportsToolCalling() bool       { return false }

func (p *GeminiWeb) Healthy(ctx context.Context) error {
	if p.bridge == nil {
		return fmt.Errorf("browser bridge not initialized")
	}
	return nil
}

func (p *GeminiWeb) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	var userMessage string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			userMessage = req.Messages[i].Content
			break
		}
	}
	if userMessage == "" {
		return &domain.ChatResponse{Content: "No user message found.", FinishReason: "stop"}, nil
	}

	p.logger.Info("gemini_web: sending message", "len", len(userMessage))

	response, err := p.bridge.SendAndReceive(ctx, p.selectors, userMessage)
	if err != nil {
		return nil, fmt.Errorf("gemini_web: %w", err)
	}

	p.logger.Info("gemini_web: received response", "len", len(response))

	return &domain.ChatResponse{
		Content:      response,
		FinishReason: "stop",
	}, nil
}

func (p *GeminiWeb) Login(ctx context.Context) error {
	return p.bridge.Login(ctx, p.selectors.URL)
}
