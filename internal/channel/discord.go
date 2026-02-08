package channel

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"openbot/internal/domain"

	"github.com/bwmarrin/discordgo"
)

const (
	discordMaxMsgLen = 2000
)

// Discord implements domain.Channel for Discord.
type Discord struct {
	token   string
	guildID string
	session *discordgo.Session
	bus     domain.MessageBus
	logger  *slog.Logger
}

// DiscordConfig configures the Discord channel.
type DiscordConfig struct {
	Token   string
	GuildID string
	Logger  *slog.Logger
}

// NewDiscord creates a new Discord channel handler.
func NewDiscord(cfg DiscordConfig) *Discord {
	return &Discord{
		token:   cfg.Token,
		guildID: cfg.GuildID,
		logger:  cfg.Logger,
	}
}

func (d *Discord) Name() string { return "discord" }

// Start connects to Discord using a bot token and begins listening.
func (d *Discord) Start(ctx context.Context, bus domain.MessageBus) error {
	d.bus = bus

	session, err := discordgo.New("Bot " + d.token)
	if err != nil {
		return fmt.Errorf("discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent

	d.session = session

	// Register outbound handler.
	bus.OnOutbound("discord", func(msg domain.OutboundMessage) {
		if msg.StreamEvent != nil && msg.Content == "" {
			return
		}
		if msg.Content == "" {
			return
		}
		d.sendMessage(msg.ChatID, msg.Content)
	})

	// Register message handler.
	session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore bot's own messages.
		if m.Author.ID == s.State.User.ID {
			return
		}

		// If guildID is set, filter messages.
		if d.guildID != "" && m.GuildID != d.guildID {
			return
		}

		d.logger.Info("discord message received",
			"author", m.Author.Username,
			"channel_id", m.ChannelID,
			"content_len", len(m.Content),
		)

		bus.Publish(domain.InboundMessage{
			Channel:   "discord",
			ChatID:    m.ChannelID,
			SenderID:  m.Author.ID,
			Content:   m.Content,
			Timestamp: time.Now(),
		})
	})

	// Register slash commands handler.
	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}
		data := i.ApplicationCommandData()
		content := "/" + data.Name
		for _, opt := range data.Options {
			if opt.Type == discordgo.ApplicationCommandOptionString {
				content += " " + opt.StringValue()
			}
		}

		// Acknowledge interaction.
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		})

		bus.Publish(domain.InboundMessage{
			Channel:   "discord",
			ChatID:    i.ChannelID,
			SenderID:  i.Member.User.ID,
			Content:   content,
			Timestamp: time.Now(),
		})
	})

	if err := session.Open(); err != nil {
		return fmt.Errorf("discord connect: %w", err)
	}

	d.logger.Info("discord bot connected", "user", session.State.User.Username)

	// Register slash commands.
	d.registerSlashCommands()

	// Wait for context cancellation.
	<-ctx.Done()
	d.logger.Info("discord bot disconnecting")
	return session.Close()
}

func (d *Discord) sendMessage(channelID, content string) {
	// Split long messages.
	chunks := splitMessage(content, discordMaxMsgLen)
	for _, chunk := range chunks {
		if _, err := d.session.ChannelMessageSend(channelID, chunk); err != nil {
			d.logger.Error("discord send failed", "channel", channelID, "err", err)
		}
	}
}

func (d *Discord) registerSlashCommands() {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "ask",
			Description: "Ask the AI assistant a question",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "question",
					Description: "Your question",
					Required:    true,
				},
			},
		},
		{
			Name:        "status",
			Description: "Show bot status",
		},
		{
			Name:        "help",
			Description: "Show available commands",
		},
	}

	guildID := d.guildID // empty = global commands
	for _, cmd := range commands {
		_, err := d.session.ApplicationCommandCreate(d.session.State.User.ID, guildID, cmd)
		if err != nil {
			d.logger.Warn("failed to register slash command", "command", cmd.Name, "err", err)
		}
	}
}

// splitMessage splits a message into chunks that fit within the max length,
// trying to split on newlines when possible.
func splitMessage(msg string, maxLen int) []string {
	if len(msg) <= maxLen {
		return []string{msg}
	}

	var chunks []string
	for len(msg) > 0 {
		if len(msg) <= maxLen {
			chunks = append(chunks, msg)
			break
		}

		// Try to split on a newline.
		cut := maxLen
		if idx := strings.LastIndex(msg[:maxLen], "\n"); idx > maxLen/2 {
			cut = idx + 1
		}

		chunks = append(chunks, msg[:cut])
		msg = msg[cut:]
	}
	return chunks
}
