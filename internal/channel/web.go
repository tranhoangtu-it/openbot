package channel

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"openbot/internal/config"
	"openbot/internal/domain"
	"openbot/internal/metrics"
)

const (
	maxFormSize       = 1 << 20 // 1MB
	maxBodySize       = 1 << 20
	requestTimeout    = 120 * time.Second
	sessionCookieName = "openbot_session"
	sessionMaxAge     = 86400 * 30 // 30 days
)

//go:embed web_templates/*.html
var templateFS embed.FS

//go:embed web_assets/*
var assetsFS embed.FS

// Web implements domain.Channel for the Web UI.
type Web struct {
	host    string
	port    int
	bus     domain.MessageBus
	logger  *slog.Logger
	server  *http.Server
	tmpl    *htmltemplate.Template
	version string
	store   domain.MemoryStore // database for conversations API

	// Config reference for settings API (protected by cfgMu)
	cfg     *config.Config
	cfgPath string
	cfgMu   sync.RWMutex

	// Auth settings
	authEnabled  bool
	authUser     string
	authPassHash string

	// SSE clients keyed by session ID for targeted delivery
	sseClients   map[string]chan sseEvent
	sseClientsMu sync.RWMutex

	// Pending responses keyed by session ID
	pendingResponses   map[string]chan string
	pendingResponsesMu sync.Mutex
}

// sseEvent is a structured SSE event sent to the browser.
type sseEvent struct {
	Type    string `json:"type"`              // thinking | token | tool_start | tool_end | done | error | message
	Content string `json:"content,omitempty"`
	Tool    string `json:"tool,omitempty"`
	ToolID  string `json:"tool_id,omitempty"`
}

type WebConfig struct {
	Host       string
	Port       int
	Logger     *slog.Logger
	Config     *config.Config
	ConfigPath string
	Version    string
	Store      domain.MemoryStore // optional: for conversations API
}

func NewWeb(cfg WebConfig) *Web {
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}

	tmpl := htmltemplate.Must(htmltemplate.ParseFS(templateFS, "web_templates/*.html"))

	w := &Web{
		host:             cfg.Host,
		port:             cfg.Port,
		logger:           cfg.Logger,
		tmpl:             tmpl,
		version:          cfg.Version,
		cfg:              cfg.Config,
		cfgPath:          cfg.ConfigPath,
		store:            cfg.Store,
		sseClients:       make(map[string]chan sseEvent),
		pendingResponses: make(map[string]chan string),
	}

	// Apply auth settings from config
	if cfg.Config != nil && cfg.Config.Channels.Web.Auth.Enabled {
		w.authEnabled = true
		w.authUser = cfg.Config.Channels.Web.Auth.Username
		w.authPassHash = cfg.Config.Channels.Web.Auth.PasswordHash
	}

	return w
}

func (w *Web) Name() string { return "web" }

