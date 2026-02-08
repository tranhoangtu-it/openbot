package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"
)

// WhisperConfig configures the Whisper speech-to-text provider.
type WhisperConfig struct {
	APIBase  string // e.g., "https://api.groq.com/openai/v1" or "https://api.openai.com/v1"
	APIKey   string
	Model    string // e.g., "whisper-large-v3" (Groq) or "whisper-1" (OpenAI)
	Language string // optional: ISO-639-1 language code
	Logger   *slog.Logger
}

// WhisperProvider handles speech-to-text transcription using the OpenAI-compatible Whisper API.
type WhisperProvider struct {
	apiBase  string
	apiKey   string
	model    string
	language string
	client   *http.Client
	logger   *slog.Logger
}

// NewWhisperProvider creates a new Whisper transcription provider.
func NewWhisperProvider(cfg WhisperConfig) *WhisperProvider {
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.groq.com/openai/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "whisper-large-v3"
	}
	return &WhisperProvider{
		apiBase:  cfg.APIBase,
		apiKey:   cfg.APIKey,
		model:    cfg.Model,
		language: cfg.Language,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		logger: cfg.Logger,
	}
}

// TranscriptionResult contains the result of a transcription.
type TranscriptionResult struct {
	Text     string  `json:"text"`
	Language string  `json:"language,omitempty"`
	Duration float64 `json:"duration,omitempty"`
}

// Transcribe converts audio data to text.
// audioData is the raw audio bytes, filename should include the extension (e.g., "audio.ogg").
func (w *WhisperProvider) Transcribe(ctx context.Context, audioData io.Reader, filename string) (*TranscriptionResult, error) {
	// Build multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, audioData); err != nil {
		return nil, fmt.Errorf("copy audio data: %w", err)
	}

	writer.WriteField("model", w.model)
	writer.WriteField("response_format", "json")
	if w.language != "" {
		writer.WriteField("language", w.language)
	}
	writer.Close()

	url := w.apiBase + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+w.apiKey)

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whisper API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("whisper API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result TranscriptionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode whisper response: %w", err)
	}

	w.logger.Info("transcription complete",
		"text_len", len(result.Text),
		"language", result.Language,
		"duration", result.Duration,
	)

	return &result, nil
}
