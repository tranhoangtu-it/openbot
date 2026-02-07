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
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
	"openbot/internal/config"
	"openbot/internal/domain"
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

	// Config reference for settings API (protected by cfgMu)
	cfg     *config.Config
	cfgPath string
	cfgMu   sync.RWMutex

	// Auth settings
	authEnabled  bool
	authUser     string
	authPassHash string

	// SSE clients keyed by session ID for targeted delivery
	sseClients   map[string]chan string
	sseClientsMu sync.RWMutex

	// Pending responses keyed by session ID
	pendingResponses   map[string]chan string
	pendingResponsesMu sync.Mutex
}

type WebConfig struct {
	Host       string
	Port       int
	Logger     *slog.Logger
	Config     *config.Config
	ConfigPath string
	Version    string
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
		sseClients:       make(map[string]chan string),
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
		w.pendingResponsesMu.Lock()
		ch, ok := w.pendingResponses[msg.ChatID]
		w.pendingResponsesMu.Unlock()
		if ok {
			select {
			case ch <- msg.Content:
			default:
			}
		}
		// Send SSE only to the session that owns this chat
		w.sendSSE(msg.ChatID, msg.Content)
	})

	mux := http.NewServeMux()

	// Static assets (logo, icons etc.) — served from embedded web_assets/
	assetsHandler := http.FileServer(http.FS(assetsFS))
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		r.URL.Path = "web_assets/" + r.URL.Path
		rw.Header().Set("Cache-Control", "public, max-age=86400")
		assetsHandler.ServeHTTP(rw, r)
	})))

	mux.HandleFunc("GET /", w.requireAuth(w.handleDashboard))
	mux.HandleFunc("GET /chat", w.requireAuth(w.handleChat))
	mux.HandleFunc("POST /chat/send", w.requireAuth(w.handleSend))
	mux.HandleFunc("GET /chat/stream", w.requireAuth(w.handleSSE))
	mux.HandleFunc("GET /status", w.handleStatus) // public endpoint
	mux.HandleFunc("POST /chat/clear", w.requireAuth(w.handleClear))

	// Settings page + API (always requires auth)
	mux.HandleFunc("GET /settings", w.requireAuth(w.handleSettings))
	mux.HandleFunc("GET /api/config", w.requireAuth(w.handleGetConfig))
	mux.HandleFunc("PUT /api/config", w.requireAuth(w.handleUpdateConfig))
	mux.HandleFunc("POST /api/config/save", w.requireAuth(w.handleSaveConfig))

	addr := fmt.Sprintf("%s:%d", w.host, w.port)
	w.server = &http.Server{
		Addr:    addr,
		Handler: mux,
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
			Name: sessionCookieName, Value: sessionID, Path: "/",
			MaxAge: sessionMaxAge, HttpOnly: true, SameSite: http.SameSiteLaxMode,
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
	// Support both application/x-www-form-urlencoded and multipart/form-data
	_ = r.ParseMultipartForm(maxFormSize)
	message := r.FormValue("message")
	if message == "" {
		rw.Header().Set("Content-Type", "application/json; charset=utf-8")
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "empty message"})
		return
	}

	// Use persistent session ID as ChatID — this ensures conversation memory
	sessionID := w.getOrCreateSession(r, rw)

	// Create response channel for this session
	responseCh := make(chan string, 1)
	w.pendingResponsesMu.Lock()
	// If a previous request is still pending, cancel it
	if oldCh, exists := w.pendingResponses[sessionID]; exists {
		close(oldCh)
	}
	w.pendingResponses[sessionID] = responseCh
	w.pendingResponsesMu.Unlock()
	defer func() {
		w.pendingResponsesMu.Lock()
		// Only delete if it's still our channel (not replaced by another request)
		if ch, ok := w.pendingResponses[sessionID]; ok && ch == responseCh {
			delete(w.pendingResponses, sessionID)
		}
		w.pendingResponsesMu.Unlock()
	}()

	w.bus.Publish(domain.InboundMessage{
		Channel:   "web",
		ChatID:    sessionID,
		SenderID:  "web_user",
		Content:   message,
		Timestamp: time.Now(),
	})

	// Wait for response — also respect client disconnect via r.Context()
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	timeout := time.NewTimer(requestTimeout)
	defer timeout.Stop()
	select {
	case resp, ok := <-responseCh:
		if ok {
			json.NewEncoder(rw).Encode(map[string]string{"content": resp})
		} else {
			// Channel was closed by a newer request replacing this one
			rw.WriteHeader(http.StatusConflict)
			json.NewEncoder(rw).Encode(map[string]string{"error": "Superseded by new request"})
		}
	case <-timeout.C:
		rw.WriteHeader(http.StatusGatewayTimeout)
		json.NewEncoder(rw).Encode(map[string]string{"error": "Request timed out"})
	case <-r.Context().Done():
		// Client disconnected — no need to write response
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

	// Use session ID to filter SSE messages — only receive your own responses
	sessionID := w.getOrCreateSession(r, rw)

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 10)

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

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			data, _ := json.Marshal(map[string]string{"content": msg})
			fmt.Fprintf(rw, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (w *Web) handleStatus(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]any{
		"status":  "ok",
		"version": w.version,
		"time":    time.Now().Format(time.RFC3339),
	})
}

