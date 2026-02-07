package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"openbot/internal/domain"
)

const apiGatewayMaxBodySize = 1 << 20 // 1MB

// APIGateway exposes an OpenAI-compatible /v1/chat/completions endpoint
// that routes through the existing agent loop.
type APIGateway struct {
	port    int
	apiKey  string
	bus     domain.MessageBus
	logger  *slog.Logger
	server  *http.Server

	// Pending responses keyed by request ID
	pending   map[string]chan string
	pendingMu sync.Mutex
}

type APIGatewayConfig struct {
	Port   int
	APIKey string
	Logger *slog.Logger
}

func NewAPIGateway(cfg APIGatewayConfig) *APIGateway {
	return &APIGateway{
		port:    cfg.Port,
		apiKey:  cfg.APIKey,
		logger:  cfg.Logger,
		pending: make(map[string]chan string),
	}
}

func (g *APIGateway) Name() string { return "api_gateway" }

func (g *APIGateway) Start(ctx context.Context, bus domain.MessageBus) error {
	g.bus = bus

	bus.OnOutbound("api_gateway", func(msg domain.OutboundMessage) {
		// Route final responses back
		if msg.StreamEvent != nil && msg.StreamEvent.Type != domain.StreamDone {
			return // skip intermediate stream events for now
		}
		content := msg.Content
		if content == "" && msg.StreamEvent != nil {
			content = msg.StreamEvent.Content
		}

		g.pendingMu.Lock()
		ch, ok := g.pending[msg.ChatID]
		g.pendingMu.Unlock()
		if ok {
			select {
			case ch <- content:
			default:
			}
		}
	})

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", g.handleChatCompletions)
	mux.HandleFunc("GET /v1/models", g.handleModels)

	addr := fmt.Sprintf(":%d", g.port)
	g.server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      150 * time.Second, // allow time for LLM response
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	g.logger.Info("API gateway started", "addr", addr)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		g.server.Shutdown(shutdownCtx)
	}()

	if err := g.server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (g *APIGateway) Stop() error {
	if g.server != nil {
		return g.server.Close()
	}
	return nil
}

func (g *APIGateway) Send(ctx context.Context, chatID string, content string) error {
	return nil
}

// handleChatCompletions is OpenAI-compatible POST /v1/chat/completions.
func (g *APIGateway) handleChatCompletions(rw http.ResponseWriter, r *http.Request) {
	// Auth check
	if g.apiKey != "" {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != g.apiKey {
			rw.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(rw).Encode(map[string]string{"error": "invalid API key"})
			return
		}
	}

	// Parse request (with size limit)
	body, err := io.ReadAll(io.LimitReader(r.Body, apiGatewayMaxBodySize))
	if err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "bad request"})
		return
	}

	var req oaiCompatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	// Extract the last user message
	var userMessage string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			userMessage = req.Messages[i].Content
			break
		}
	}
	if userMessage == "" {
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "no user message found"})
		return
	}

	// Create a unique request ID
	reqID := fmt.Sprintf("api_%d", time.Now().UnixNano())

	responseCh := make(chan string, 1)
	g.pendingMu.Lock()
	g.pending[reqID] = responseCh
	g.pendingMu.Unlock()

	defer func() {
		g.pendingMu.Lock()
		delete(g.pending, reqID)
		g.pendingMu.Unlock()
	}()

	// Publish to the agent loop
	g.bus.Publish(domain.InboundMessage{
		Channel:   "api_gateway",
		ChatID:    reqID,
		SenderID:  "api",
		Content:   userMessage,
		Timestamp: time.Now(),
	})

	// Wait for response
	timeout := time.NewTimer(120 * time.Second)
	defer timeout.Stop()

	var content string
	select {
	case content = <-responseCh:
	case <-timeout.C:
		rw.WriteHeader(http.StatusGatewayTimeout)
		json.NewEncoder(rw).Encode(map[string]string{"error": "request timed out"})
		return
	case <-r.Context().Done():
		return
	}

	// Build OpenAI-compatible response
	resp := oaiCompatResponse{
		ID:      "chatcmpl-" + reqID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []oaiCompatChoice{{
			Index: 0,
			Message: oaiCompatMessage{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: "stop",
		}},
	}

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(resp)
}

func (g *APIGateway) handleModels(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]any{
		"object": "list",
		"data": []map[string]any{
			{"id": "openbot", "object": "model", "owned_by": "openbot"},
		},
	})
}

// --- OpenAI-compatible request/response types ---

type oaiCompatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiCompatRequest struct {
	Model    string             `json:"model"`
	Messages []oaiCompatMessage `json:"messages"`
	Stream   bool               `json:"stream"`
}

type oaiCompatChoice struct {
	Index        int              `json:"index"`
	Message      oaiCompatMessage `json:"message"`
	FinishReason string           `json:"finish_reason"`
}

type oaiCompatResponse struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []oaiCompatChoice `json:"choices"`
}
