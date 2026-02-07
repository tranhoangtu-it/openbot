package channel

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"openbot/internal/domain"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	telegramMaxMsgLen       = 4000
	telegramConfirmTimeout  = 120 * time.Second
	telegramMaxSendRetries  = 3
)

// Telegram implements domain.Channel for Telegram Bot.
type Telegram struct {
	token     string
	allowFrom []int64 // Allowed user IDs (empty = allow all)
	parseMode string

	bot    *tgbotapi.BotAPI
	bus    domain.MessageBus
	logger *slog.Logger

	// pendingConfirm tracks confirmation requests from security engine
	pendingConfirm   map[int64]chan bool
	pendingConfirmMu sync.Mutex
}

type TelegramConfig struct {
	Token     string
	AllowFrom []string // User IDs as strings
	ParseMode string
	Logger    *slog.Logger
}

func NewTelegram(cfg TelegramConfig) *Telegram {
	var allowed []int64
	for _, s := range cfg.AllowFrom {
		if id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			allowed = append(allowed, id)
		}
	}
	if cfg.ParseMode == "" {
		cfg.ParseMode = "Markdown"
	}
	return &Telegram{
		token:          cfg.Token,
		allowFrom:      allowed,
		parseMode:      cfg.ParseMode,
		logger:         cfg.Logger,
		pendingConfirm: make(map[int64]chan bool),
	}
}

func (t *Telegram) Name() string { return "telegram" }

// Start connects to Telegram and begins polling for updates.
func (t *Telegram) Start(ctx context.Context, bus domain.MessageBus) error {
	t.bus = bus

	bot, err := tgbotapi.NewBotAPI(t.token)
	if err != nil {
		return fmt.Errorf("telegram bot init: %w", err)
	}
	t.bot = bot
	t.logger.Info("telegram bot connected",
		"username", bot.Self.UserName,
		"id", bot.Self.ID,
	)

	bus.OnOutbound("telegram", func(msg domain.OutboundMessage) {
		// Skip stream-only events — Telegram doesn't support incremental streaming,
		// so only deliver the final message that carries Content.
		if msg.StreamEvent != nil && msg.Content == "" {
			return
		}
		chatID, err := strconv.ParseInt(msg.ChatID, 10, 64)
		if err != nil {
			t.logger.Error("invalid chat ID for telegram outbound", "chatID", msg.ChatID, "err", err)
			return
		}
		t.sendMessage(chatID, msg.Content)
	})

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := bot.GetUpdatesChan(u)

	t.logger.Info("telegram polling started")

	for {
		select {
		case <-ctx.Done():
			t.logger.Info("telegram channel stopping")
			bot.StopReceivingUpdates()
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			t.handleUpdate(ctx, update)
		}
	}
}

// Stop shuts down the Telegram bot.
// Note: StopReceivingUpdates is already called when ctx is cancelled in Start().
// Calling it twice panics, so Stop() is a no-op.
func (t *Telegram) Stop() error {
	// No-op: the bot stops when Start's context is cancelled.
	return nil
}

func (t *Telegram) Send(ctx context.Context, chatID string, content string) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}
	t.sendMessage(id, content)
	return nil
}

// RequestConfirmation sends a confirmation prompt via Telegram and waits for user response.
// Used by the security engine for dangerous commands.
func (t *Telegram) RequestConfirmation(ctx context.Context, chatID int64, question string) (bool, error) {
	t.pendingConfirmMu.Lock()
	ch := make(chan bool, 1)
	t.pendingConfirm[chatID] = ch
	t.pendingConfirmMu.Unlock()

	defer func() {
		t.pendingConfirmMu.Lock()
		delete(t.pendingConfirm, chatID)
		t.pendingConfirmMu.Unlock()
	}()

	msg := tgbotapi.NewMessage(chatID, question)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Allow", "confirm_yes"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Deny", "confirm_no"),
		),
	)
	if _, err := t.bot.Send(msg); err != nil {
		return false, fmt.Errorf("send confirmation: %w", err)
	}

	select {
	case confirmed := <-ch:
		return confirmed, nil
	case <-time.After(telegramConfirmTimeout):
		t.sendMessage(chatID, "⏰ Confirmation timed out. Action denied.")
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (t *Telegram) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		t.handleCallback(update.CallbackQuery)
		return
	}

	if update.Message == nil || update.Message.From == nil || update.Message.Chat == nil {
		return
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	if !t.isAllowed(userID) {
		t.logger.Warn("unauthorized telegram user",
			"user_id", userID,
			"username", update.Message.From.UserName,
		)
		t.sendMessage(chatID, "⛔ Unauthorized. Your user ID is not in the allow list.")
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	if text == "" {
		return
	}

	if update.Message.IsCommand() {
		t.handleCommand(chatID, update.Message)
		return
	}

	t.logger.Info("telegram message received",
		"user_id", userID,
		"chat_id", chatID,
		"text_len", len(text),
	)

	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	_, _ = t.bot.Send(typing)

	t.bus.Publish(domain.InboundMessage{
		Channel:   "telegram",
		ChatID:    strconv.FormatInt(chatID, 10),
		SenderID:  strconv.FormatInt(userID, 10),
		Content:   text,
		Timestamp: time.Unix(int64(update.Message.Date), 0),
	})
}

func (t *Telegram) handleCallback(cq *tgbotapi.CallbackQuery) {
	if cq.Message == nil || cq.Message.Chat == nil {
		return
	}
	chatID := cq.Message.Chat.ID
	data := cq.Data

	callback := tgbotapi.NewCallback(cq.ID, "")
	_, _ = t.bot.Request(callback)

	t.pendingConfirmMu.Lock()
	ch, ok := t.pendingConfirm[chatID]
	t.pendingConfirmMu.Unlock()

	if ok {
		switch data {
		case "confirm_yes":
			ch <- true
			t.sendMessage(chatID, "✅ Action confirmed.")
		case "confirm_no":
			ch <- false
			t.sendMessage(chatID, "❌ Action denied.")
		}

		edit := tgbotapi.NewEditMessageReplyMarkup(chatID, cq.Message.MessageID, tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}})
		_, _ = t.bot.Send(edit)
	}
}

