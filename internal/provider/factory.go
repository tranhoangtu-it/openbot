package provider

import (
	"context"
	"fmt"
	"log/slog"
	"openbot/internal/config"
	"openbot/internal/domain"
)

// Factory creates LLM providers from config.
type Factory struct {
	cfg    *config.Config
	logger *slog.Logger
}

func NewFactory(cfg *config.Config, logger *slog.Logger) *Factory {
	return &Factory{cfg: cfg, logger: logger}
}

// Get returns the provider with the given name, or the default provider if name is empty.
func (f *Factory) Get(name string) (domain.Provider, error) {
	if name == "" {
		name = f.cfg.General.DefaultProvider
	}

	pc, ok := f.cfg.Providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
	if !pc.Enabled {
		return nil, fmt.Errorf("provider %s is disabled", name)
	}

	switch name {
	case "ollama", "ollama-cloud":
		return NewOllama(OllamaConfig{
			APIBase:      pc.APIBase,
			DefaultModel: pc.DefaultModel,
			Logger:       f.logger,
		}), nil
	case "chatgpt":
		return NewChatGPTWeb(ChatGPTWebConfig{
			ProfileDir: pc.ProfileDir,
			Selectors:  pc.Selectors,
			Logger:     f.logger,
		}), nil
	case "gemini":
		if pc.Mode == "browser" {
			return NewGeminiWeb(GeminiWebConfig{
				ProfileDir: pc.ProfileDir,
				Selectors:  pc.Selectors,
				Logger:     f.logger,
			}), nil
		}
		// fallthrough to OpenAI-compatible for API mode
		return NewOpenAI(OpenAIConfig{
			APIKey:  pc.APIKey,
			APIBase: pc.APIBase,
			Model:   pc.DefaultModel,
			Logger:  f.logger,
		}), nil
	case "openai":
		return NewOpenAI(OpenAIConfig{
			APIKey:  pc.APIKey,
			APIBase: pc.APIBase,
			Model:   pc.DefaultModel,
			Logger:  f.logger,
		}), nil
	case "claude":
		return NewClaude(ClaudeConfig{
			APIKey: pc.APIKey,
			Model:  pc.DefaultModel,
			Logger: f.logger,
		}), nil
	default:
		// Try as OpenAI-compatible (e.g., deepseek, groq, openrouter)
		if pc.APIBase != "" && pc.APIKey != "" {
			return NewOpenAI(OpenAIConfig{
				APIKey:  pc.APIKey,
				APIBase: pc.APIBase,
				Model:   pc.DefaultModel,
				Logger:  f.logger,
			}), nil
		}
		return nil, fmt.Errorf("provider %s: no API base or API key configured", name)
	}
}

func (f *Factory) DefaultProvider() (domain.Provider, error) {
	return f.Get("")
}

// HealthyProvider returns the first provider that passes Healthy check, or nil.
func (f *Factory) HealthyProvider(ctx context.Context) domain.Provider {
	for name := range f.cfg.Providers {
		p, err := f.Get(name)
		if err != nil || p == nil {
			continue
		}
		if p.Healthy(ctx) == nil {
			return p
		}
	}
	return nil
}
