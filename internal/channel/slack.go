package channel

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"openbot/internal/domain"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const slackMaxMsgLen = 4000

// Slack implements domain.Channel for Slack using Socket Mode.
type Slack struct {
	botToken string
	appToken string
	client   *slack.Client
	socket   *socketmode.Client
	bus      domain.MessageBus
	logger   *slog.Logger
	botUID   string // the bot's own user ID, to avoid replying to self
}

// SlackConfig configures the Slack channel.
type SlackConfig struct {
	BotToken string
	AppToken string
	Logger   *slog.Logger
}

// NewSlack creates a new Slack channel handler.
func NewSlack(cfg SlackConfig) *Slack {
	return &Slack{
		botToken: cfg.BotToken,
		appToken: cfg.AppToken,
		logger:   cfg.Logger,
	}
}

func (s *Slack) Name() string { return "slack" }

// Start connects to Slack via Socket Mode and begins listening for events.
func (s *Slack) Start(ctx context.Context, bus domain.MessageBus) error {
	s.bus = bus

	api := slack.New(
		s.botToken,
		slack.OptionAppLevelToken(s.appToken),
	)
	s.client = api

	// Get bot user ID.
	authResp, err := api.AuthTest()
	if err != nil {
		return fmt.Errorf("slack auth: %w", err)
	}
	s.botUID = authResp.UserID
	s.logger.Info("slack bot connected", "user", authResp.User, "user_id", authResp.UserID)

	socketClient := socketmode.New(api)
	s.socket = socketClient

	// Register outbound handler.
	bus.OnOutbound("slack", func(msg domain.OutboundMessage) {
		if msg.StreamEvent != nil && msg.Content == "" {
			return
		}
		if msg.Content == "" {
			return
		}
		s.sendMessage(msg.ChatID, msg.Content)
	})

	// Event handling goroutine.
	go func() {
		for evt := range socketClient.Events {
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				socketClient.Ack(*evt.Request)
				s.handleEventsAPI(eventsAPIEvent)

			case socketmode.EventTypeSlashCommand:
				cmd, ok := evt.Data.(slack.SlashCommand)
				if !ok {
					continue
				}
				socketClient.Ack(*evt.Request)
				s.handleSlashCommand(cmd)

			case socketmode.EventTypeInteractive:
				socketClient.Ack(*evt.Request)
				// Interactive components can be handled here in the future.

			default:
				// Acknowledge unknown events to prevent Socket Mode disconnection.
				if evt.Request != nil {
					socketClient.Ack(*evt.Request)
				}
			}
		}
	}()

	// Run Socket Mode client (blocks until context is done).
	errCh := make(chan error, 1)
	go func() {
		errCh <- socketClient.RunContext(ctx)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("slack bot disconnecting")
		return nil
	case err := <-errCh:
		return fmt.Errorf("slack socket mode: %w", err)
	}
}

func (s *Slack) handleEventsAPI(event slackevents.EventsAPIEvent) {
	switch event.Type {
	case slackevents.CallbackEvent:
		innerEvent := event.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			// Ignore bot's own messages and message_changed subtypes.
			if ev.User == s.botUID || ev.User == "" {
				return
			}
			if ev.SubType != "" {
				return
			}

			s.logger.Info("slack message received",
				"user", ev.User,
				"channel", ev.Channel,
				"content_len", len(ev.Text),
			)

			s.bus.Publish(domain.InboundMessage{
				Channel:   "slack",
				ChatID:    ev.Channel,
				SenderID:  ev.User,
				Content:   ev.Text,
				Timestamp: time.Now(),
			})

		case *slackevents.AppMentionEvent:
			// Handle @mentions of the bot.
			s.logger.Info("slack mention received",
				"user", ev.User,
				"channel", ev.Channel,
			)

			// Strip the mention prefix.
			content := ev.Text
			if idx := strings.Index(content, ">"); idx >= 0 {
				content = strings.TrimSpace(content[idx+1:])
			}

			s.bus.Publish(domain.InboundMessage{
				Channel:   "slack",
				ChatID:    ev.Channel,
				SenderID:  ev.User,
				Content:   content,
				Timestamp: time.Now(),
			})
		}
	}
}

func (s *Slack) handleSlashCommand(cmd slack.SlashCommand) {
	content := cmd.Command + " " + cmd.Text
	content = strings.TrimSpace(content)

	s.logger.Info("slack slash command",
		"command", cmd.Command,
		"user", cmd.UserID,
		"channel", cmd.ChannelID,
	)

	s.bus.Publish(domain.InboundMessage{
		Channel:   "slack",
		ChatID:    cmd.ChannelID,
		SenderID:  cmd.UserID,
		Content:   content,
		Timestamp: time.Now(),
	})
}

func (s *Slack) sendMessage(channelID, content string) {
	// Split long messages.
	chunks := splitSlackMessage(content, slackMaxMsgLen)
	for _, chunk := range chunks {
		_, _, err := s.client.PostMessage(
			channelID,
			slack.MsgOptionText(chunk, false),
			slack.MsgOptionAsUser(true),
		)
		if err != nil {
			s.logger.Error("slack send failed", "channel", channelID, "err", err)
		}
	}
}

func splitSlackMessage(msg string, maxLen int) []string {
	if len(msg) <= maxLen {
		return []string{msg}
	}

	var chunks []string
	for len(msg) > 0 {
		if len(msg) <= maxLen {
			chunks = append(chunks, msg)
			break
		}
		cut := maxLen
		if idx := strings.LastIndex(msg[:maxLen], "\n"); idx > maxLen/2 {
			cut = idx + 1
		}
		chunks = append(chunks, msg[:cut])
		msg = msg[cut:]
	}
	return chunks
}
