package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// TTSConfig configures the text-to-speech provider.
type TTSConfig struct {
	Provider string // "openai" | "elevenlabs"
	APIBase  string
	APIKey   string
	Model    string // e.g., "tts-1" (OpenAI) or voice ID (ElevenLabs)
	Voice    string // e.g., "alloy", "echo", "fable", "onyx", "nova", "shimmer" (OpenAI)
	Logger   *slog.Logger
}

// TTSProvider handles text-to-speech synthesis.
type TTSProvider struct {
	provider string
	apiBase  string
	apiKey   string
	model    string
	voice    string
	client   *http.Client
	logger   *slog.Logger
}

// NewTTSProvider creates a new text-to-speech provider.
func NewTTSProvider(cfg TTSConfig) *TTSProvider {
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "tts-1"
	}
	if cfg.Voice == "" {
		cfg.Voice = "alloy"
	}
	return &TTSProvider{
		provider: cfg.Provider,
		apiBase:  cfg.APIBase,
		apiKey:   cfg.APIKey,
		model:    cfg.Model,
		voice:    cfg.Voice,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		logger: cfg.Logger,
	}
}

// Synthesize converts text to speech audio (MP3 format).
// Returns an io.ReadCloser with the audio data.
func (t *TTSProvider) Synthesize(ctx context.Context, text string) (io.ReadCloser, error) {
	switch t.provider {
	case "openai":
		return t.synthesizeOpenAI(ctx, text)
	case "elevenlabs":
		return t.synthesizeElevenLabs(ctx, text)
	default:
		return nil, fmt.Errorf("unsupported TTS provider: %s", t.provider)
	}
}

func (t *TTSProvider) synthesizeOpenAI(ctx context.Context, text string) (io.ReadCloser, error) {
	body := fmt.Sprintf(`{"model":"%s","input":"%s","voice":"%s"}`,
		t.model, escapeJSON(text), t.voice)

	url := t.apiBase + "/audio/speech"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("TTS API request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("TTS API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return resp.Body, nil
}

func (t *TTSProvider) synthesizeElevenLabs(ctx context.Context, text string) (io.ReadCloser, error) {
	voiceID := t.voice
	if voiceID == "" {
		voiceID = "21m00Tcm4TlvDq8ikWAM" // default ElevenLabs voice
	}

	body := fmt.Sprintf(`{"text":"%s","model_id":"eleven_monolingual_v1"}`, escapeJSON(text))

	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", voiceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("xi-api-key", t.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ElevenLabs API request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ElevenLabs API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return resp.Body, nil
}

func escapeJSON(s string) string {
	var buf bytes.Buffer
	for _, c := range s {
		switch c {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			buf.WriteRune(c)
		}
	}
	return buf.String()
}
