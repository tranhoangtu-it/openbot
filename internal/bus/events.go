package bus

import (
	"log/slog"
	"sync"
	"time"
)

// Event represents a system event for internal pub/sub.
type Event struct {
	Type      string         // e.g. "message.received", "tool.before_execute", "security.blocked"
	Source    string         // originating component
	Payload   map[string]any // event-specific data
	Timestamp time.Time      // when the event was created
}

// EventHandler is a callback for events.
type EventHandler func(Event)

// EventBus provides a topic-based publish/subscribe event system for internal events.
// It supports wildcard subscriptions, event history replay, and async dispatch.
type EventBus struct {
	handlers   map[string][]namedHandler
	mu         sync.RWMutex
	logger     *slog.Logger
	history    []Event
	maxHistory int
}

// namedHandler pairs a handler with an ID for unsubscription.
type namedHandler struct {
	ID      string
	Handler EventHandler
}

// NewEventBus creates a new EventBus with optional history replay buffer.
func NewEventBus(logger *slog.Logger) *EventBus {
	return &EventBus{
		handlers:   make(map[string][]namedHandler),
		logger:     logger,
		maxHistory: 1000,
	}
}

// On registers a handler for the given event type.
// Use "*" to listen to all events. Returns the handler ID for unsubscription.
func (eb *EventBus) On(eventType string, handler EventHandler) string {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	id := eventType + "-" + itoa(len(eb.handlers[eventType]))
	eb.handlers[eventType] = append(eb.handlers[eventType], namedHandler{ID: id, Handler: handler})
	return id
}

// Off removes a handler by its ID.
func (eb *EventBus) Off(eventType, handlerID string) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	handlers := eb.handlers[eventType]
	for i, h := range handlers {
		if h.ID == handlerID {
			eb.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
			return
		}
	}
}

// Emit publishes an event to all registered handlers.
// Handlers are called synchronously in order.
func (eb *EventBus) Emit(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Append to history.
	eb.mu.Lock()
	if len(eb.history) >= eb.maxHistory {
		eb.history = eb.history[1:]
	}
	eb.history = append(eb.history, event)
	eb.mu.Unlock()

	eb.mu.RLock()
	handlers := make([]namedHandler, 0)

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
		func(nh namedHandler) {
			defer func() {
				if r := recover(); r != nil {
					eb.logger.Error("event handler panic", "event", event.Type, "handler", nh.ID, "panic", r)
				}
			}()
			nh.Handler(event)
		}(h)
	}
}

// EmitAsync publishes an event to all registered handlers asynchronously.
func (eb *EventBus) EmitAsync(event Event) {
	go eb.Emit(event)
}

// Replay returns historical events matching the given type since the given time.
// Use "*" for all event types.
func (eb *EventBus) Replay(eventType string, since time.Time) []Event {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	var result []Event
	for _, e := range eb.history {
		if e.Timestamp.Before(since) {
			continue
		}
		if eventType == "*" || e.Type == eventType {
			result = append(result, e)
		}
	}
	return result
}

// HistoryLen returns the current number of events in the history buffer.
func (eb *EventBus) HistoryLen() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.history)
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
	EventAgentDelegated      = "agent.delegated"
	EventWebhookReceived     = "webhook.received"
	EventMetricRecorded      = "metric.recorded"
)

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