func (w *Web) handleSettings(rw http.ResponseWriter, r *http.Request) {
	w.tmpl.ExecuteTemplate(rw, "settings.html", map[string]any{
		"Title": "OpenBot Settings",
	})
}

func (w *Web) handleGetConfig(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")

	w.cfgMu.RLock()
	cfg := w.cfg
	w.cfgMu.RUnlock()

	if cfg == nil {
		rw.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(rw).Encode(map[string]string{"error": "config not loaded"})
		return
	}
	sanitized := config.Sanitize(cfg)
	json.NewEncoder(rw).Encode(sanitized)
}

func (w *Web) handleUpdateConfig(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")

	w.cfgMu.Lock()
	defer w.cfgMu.Unlock()

	if w.cfg == nil {
		rw.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(rw).Encode(map[string]string{"error": "config not loaded"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "read body: " + err.Error()})
		return
	}
	defer r.Body.Close()

	// Partial update: { "path": "general.defaultProvider", "value": "ollama" }
	var partial struct {
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal(body, &partial); err == nil && partial.Path != "" {
		if err := config.SetByPath(w.cfg, partial.Path, partial.Value); err != nil {
			rw.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := config.Validate(w.cfg); err != nil {
			rw.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(rw).Encode(map[string]string{"error": "validation: " + err.Error()})
			return
		}
		w.logger.Info("config updated via path", "path", partial.Path, "value", partial.Value)
		json.NewEncoder(rw).Encode(map[string]string{"status": "updated", "path": partial.Path})
		return
	}

	// Full config update — unmarshal into a temporary copy first, then validate
	var candidate config.Config
	if err := json.Unmarshal(body, &candidate); err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "invalid config: " + err.Error()})
		return
	}
	if err := config.Validate(&candidate); err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "validation: " + err.Error()})
		return
	}
	*w.cfg = candidate

	w.logger.Info("config updated (full)")
	json.NewEncoder(rw).Encode(map[string]string{"status": "updated"})
}

func (w *Web) handleSaveConfig(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")

	w.cfgMu.RLock()
	cfg := w.cfg
	cfgPath := w.cfgPath
	w.cfgMu.RUnlock()

	if cfg == nil || cfgPath == "" {
		rw.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(rw).Encode(map[string]string{"error": "config not available"})
		return
	}

	if err := config.Save(cfgPath, cfg); err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(rw).Encode(map[string]string{"error": "save failed: " + err.Error()})
		return
	}

	w.logger.Info("config saved to disk", "path", cfgPath)
	json.NewEncoder(rw).Encode(map[string]string{"status": "saved", "path": cfgPath})
}

// sendSSE delivers a message to the SSE client that owns the given session ID.
func (w *Web) sendSSE(sessionID string, content string) {
	w.sseClientsMu.RLock()
	ch, ok := w.sseClients[sessionID]
	w.sseClientsMu.RUnlock()
	if ok {
		select {
		case ch <- content:
		default:
		}
	}
}
