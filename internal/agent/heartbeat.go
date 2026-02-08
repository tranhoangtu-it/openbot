package agent

import (
	"context"
	"log/slog"
	"time"

	"openbot/internal/domain"
)

// HeartbeatConfig configures the proactive heartbeat system.
type HeartbeatConfig struct {
	Enabled         bool
	IntervalMinutes int
	Channel         string // target channel for heartbeat messages
	ChatID          string // target chat ID
	Logger          *slog.Logger
}

// Heartbeat sends periodic proactive messages to configured channels.
type Heartbeat struct {
	enabled  bool
	interval time.Duration
	channel  string
	chatID   string
	bus      domain.MessageBus
	logger   *slog.Logger

	// MessageFunc generates the heartbeat message content.
	// If nil, a default message is used.
	MessageFunc func() string
}

// NewHeartbeat creates a new heartbeat system.
func NewHeartbeat(cfg HeartbeatConfig, bus domain.MessageBus) *Heartbeat {
	interval := time.Duration(cfg.IntervalMinutes) * time.Minute
	if interval < time.Minute {
		interval = 30 * time.Minute // default 30 min
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Heartbeat{
		enabled:  cfg.Enabled,
		interval: interval,
		channel:  cfg.Channel,
		chatID:   cfg.ChatID,
		bus:      bus,
		logger:   cfg.Logger,
	}
}

// Start begins the heartbeat loop. Blocks until context is cancelled.
func (h *Heartbeat) Start(ctx context.Context) {
	if !h.enabled {
		return
	}

	h.logger.Info("heartbeat started",
		"interval", h.interval,
		"channel", h.channel,
	)

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("heartbeat stopped")
			return
		case <-ticker.C:
			h.sendHeartbeat()
		}
	}
}

func (h *Heartbeat) sendHeartbeat() {
	msg := "I'm here and ready to help!"
	if h.MessageFunc != nil {
		msg = h.MessageFunc()
	}

	h.bus.SendOutbound(domain.OutboundMessage{
		Channel: h.channel,
		ChatID:  h.chatID,
		Content: msg,
		Format:  "text",
	})

	h.logger.Debug("heartbeat sent", "channel", h.channel)
}