func (t *Telegram) handleCommand(chatID int64, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start":
		t.sendMessage(chatID, "👋 Hello! I'm OpenBot, your AI assistant.\n\nJust send me a message and I'll help you.\n\nCommands:\n/status — Show bot status\n/clear — Clear conversation\n/help — Show this message")
	case "help":
		t.sendMessage(chatID, "📖 *OpenBot Help*\n\nSend me any message and I'll respond using AI.\n\nI can:\n• Answer questions\n• Run shell commands\n• Read/write files\n• Search the web\n• Control your computer\n\nCommands:\n/status — Bot status\n/clear — Clear conversation\n/provider — Current provider info")
	case "status":
		t.sendMessage(chatID, fmt.Sprintf("🟢 OpenBot v0.2.0\n\nBot: @%s\nYour ID: %d\nChat ID: %d", t.bot.Self.UserName, msg.From.ID, chatID))
	case "clear":
		t.bus.Publish(domain.InboundMessage{
			Channel:  "telegram",
			ChatID:   strconv.FormatInt(chatID, 10),
			SenderID: strconv.FormatInt(msg.From.ID, 10),
			Content:  "/clear",
		})
		t.sendMessage(chatID, "🗑 Conversation cleared.")
	default:
		t.sendMessage(chatID, "Unknown command. Type /help for available commands.")
	}
}

func (t *Telegram) isAllowed(userID int64) bool {
	if len(t.allowFrom) == 0 {
		return true // Empty list = allow all
	}
	for _, id := range t.allowFrom {
		if id == userID {
			return true
		}
	}
	return false
}

func (t *Telegram) sendMessage(chatID int64, text string) {
	// Telegram has a 4096 char limit per message
	const maxLen = telegramMaxMsgLen
	for len(text) > 0 {
		chunk := text
		if len(chunk) > maxLen {
			cutAt := strings.LastIndex(chunk[:maxLen], "\n")
			if cutAt < maxLen/2 {
				cutAt = maxLen
			}
			chunk = text[:cutAt]
			text = text[cutAt:]
		} else {
			text = ""
		}

		t.sendChunk(chatID, chunk)
	}
}

// sendChunk sends a single message chunk with retry and rate limit handling.
// Strategy: try Markdown first → on parse error fallback to plain text → retry with backoff.
func (t *Telegram) sendChunk(chatID int64, text string) {
	const maxRetries = telegramMaxSendRetries

	for attempt := 0; attempt <= maxRetries; attempt++ {
		msg := tgbotapi.NewMessage(chatID, text)
		if attempt == 0 && t.parseMode != "" {
			msg.ParseMode = t.parseMode
		}
		// On subsequent attempts: send as plain text (parse mode may be malformed).

		_, err := t.bot.Send(msg)
		if err == nil {
			return
		}

		errStr := err.Error()

		// Handle Telegram rate limiting (HTTP 429).
		if strings.Contains(errStr, "Too Many Requests") || strings.Contains(errStr, "429") {
			retryAfter := time.Duration(attempt+1) * 3 * time.Second
			t.logger.Warn("telegram rate limited, backing off",
				"retry_after", retryAfter, "attempt", attempt+1,
			)
			time.Sleep(retryAfter)
			continue
		}

		// Markdown parse error on first attempt — immediately retry as plain text.
		if attempt == 0 && msg.ParseMode != "" &&
			strings.Contains(errStr, "can't parse entities") {
			t.logger.Warn("telegram markdown parse error, retrying as plain text",
				"err", err, "parseMode", t.parseMode,
			)
			plainMsg := tgbotapi.NewMessage(chatID, text)
			if _, err2 := t.bot.Send(plainMsg); err2 == nil {
				return
			}
			// Plain also failed — fall through to backoff loop.
		}

		// Exponential backoff for other transient errors.
		if attempt < maxRetries {
			backoff := time.Duration(attempt+1) * time.Second
			t.logger.Warn("telegram send error, retrying", "err", err, "backoff", backoff)
			time.Sleep(backoff)
			continue
		}

		t.logger.Error("telegram send failed after retries", "err", err, "attempts", maxRetries+1)
	}
}
