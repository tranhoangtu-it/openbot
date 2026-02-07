package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"openbot/internal/config"
	"openbot/internal/domain"
)

// ProviderConstructor is a function that creates a provider from a config entry.
type ProviderConstructor func(pc config.ProviderConfig, logger *slog.Logger) domain.Provider

// Factory creates and caches LLM providers from config.
type Factory struct {
	cfg          *config.Config
	logger       *slog.Logger
	constructors map[string]ProviderConstructor
	cache        map[string]domain.Provider
	mu           sync.RWMutex
}

// NewFactory creates a provider factory with the built-in constructors registered.
func NewFactory(cfg *config.Config, logger *slog.Logger) *Factory {
	f := &Factory{
		cfg:          cfg,
		logger:       logger,
		constructors: make(map[string]ProviderConstructor),
		cache:        make(map[string]domain.Provider),
	}
	f.registerDefaults()
	return f
}

// RegisterConstructor adds (or replaces) a provider constructor by name.
// This allows third-party or plugin providers to be registered at runtime.
func (f *Factory) RegisterConstructor(name string, ctor ProviderConstructor) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.constructors[name] = ctor
}

// registerDefaults registers all built-in provider constructors.
func (f *Factory) registerDefaults() {
	f.constructors["ollama"] = func(pc config.ProviderConfig, logger *slog.Logger) domain.Provider {
		return NewOllama(OllamaConfig{APIBase: pc.APIBase, DefaultModel: pc.DefaultModel, Logger: logger})
	}
	f.constructors["ollama-cloud"] = f.constructors["ollama"]

	f.constructors["openai"] = func(pc config.ProviderConfig, logger *slog.Logger) domain.Provider {
		return NewOpenAI(OpenAIConfig{APIKey: pc.APIKey, APIBase: pc.APIBase, Model: pc.DefaultModel, Logger: logger})
	}

	f.constructors["claude"] = func(pc config.ProviderConfig, logger *slog.Logger) domain.Provider {
		return NewClaude(ClaudeConfig{APIKey: pc.APIKey, Model: pc.DefaultModel, Logger: logger})
	}

	f.constructors["chatgpt"] = func(pc config.ProviderConfig, logger *slog.Logger) domain.Provider {
		return NewChatGPTWeb(ChatGPTWebConfig{ProfileDir: pc.ProfileDir, Selectors: pc.Selectors, Logger: logger})
	}

	f.constructors["gemini"] = func(pc config.ProviderConfig, logger *slog.Logger) domain.Provider {
		if pc.Mode == "browser" {
			return NewGeminiWeb(GeminiWebConfig{ProfileDir: pc.ProfileDir, Selectors: pc.Selectors, Logger: logger})
		}
		// API mode — use OpenAI-compatible endpoint (Gemini supports this).
		return NewOpenAI(OpenAIConfig{APIKey: pc.APIKey, APIBase: pc.APIBase, Model: pc.DefaultModel, Logger: logger})
	}
}

// Get returns the provider with the given name, or the default if name is empty.
// Created providers are cached so the same instance is reused across calls.
// Uses double-check locking to avoid TOCTOU races.
func (f *Factory) Get(name string) (domain.Provider, error) {
	if name == "" {
		name = f.cfg.General.DefaultProvider
	}

	// Fast path: read lock.
	f.mu.RLock()
	if cached, ok := f.cache[name]; ok {
		f.mu.RUnlock()
		return cached, nil
	}
	f.mu.RUnlock()

	// Slow path: write lock with double-check.
	f.mu.Lock()
	defer f.mu.Unlock()

	// Re-check under write lock (another goroutine may have created it).
	if cached, ok := f.cache[name]; ok {
		return cached, nil
	}

	pc, ok := f.cfg.Providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
	if !pc.Enabled {
		return nil, fmt.Errorf("provider %s is disabled", name)
	}

	ctor, found := f.constructors[name]

	var p domain.Provider
	if found {
		p = ctor(pc, f.logger)
	} else if pc.APIBase != "" && pc.APIKey != "" {
		// Fallback: treat unknown providers as OpenAI-compatible.
		p = NewOpenAI(OpenAIConfig{APIKey: pc.APIKey, APIBase: pc.APIBase, Model: pc.DefaultModel, Logger: f.logger})
	} else {
		return nil, fmt.Errorf("provider %s: no constructor registered and no API base/key configured", name)
	}

	f.cache[name] = p
	return p, nil
}

// DefaultProvider returns the configured default provider.
func (f *Factory) DefaultProvider() (domain.Provider, error) {
	return f.Get("")
}

// HealthyProvider returns the first provider that passes a health check, or nil.
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
