package channel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
	"openbot/internal/domain"
)

// CLI implements domain.Channel for interactive terminal chat.
type CLI struct {
	bus       domain.MessageBus
	logger    *slog.Logger
	in        io.Reader
	out       io.Writer
	thinking  bool
	thinkMu   sync.Mutex
	thinkStop chan struct{}
}

type CLIConfig struct {
	Logger *slog.Logger
	In     io.Reader
	Out    io.Writer
}

func NewCLI(cfg CLIConfig) *CLI {
	if cfg.In == nil {
		cfg.In = os.Stdin
	}
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}
	return &CLI{
		logger: cfg.Logger,
		in:     cfg.In,
		out:    cfg.Out,
	}
}

func (c *CLI) Name() string { return "cli" }

// Start runs the interactive REPL and blocks until context is cancelled.
func (c *CLI) Start(ctx context.Context, bus domain.MessageBus) error {
	c.bus = bus

	bus.OnOutbound("cli", func(msg domain.OutboundMessage) {
		c.stopThinking()
		_, _ = fmt.Fprintln(c.out, "\r\033[K") // Clear spinner line
		_, _ = fmt.Fprintln(c.out, "--- OpenBot ---")
		_, _ = fmt.Fprintln(c.out, msg.Content)
		_, _ = fmt.Fprintln(c.out, "----------------")
		_, _ = fmt.Fprint(c.out, "You> ")
	})

	_, _ = fmt.Fprintln(c.out, "OpenBot CLI. Type your message and press Enter. Type /quit to exit.")
	_, _ = fmt.Fprint(c.out, "You> ")

	scanner := bufio.NewScanner(c.in)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return nil // EOF
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			_, _ = fmt.Fprint(c.out, "You> ")
			continue
		}
		if line == "/quit" || line == "/exit" || line == "/q" {
			c.logger.Info("user requested quit")
			return nil
		}

		c.startThinking()
		c.bus.Publish(domain.InboundMessage{
			Channel:  "cli",
			ChatID:   "direct",
			SenderID: "user",
			Content:  line,
		})
	}
}

func (c *CLI) startThinking() {
	c.thinkMu.Lock()
	defer c.thinkMu.Unlock()
	if c.thinking {
		return
	}
	c.thinking = true
	c.thinkStop = make(chan struct{})
	go func() {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-c.thinkStop:
				return
			case <-ticker.C:
				fmt.Fprintf(c.out, "\r%s Thinking...", frames[i%len(frames)])
				i++
			}
		}
	}()
}

func (c *CLI) stopThinking() {
	c.thinkMu.Lock()
	defer c.thinkMu.Unlock()
	if !c.thinking {
		return
	}
	c.thinking = false
	close(c.thinkStop)
}

// Stop is a no-op for CLI (we exit when Start returns).
func (c *CLI) Stop() error { return nil }

func (c *CLI) Send(ctx context.Context, chatID string, content string) error {
	_, err := fmt.Fprintln(c.out, content)
	return err
}
