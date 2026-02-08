package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"openbot/internal/domain"

	"github.com/gorilla/websocket"
)

// WSConfig configures the WebSocket channel.
type WSConfig struct {
	Port   int
	Path   string // WebSocket endpoint path (default: /ws)
	Logger *slog.Logger
}

// WebSocketChannel provides real-time bidirectional communication.
type WebSocketChannel struct {
	port     int
	path     string
	bus      domain.MessageBus
	logger   *slog.Logger
	server   *http.Server

	mu      sync.RWMutex
	clients map[string]*wsClient
}

// wsClient tracks a connected WebSocket client.
type wsClient struct {
	conn   *websocket.Conn
	chatID string
	mu     sync.Mutex
}

// WSMessage is the JSON protocol for WebSocket communication.
type WSMessage struct {
	Type    string `json:"type"`    // "message" | "typing" | "status" | "stream"
	Content string `json:"content,omitempty"`
	ChatID  string `json:"chat_id,omitempty"`
	UserID  string `json:"user_id,omitempty"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins (configure CORS for production)
	},
}

// NewWebSocketChannel creates a new WebSocket channel.
func NewWebSocketChannel(cfg WSConfig) *WebSocketChannel {
	if cfg.Path == "" {
		cfg.Path = "/ws"
	}
	if cfg.Port == 0 {
		cfg.Port = 8081
	}
	return &WebSocketChannel{
		port:    cfg.Port,
		path:    cfg.Path,
		logger:  cfg.Logger,
		clients: make(map[string]*wsClient),
	}
}

func (ws *WebSocketChannel) Name() string { return "websocket" }

// Start begins the WebSocket server.
func (ws *WebSocketChannel) Start(ctx context.Context, bus domain.MessageBus) error {
	ws.bus = bus

	mux := http.NewServeMux()
	mux.HandleFunc(ws.path, ws.handleUpgrade)

	ws.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", ws.port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Register outbound handler.
	bus.OnOutbound("websocket", func(msg domain.OutboundMessage) {
		ws.broadcastToChat(msg.ChatID, WSMessage{
			Type:    "message",
			Content: msg.Content,
			ChatID:  msg.ChatID,
		})
	})

	ws.logger.Info("websocket server starting", "port", ws.port, "path", ws.path)

	errCh := make(chan error, 1)
	go func() {
		if err := ws.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		ws.closeAllClients()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return ws.server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (ws *WebSocketChannel) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		ws.logger.Error("websocket upgrade failed", "err", err)
		return
	}

	chatID := r.URL.Query().Get("chat_id")
	if chatID == "" {
		chatID = fmt.Sprintf("ws-%d", time.Now().UnixNano())
	}

	client := &wsClient{
		conn:   conn,
		chatID: chatID,
	}

	clientID := fmt.Sprintf("%s-%p", chatID, conn)
	ws.mu.Lock()
	ws.clients[clientID] = client
	ws.mu.Unlock()

	ws.logger.Info("websocket client connected", "client_id", clientID, "chat_id", chatID)

	// Send welcome message.
	client.send(WSMessage{Type: "status", Content: "connected", ChatID: chatID})

	// Read loop.
	defer func() {
		ws.mu.Lock()
		delete(ws.clients, clientID)
		ws.mu.Unlock()
		conn.Close()
		ws.logger.Info("websocket client disconnected", "client_id", clientID)
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				ws.logger.Error("websocket read error", "err", err)
			}
			return
		}

		var wsMsg WSMessage
		if err := json.Unmarshal(message, &wsMsg); err != nil {
			ws.logger.Warn("invalid websocket message", "err", err)
			continue
		}

		switch wsMsg.Type {
		case "message":
			ws.bus.Publish(domain.InboundMessage{
				Channel:   "websocket",
				ChatID:    chatID,
				SenderID:  wsMsg.UserID,
				Content:   wsMsg.Content,
				Timestamp: time.Now(),
			})

		case "typing":
			// Could broadcast typing indicators to other clients in same chat.
			ws.logger.Debug("typing indicator", "chat_id", chatID, "user_id", wsMsg.UserID)
		}
	}
}

func (ws *WebSocketChannel) broadcastToChat(chatID string, msg WSMessage) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	for _, client := range ws.clients {
		if client.chatID == chatID || chatID == "" {
			client.mu.Lock()
			err := client.conn.WriteMessage(websocket.TextMessage, data)
			client.mu.Unlock()
			if err != nil {
				ws.logger.Debug("websocket write failed", "err", err)
			}
		}
	}
}

func (c *wsClient) send(msg WSMessage) {
	data, _ := json.Marshal(msg)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.WriteMessage(websocket.TextMessage, data)
}

func (ws *WebSocketChannel) closeAllClients() {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	for id, client := range ws.clients {
		client.conn.Close()
		delete(ws.clients, id)
	}
}
