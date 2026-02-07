package bus

import (
	"log/slog"
	"sync"
)

// Event represents a system event for internal pub/sub.
type Event struct {
	Type    string         // e.g. "message.received", "tool.before_execute", "security.blocked"
	Payload map[string]any // event-specific data
}

// EventHandler is a callback for events.
type EventHandler func(Event)

// EventBus provides a simple publish/subscribe event system for internal events.
// It is separate from the MessageBus (which handles inbound/outbound messages).
type EventBus struct {
	handlers map[string][]EventHandler
	mu       sync.RWMutex
	logger   *slog.Logger
}

// NewEventBus creates a new EventBus.
func NewEventBus(logger *slog.Logger) *EventBus {
	return &EventBus{
		handlers: make(map[string][]EventHandler),
		logger:   logger,
	}
}

// On registers a handler for the given event type.
// Use "*" to listen to all events.
func (eb *EventBus) On(eventType string, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.handlers[eventType] = append(eb.handlers[eventType], handler)
}

// Emit publishes an event to all registered handlers.
// Handlers are called synchronously in order.
func (eb *EventBus) Emit(event Event) {
	eb.mu.RLock()
	handlers := make([]EventHandler, 0)

	// Specific handlers
	if h, ok := eb.handlers[event.Type]; ok {
		handlers = append(handlers, h...)
	}
	// Wildcard handlers
	if h, ok := eb.handlers["*"]; ok {
		handlers = append(handlers, h...)
	}
	eb.mu.RUnlock()

	for _, h := range handlers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					eb.logger.Error("event handler panic", "event", event.Type, "panic", r)
				}
			}()
			h(event)
		}()
	}
}

// EmitAsync publishes an event to all registered handlers asynchronously.
func (eb *EventBus) EmitAsync(event Event) {
	go eb.Emit(event)
}

// --- Well-known event types ---
const (
	EventMessageReceived     = "message.received"
	EventMessageSent         = "message.sent"
	EventToolBeforeExecute   = "tool.before_execute"
	EventToolAfterExecute    = "tool.after_execute"
	EventSecurityBlocked     = "security.blocked"
	EventSecurityConfirmed   = "security.confirmed"
	EventProviderError       = "provider.error"
	EventConversationCreated = "conversation.created"
)