// Start starts the web server.
func (w *Web) Start(ctx context.Context, bus domain.MessageBus) error {
	w.bus = bus

	// Register outbound handler — routes responses back to the correct session
	bus.OnOutbound("web", func(msg domain.OutboundMessage) {
		// Handle stream events (token-by-token, tool start/end, etc.)
		if msg.StreamEvent != nil {
			evt := sseEvent{
				Type:    string(msg.StreamEvent.Type),
				Content: msg.StreamEvent.Content,
				Tool:    msg.StreamEvent.Tool,
				ToolID:  msg.StreamEvent.ToolID,
			}
			w.sendSSEEvent(msg.ChatID, evt)

			// When stream is done, also deliver to the pending response channel
			if msg.StreamEvent.Type == domain.StreamDone && msg.Content != "" {
				w.pendingResponsesMu.Lock()
				ch, ok := w.pendingResponses[msg.ChatID]
				w.pendingResponsesMu.Unlock()
				if ok {
					select {
					case ch <- msg.Content:
					default:
					}
				}
			}
			return
		}

		// Legacy: non-streaming outbound (full response at once)
		w.pendingResponsesMu.Lock()
		ch, ok := w.pendingResponses[msg.ChatID]
		w.pendingResponsesMu.Unlock()
		if ok {
			select {
			case ch <- msg.Content:
			default:
			}
		}
		w.sendSSEEvent(msg.ChatID, sseEvent{Type: "message", Content: msg.Content})
	})

	mux := http.NewServeMux()

	// Static assets (logo, JS, CSS) — served from embedded web_assets/
	assetsHandler := http.FileServer(http.FS(assetsFS))
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		name := r.URL.Path
		r.URL.Path = "web_assets/" + name
		rw.Header().Set("Cache-Control", "public, max-age=86400")
		// Ensure correct Content-Type for embedded JS/CSS (net/http may not detect these)
		switch {
		case strings.HasSuffix(name, ".js"):
			rw.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		case strings.HasSuffix(name, ".css"):
			rw.Header().Set("Content-Type", "text/css; charset=utf-8")
		}
		assetsHandler.ServeHTTP(rw, r)
	})))

	mux.HandleFunc("GET /", w.requireAuth(w.handleDashboard))
	mux.HandleFunc("GET /chat", w.requireAuth(w.handleChat))
	mux.HandleFunc("POST /chat/send", w.requireAuth(w.handleSend))
	mux.HandleFunc("GET /chat/stream", w.requireAuth(w.handleSSE))
	mux.HandleFunc("GET /status", w.handleStatus) // public endpoint
	mux.HandleFunc("POST /chat/clear", w.requireAuth(w.handleClear))

	// Conversations API
	mux.HandleFunc("GET /api/conversations", w.requireAuth(w.handleListConversations))
	mux.HandleFunc("POST /api/conversations", w.requireAuth(w.handleCreateConversation))
	mux.HandleFunc("GET /api/conversations/{id}/messages", w.requireAuth(w.handleGetConversationMessages))
	mux.HandleFunc("DELETE /api/conversations/{id}", w.requireAuth(w.handleDeleteConversation))

	// Stats API
	mux.HandleFunc("GET /api/stats", w.requireAuth(w.handleStats))
	mux.HandleFunc("GET /api/system", w.requireAuth(w.handleSystemInfo))

	// Settings page + API (always requires auth)
	mux.HandleFunc("GET /settings", w.requireAuth(w.handleSettings))
	mux.HandleFunc("GET /api/config", w.requireAuth(w.handleGetConfig))
	mux.HandleFunc("PUT /api/config", w.requireAuth(w.handleUpdateConfig))
	mux.HandleFunc("POST /api/config/save", w.requireAuth(w.handleSaveConfig))

	// Prometheus-compatible metrics endpoint
	mux.HandleFunc("GET /metrics", metrics.Collector.Handler())

	addr := fmt.Sprintf("%s:%d", w.host, w.port)
	w.server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
		// WriteTimeout intentionally omitted: SSE requires long-lived writes.
	}

	w.logger.Info("web UI started", "addr", "http://"+addr, "auth", w.authEnabled)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		w.server.Shutdown(shutdownCtx)
	}()

	if err := w.server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// requireAuth wraps a handler with HTTP Basic Auth when auth is enabled.
func (w *Web) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if !w.authEnabled {
			next(rw, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || !w.checkCredentials(user, pass) {
			rw.Header().Set("WWW-Authenticate", `Basic realm="OpenBot"`)
			http.Error(rw, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(rw, r)
	}
}

// checkCredentials verifies username and password against stored hash.
func (w *Web) checkCredentials(user, pass string) bool {
	if subtle.ConstantTimeCompare([]byte(user), []byte(w.authUser)) != 1 {
		return false
	}
	// If passwordHash is a raw SHA-256 hex, compare that way
	hash := sha256.Sum256([]byte(pass))
	got := hex.EncodeToString(hash[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(w.authPassHash)) == 1
}

func (w *Web) Stop() error {
	if w.server != nil {
		return w.server.Close()
	}
	return nil
}

func (w *Web) Send(ctx context.Context, chatID string, content string) error {
	w.sendSSE(chatID, content)
	return nil
}

// getOrCreateSession returns a persistent session ID from cookies.
// If no session exists, creates a new one and sets the cookie.
func (w *Web) getOrCreateSession(r *http.Request, rw http.ResponseWriter) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// Generate a random session ID
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID
		sessionID := fmt.Sprintf("web_%d", time.Now().UnixNano())
		w.logger.Warn("rand.Read failed, using fallback session ID", "err", err)
		http.SetCookie(rw, &http.Cookie{
			Name:     sessionCookieName,
			Value:    sessionID,
			Path:     "/",
			MaxAge:   sessionMaxAge,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   true,
		})
		return sessionID
	}
	sessionID := "web_" + hex.EncodeToString(b)

	http.SetCookie(rw, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	})
	w.logger.Info("new web session created", "session", sessionID)
	return sessionID
}

