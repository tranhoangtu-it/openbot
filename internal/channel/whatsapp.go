package channel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"time"

	"openbot/internal/config"
	"openbot/internal/domain"
)

const whatsappAPIBase = "https://graph.facebook.com/v21.0"

// WhatsApp implements domain.Channel for WhatsApp Business Cloud API.
type WhatsApp struct {
	cfg    config.WhatsAppConfig
	bus    domain.MessageBus
	logger *slog.Logger
	client *http.Client
	mux    *http.ServeMux
}

type WhatsAppChannelConfig struct {
	Config config.WhatsAppConfig
	Logger *slog.Logger
}

func NewWhatsApp(cfg WhatsAppChannelConfig) *WhatsApp {
	return &WhatsApp{
		cfg:    cfg.Config,
		logger: cfg.Logger,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (w *WhatsApp) Name() string { return "whatsapp" }

func (w *WhatsApp) Start(ctx context.Context, bus domain.MessageBus) error {
	w.bus = bus

	// Register outbound handler — skip stream-only events (tokens, tool deltas)
	// since WhatsApp doesn't support incremental streaming.
	bus.OnOutbound("whatsapp", func(msg domain.OutboundMessage) {
		if msg.StreamEvent != nil && msg.Content == "" {
			return
		}
		if err := w.sendMessage(ctx, msg.ChatID, msg.Content); err != nil {
			w.logger.Error("whatsapp send failed", "err", err, "chat", msg.ChatID)
		}
	})

	w.mux = http.NewServeMux()
	webhookPath := w.cfg.WebhookPath
	if webhookPath == "" {
		webhookPath = "/webhook/whatsapp"
	}

	w.mux.HandleFunc("GET "+webhookPath, w.handleVerification)
	w.mux.HandleFunc("POST "+webhookPath, w.handleIncoming)

	w.logger.Info("whatsapp channel ready", "webhook", webhookPath)
	return nil
}

func (w *WhatsApp) Stop() error { return nil }

func (w *WhatsApp) Send(ctx context.Context, chatID string, content string) error {
	return w.sendMessage(ctx, chatID, content)
}

// Handler returns the HTTP handler for the WhatsApp webhook (to be mounted on the main mux).
func (w *WhatsApp) Handler() http.Handler {
	if w.mux == nil {
		return http.NotFoundHandler()
	}
	return w.mux
}

// --- Webhook handlers ---

// handleVerification handles the WhatsApp webhook verification challenge.
func (w *WhatsApp) handleVerification(rw http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	if mode == "subscribe" && token == w.cfg.VerifyToken {
		w.logger.Info("whatsapp webhook verified")
		rw.WriteHeader(http.StatusOK)
		fmt.Fprint(rw, html.EscapeString(challenge))
		return
	}

	w.logger.Warn("whatsapp webhook verification failed", "mode", mode)
	http.Error(rw, "Forbidden", http.StatusForbidden)
}

// handleIncoming processes incoming WhatsApp messages.
func (w *WhatsApp) handleIncoming(rw http.ResponseWriter, r *http.Request) {
	// Verify signature
	if w.cfg.AppSecret != "" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(rw, "Bad request", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		sig := r.Header.Get("X-Hub-Signature-256")
		if !w.verifySignature(body, sig) {
			w.logger.Warn("whatsapp invalid signature")
			http.Error(rw, "Forbidden", http.StatusForbidden)
			return
		}
	}

	var payload waPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.logger.Warn("whatsapp bad payload", "err", err)
		http.Error(rw, "Bad request", http.StatusBadRequest)
		return
	}

	// Process messages
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			if change.Value.Messages == nil {
				continue
			}
			for _, msg := range change.Value.Messages {
				if msg.Type != "text" || msg.Text == nil {
					continue
				}

				w.logger.Info("whatsapp message received",
					"from", msg.From, "text_len", len(msg.Text.Body))

				w.bus.Publish(domain.InboundMessage{
					Channel:   "whatsapp",
					ChatID:    msg.From,
					SenderID:  msg.From,
					Content:   msg.Text.Body,
					Timestamp: time.Now(),
				})
			}
		}
	}

	rw.WriteHeader(http.StatusOK)
}

// verifySignature checks the X-Hub-Signature-256 header.
func (w *WhatsApp) verifySignature(body []byte, signature string) bool {
	if len(signature) < 7 || signature[:7] != "sha256=" {
		return false
	}
	expected := signature[7:]

	mac := hmac.New(sha256.New, []byte(w.cfg.AppSecret))
	mac.Write(body)
	computed := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(computed))
}

// sendMessage sends a text message via WhatsApp Cloud API.
func (w *WhatsApp) sendMessage(ctx context.Context, to string, text string) error {
	url := fmt.Sprintf("%s/%s/messages", whatsappAPIBase, w.cfg.PhoneNumberID)

	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]string{"body": text},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.cfg.AccessToken)

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("whatsapp API %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// --- WhatsApp webhook payload types ---

type waPayload struct {
	Object string    `json:"object"`
	Entry  []waEntry `json:"entry"`
}

type waEntry struct {
	ID      string     `json:"id"`
	Changes []waChange `json:"changes"`
}

type waChange struct {
	Value waValue `json:"value"`
	Field string  `json:"field"`
}

type waValue struct {
	MessagingProduct string      `json:"messaging_product"`
	Messages         []waMessage `json:"messages"`
}

type waMessage struct {
	From string  `json:"from"`
	ID   string  `json:"id"`
	Type string  `json:"type"`
	Text *waText `json:"text,omitempty"`
}

type waText struct {
	Body string `json:"body"`
}
