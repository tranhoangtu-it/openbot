package provider

import (
	"context"
	"fmt"
	"log/slog"
	"openbot/internal/browser"
	"openbot/internal/domain"
)

// ChatGPTWeb implements domain.Provider using browser automation to chatgpt.com.
type ChatGPTWeb struct {
	bridge    *browser.Bridge
	selectors browser.SelectorSet
	logger    *slog.Logger
}

type ChatGPTWebConfig struct {
	ProfileDir string
	Selectors  map[string]string // Override default selectors
	Logger     *slog.Logger
}

func NewChatGPTWeb(cfg ChatGPTWebConfig) *ChatGPTWeb {
	bridge := browser.NewBridge(browser.BridgeConfig{
		ProfileDir: cfg.ProfileDir,
		Headless:   true,
		Logger:     cfg.Logger,
	})

	sel := browser.ChatGPTSelectors()
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

	return &ChatGPTWeb{
		bridge:    bridge,
		selectors: sel,
		logger:    cfg.Logger,
	}
}

func (p *ChatGPTWeb) Name() string                  { return "chatgpt" }
func (p *ChatGPTWeb) Mode() domain.ProviderMode     { return domain.ModeBrowser }
func (p *ChatGPTWeb) Models() []string               { return []string{"gpt-4o", "gpt-4o-mini"} }
func (p *ChatGPTWeb) SupportsToolCalling() bool       { return false }

func (p *ChatGPTWeb) Healthy(ctx context.Context) error {
	if p.bridge == nil {
		return fmt.Errorf("browser bridge not initialized")
	}
	return nil
}

// Chat sends a message via the ChatGPT web interface and returns the response.
// Note: Tool calling is NOT supported in browser mode. The agent must use
// prompt-based tool calling (instruct the LLM to output JSON tool calls in text).
func (p *ChatGPTWeb) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	// For browser mode, we only send the last user message
	// (the web interface manages its own context)
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

	p.logger.Info("chatgpt_web: sending message", "len", len(userMessage))

	response, err := p.bridge.SendAndReceive(ctx, p.selectors, userMessage)
	if err != nil {
		return nil, fmt.Errorf("chatgpt_web: %w", err)
	}

	p.logger.Info("chatgpt_web: received response", "len", len(response))

	return &domain.ChatResponse{
		Content:      response,
		FinishReason: "stop",
	}, nil
}

// Login opens a visible browser for the user to log in to ChatGPT.
func (p *ChatGPTWeb) Login(ctx context.Context) error {
	return p.bridge.Login(ctx, p.selectors.URL)
}