func (w *Web) handleDashboard(rw http.ResponseWriter, r *http.Request) {
	if err := w.tmpl.ExecuteTemplate(rw, "dashboard.html", map[string]any{
		"Title": "OpenBot Dashboard",
		"Time":  time.Now().Format("2006-01-02 15:04:05"),
	}); err != nil {
		w.logger.Error("template error", "template", "dashboard", "err", err)
	}
}

func (w *Web) handleChat(rw http.ResponseWriter, r *http.Request) {
	w.getOrCreateSession(r, rw)
	if err := w.tmpl.ExecuteTemplate(rw, "chat.html", map[string]any{
		"Title": "OpenBot Chat",
	}); err != nil {
		w.logger.Error("template error", "template", "chat", "err", err)
	}
}

func (w *Web) handleSend(rw http.ResponseWriter, r *http.Request) {
	_ = r.ParseMultipartForm(maxFormSize)
	message := r.FormValue("message")
	if message == "" {
		rw.Header().Set("Content-Type", "application/json; charset=utf-8")
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "empty message"})
		return
	}

	sessionID := w.getOrCreateSession(r, rw)
	provider := r.FormValue("provider") // optional: per-message provider switch

	// Check if the client wants to use streaming (via SSE) or blocking mode.
	// If "stream=true" is set (or the SSE client is connected), return 202 immediately.
	streamMode := r.FormValue("stream") == "true"

	inbound := domain.InboundMessage{
		Channel:   "web",
		ChatID:    sessionID,
		SenderID:  "web_user",
		Content:   message,
		Timestamp: time.Now(),
		Provider:  provider,
	}

	if streamMode {
		// Non-blocking: publish and return 202
		w.bus.Publish(inbound)
		rw.Header().Set("Content-Type", "application/json; charset=utf-8")
		rw.WriteHeader(http.StatusAccepted)
		json.NewEncoder(rw).Encode(map[string]string{"status": "accepted", "session": sessionID})
		return
	}

	// Blocking mode: wait for full response (legacy behavior)
	responseCh := make(chan string, 1)
	w.pendingResponsesMu.Lock()
	if oldCh, exists := w.pendingResponses[sessionID]; exists {
		close(oldCh)
	}
	w.pendingResponses[sessionID] = responseCh
	w.pendingResponsesMu.Unlock()
	defer func() {
		w.pendingResponsesMu.Lock()
		if ch, ok := w.pendingResponses[sessionID]; ok && ch == responseCh {
			delete(w.pendingResponses, sessionID)
		}
		w.pendingResponsesMu.Unlock()
	}()

	w.bus.Publish(inbound)

	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	timeout := time.NewTimer(requestTimeout)
	defer timeout.Stop()
	select {
	case resp, ok := <-responseCh:
		if ok {
			json.NewEncoder(rw).Encode(map[string]string{"content": resp})
		} else {
			rw.WriteHeader(http.StatusConflict)
			json.NewEncoder(rw).Encode(map[string]string{"error": "Superseded by new request"})
		}
	case <-timeout.C:
		rw.WriteHeader(http.StatusGatewayTimeout)
		json.NewEncoder(rw).Encode(map[string]string{"error": "Request timed out"})
	case <-r.Context().Done():
		w.logger.Info("web client disconnected", "session", sessionID)
	}
}

