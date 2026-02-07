package bus

import (
	"log/slog"
	"sync"
	"time"
	"openbot/internal/domain"
)

const publishTimeout = 10 * time.Second

// InMemoryBus is a Go-channel based message bus for in-process communication.
type InMemoryBus struct {
	inbound  chan domain.InboundMessage
	handlers map[string]func(domain.OutboundMessage)
	mu       sync.RWMutex
	closed   bool
	logger   *slog.Logger
}

// New creates a new InMemoryBus with the given buffer size.
func New(bufferSize int, logger *slog.Logger) *InMemoryBus {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &InMemoryBus{
		inbound:  make(chan domain.InboundMessage, bufferSize),
		handlers: make(map[string]func(domain.OutboundMessage)),
		logger:   logger,
	}
}

// Blocks up to 10 seconds if the bus is full instead of dropping.
func (b *InMemoryBus) Publish(msg domain.InboundMessage) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		b.logger.Warn("attempted to publish to closed bus")
		return
	}

	select {
	case b.inbound <- msg:
	default:
		// Bus full — wait with timeout instead of dropping
		b.logger.Warn("inbound bus full, waiting...", "channel", msg.Channel, "sender", msg.SenderID)
		timer := time.NewTimer(publishTimeout)
		defer timer.Stop()
		select {
		case b.inbound <- msg:
			b.logger.Info("message delivered after wait", "channel", msg.Channel)
		case <-timer.C:
			b.logger.Error("message dropped: bus full for 10s",
				"channel", msg.Channel,
				"sender", msg.SenderID,
			)
		}
	}
}

func (b *InMemoryBus) Subscribe() <-chan domain.InboundMessage {
	return b.inbound
}

func (b *InMemoryBus) SendOutbound(msg domain.OutboundMessage) {
	b.mu.RLock()
	handler, ok := b.handlers[msg.Channel]
	b.mu.RUnlock()

	if !ok {
		b.logger.Warn("no handler registered for channel",
			"channel", msg.Channel,
		)
		return
	}

	handler(msg)
}

func (b *InMemoryBus) OnOutbound(channelName string, handler func(domain.OutboundMessage)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[channelName] = handler
}

func (b *InMemoryBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.closed {
		b.closed = true
		close(b.inbound)
	}
}
