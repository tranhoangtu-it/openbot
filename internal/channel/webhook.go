package channel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"openbot/internal/domain"
)

// WebhookConfig configures the webhook channel.
type WebhookConfig struct {
	Port   int
	Path   string // webhook URL path (default: /webhook)
	Secret string // HMAC secret for verifying webhook signatures
	Logger *slog.Logger
}

// Webhook implements a channel that accepts HTTP POST requests to trigger the agent.
type Webhook struct {
	port   int
	path   string
	secret string
	bus    domain.MessageBus
	logger *slog.Logger
	server *http.Server
}

// WebhookPayload is the expected JSON body for webhook requests.
type WebhookPayload struct {
	Channel string `json:"channel"` // source channel identifier
	ChatID  string `json:"chat_id"` // target chat/conversation ID
	UserID  string `json:"user_id"` // sender identifier
	Content string `json:"content"` // message content
}

// NewWebhook creates a new webhook channel handler.
func NewWebhook(cfg WebhookConfig) *Webhook {
	if cfg.Path == "" {
		cfg.Path = "/webhook"
	}
	if cfg.Port == 0 {
		cfg.Port = 9090
	}
	return &Webhook{
		port:   cfg.Port,
		path:   cfg.Path,
		secret: cfg.Secret,
		logger: cfg.Logger,
	}
}

func (w *Webhook) Name() string { return "webhook" }

// Start begins the webhook HTTP server.
func (w *Webhook) Start(ctx context.Context, bus domain.MessageBus) error {
	w.bus = bus

	mux := http.NewServeMux()
	mux.HandleFunc(w.path, w.handleWebhook)

	w.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", w.port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Register outbound handler (webhooks typically don't need responses, but support it).
	bus.OnOutbound("webhook", func(msg domain.OutboundMessage) {
		// Webhook responses could be logged or forwarded.
		if msg.Content != "" {
			w.logger.Debug("webhook outbound (not forwarded)", "chat_id", msg.ChatID, "content_len", len(msg.Content))
		}
	})

	w.logger.Info("webhook server starting", "port", w.port, "path", w.path)

	errCh := make(chan error, 1)
	go func() {
		if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		w.logger.Info("webhook server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return w.server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return fmt.Errorf("webhook server: %w", err)
	}
}

func (w *Webhook) handleWebhook(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB max
	if err != nil {
		http.Error(rw, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Verify HMAC signature if secret is configured.
	if w.secret != "" {
		sig := r.Header.Get("X-Signature-256")
		if sig == "" {
			http.Error(rw, "Missing signature", http.StatusUnauthorized)
			return
		}
		if !verifyHMAC(body, w.secret, sig) {
			http.Error(rw, "Invalid signature", http.StatusForbidden)
			return
		}
	}

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(rw, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if payload.Content == "" {
		http.Error(rw, "Content is required", http.StatusBadRequest)
		return
	}

	if payload.Channel == "" {
		payload.Channel = "webhook"
	}
	if payload.ChatID == "" {
		payload.ChatID = "webhook-default"
	}
	if payload.UserID == "" {
		payload.UserID = "webhook"
	}

	w.logger.Info("webhook received",
		"channel", payload.Channel,
		"chat_id", payload.ChatID,
		"user_id", payload.UserID,
		"content_len", len(payload.Content),
	)

	w.bus.Publish(domain.InboundMessage{
		Channel:   payload.Channel,
		ChatID:    payload.ChatID,
		SenderID:  payload.UserID,
		Content:   payload.Content,
		Timestamp: time.Now(),
	})

	rw.WriteHeader(http.StatusAccepted)
	json.NewEncoder(rw).Encode(map[string]string{
		"status": "accepted",
	})
}

// verifyHMAC verifies the HMAC-SHA256 signature of the body.
func verifyHMAC(body []byte, secret, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