func (w *Web) handleClear(rw http.ResponseWriter, r *http.Request) {
	// Clear session by setting an expired cookie — next request creates new session
	http.SetCookie(rw, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(rw).Encode(map[string]string{"status": "session cleared"})
}

func (w *Web) handleSSE(rw http.ResponseWriter, r *http.Request) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "SSE not supported", http.StatusInternalServerError)
		return
	}

	sessionID := w.getOrCreateSession(r, rw)

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	ch := make(chan sseEvent, 50)

	w.sseClientsMu.Lock()
	w.sseClients[sessionID] = ch
	w.sseClientsMu.Unlock()

	defer func() {
		w.sseClientsMu.Lock()
		if existing, ok := w.sseClients[sessionID]; ok && existing == ch {
			delete(w.sseClients, sessionID)
		}
		w.sseClientsMu.Unlock()
	}()

	// Send initial connection event
	data, _ := json.Marshal(sseEvent{Type: "connected"})
	fmt.Fprintf(rw, "data: %s\n\n", data)
	flusher.Flush()

	ctx := r.Context()
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-ch:
			data, _ := json.Marshal(evt)
			fmt.Fprintf(rw, "data: %s\n\n", data)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(rw, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// --- Conversations API ---

func (w *Web) handleListConversations(rw http.ResponseWriter, r *http.Request) {
	if w.store == nil {
		http.Error(rw, `{"error":"store not available"}`, http.StatusServiceUnavailable)
		return
	}
	convs, err := w.store.ListConversations(r.Context(), 50)
	if err != nil {
		w.logger.Error("list conversations", "err", err)
		rw.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(convs)
}

func (w *Web) handleCreateConversation(rw http.ResponseWriter, r *http.Request) {
	sessionID := w.getOrCreateSession(r, rw)
	// A new conversation is simply a new session cookie
	http.SetCookie(rw, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
	})
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]string{"status": "new_conversation", "old_session": sessionID})
}

func (w *Web) handleGetConversationMessages(rw http.ResponseWriter, r *http.Request) {
	if w.store == nil {
		http.Error(rw, `{"error":"store not available"}`, http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "missing conversation id"})
		return
	}
	msgs, err := w.store.GetMessages(r.Context(), id, 200)
	if err != nil {
		w.logger.Error("get messages", "err", err, "conv", id)
		rw.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(msgs)
}

func (w *Web) handleDeleteConversation(rw http.ResponseWriter, r *http.Request) {
	if w.store == nil {
		http.Error(rw, `{"error":"store not available"}`, http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "missing conversation id"})
		return
	}

	// Use the concrete type's DeleteConversation method
	type deleter interface {
		DeleteConversation(ctx context.Context, id string) error
	}
	if d, ok := w.store.(deleter); ok {
		if err := d.DeleteConversation(r.Context(), id); err != nil {
			w.logger.Error("delete conversation", "err", err, "conv", id)
			rw.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
			return
		}
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]string{"status": "deleted"})
}

// --- Stats API ---

func (w *Web) handleStats(rw http.ResponseWriter, r *http.Request) {
	stats := map[string]any{
		"version": w.version,
		"time":    time.Now().Format(time.RFC3339),
	}

	type counter interface {
		MessageCount(ctx context.Context) (int64, error)
		ConversationCount(ctx context.Context) (int64, error)
	}
	if c, ok := w.store.(counter); ok {
		if n, err := c.MessageCount(r.Context()); err == nil {
			stats["messages"] = n
		}
		if n, err := c.ConversationCount(r.Context()); err == nil {
			stats["conversations"] = n
		}
	}

	w.sseClientsMu.RLock()
	stats["active_sessions"] = len(w.sseClients)
	w.sseClientsMu.RUnlock()

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(stats)
}

func (w *Web) handleSystemInfo(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]any{
		"status":  "ok",
		"version": w.version,
		"time":    time.Now().Format(time.RFC3339),
	})
}

func (w *Web) handleStatus(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]any{
		"status":  "ok",
		"version": w.version,
		"time":    time.Now().Format(time.RFC3339),
	})
}

// sendSSEEvent delivers a structured event to the SSE client that owns the given session.
func (w *Web) sendSSEEvent(sessionID string, evt sseEvent) {
	w.sseClientsMu.RLock()
	ch, ok := w.sseClients[sessionID]
	w.sseClientsMu.RUnlock()
	if ok {
		select {
		case ch <- evt:
		default:
			w.logger.Warn("SSE channel full, dropping event", "session", sessionID, "type", evt.Type)
		}
	}
}

// sendSSE delivers a text message to the SSE client (legacy helper).
func (w *Web) sendSSE(sessionID string, content string) {
	w.sendSSEEvent(sessionID, sseEvent{Type: "message", Content: content})
}
