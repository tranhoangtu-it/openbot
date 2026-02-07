package browser

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/chromedp"
)

// Bridge manages headless Chrome instances for web automation.
type Bridge struct {
	profileDir string
	headless   bool
	logger     *slog.Logger
}

// BridgeConfig holds configuration for the browser bridge.
type BridgeConfig struct {
	ProfileDir string // Chrome user data directory (persists cookies/sessions)
	Headless   bool   // Run headless (true) or with visible UI (false)
	Logger     *slog.Logger
}

func NewBridge(cfg BridgeConfig) *Bridge {
	if cfg.ProfileDir == "" {
		home, _ := os.UserHomeDir()
		cfg.ProfileDir = filepath.Join(home, ".openbot", "chrome-profiles", "default")
	}
	return &Bridge{
		profileDir: cfg.ProfileDir,
		headless:   cfg.Headless,
		logger:     cfg.Logger,
	}
}

// NewContext creates a new chromedp context with the bridge's Chrome profile.
// The caller MUST call cancel() when done.
func (b *Bridge) NewContext(parentCtx context.Context) (context.Context, context.CancelFunc) {
	if err := os.MkdirAll(b.profileDir, 0o755); err != nil {
		b.logger.Error("failed to create profile dir", "dir", b.profileDir, "err", err)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(b.profileDir),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("exclude-switches", "enable-automation"),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"),
	)

	if b.headless {
		opts = append(opts, chromedp.Headless)
	} else {
		opts = append(opts, chromedp.Flag("headless", false))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(parentCtx, opts...)
	taskCtx, taskCancel := chromedp.NewContext(allocCtx)

	cancelAll := func() {
		taskCancel()
		allocCancel()
	}

	return taskCtx, cancelAll
}

// Login opens a visible browser for the user to log in manually.
// After login, cookies are saved in the profile directory.
func (b *Bridge) Login(ctx context.Context, url string) error {
	b.logger.Info("opening browser for login", "url", url)

	// Force visible browser for login
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(b.profileDir),
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("exclude-switches", "enable-automation"),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	if err := chromedp.Run(taskCtx, chromedp.Navigate(url)); err != nil {
		return fmt.Errorf("navigate to login page: %w", err)
	}

	b.logger.Info("browser opened. Please log in manually. Press Ctrl+C when done.")

	<-ctx.Done()

	b.logger.Info("login session saved", "profile", b.profileDir)
	return nil
}

// SendAndReceive navigates to a chat page, sends a message, and waits for the response.
func (b *Bridge) SendAndReceive(ctx context.Context, sel SelectorSet, message string) (string, error) {
	taskCtx, cancel := b.NewContext(ctx)
	defer cancel()

	taskCtx, taskCancel := context.WithTimeout(taskCtx, 120*time.Second)
	defer taskCancel()

	var response string

	err := chromedp.Run(taskCtx,
		chromedp.Navigate(sel.URL),
		chromedp.WaitReady("body"),
		chromedp.WaitVisible(sel.Input, chromedp.ByQuery),
		chromedp.Sleep(1*time.Second),
		chromedp.Click(sel.Input, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
	)
	if err != nil {
		return "", fmt.Errorf("prepare chat page: %w", err)
	}

	// Type the message character by character (more human-like)
	err = chromedp.Run(taskCtx,
		chromedp.SendKeys(sel.Input, message, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
	)
	if err != nil {
		return "", fmt.Errorf("type message: %w", err)
	}

	err = chromedp.Run(taskCtx,
		chromedp.Click(sel.Submit, chromedp.ByQuery),
	)
	if err != nil {
		return "", fmt.Errorf("click send: %w", err)
	}

	b.logger.Debug("waiting for response...")

	for i := 0; i < 120; i++ { // max 120 seconds
		time.Sleep(1 * time.Second)

		var loadingExists bool
		err = chromedp.Run(taskCtx,
			chromedp.Evaluate(
				fmt.Sprintf(`document.querySelector('%s') !== null`, sel.Loading),
				&loadingExists,
			),
		)
		if err != nil {
			break
		}
		if !loadingExists {
			// Loading complete, wait a bit for final render
			time.Sleep(500 * time.Millisecond)
			break
		}
	}

	err = chromedp.Run(taskCtx,
		chromedp.Evaluate(
			fmt.Sprintf(`
				(function() {
					var elements = document.querySelectorAll('%s');
					if (elements.length === 0) return '';
					var last = elements[elements.length - 1];
					return last.innerText || last.textContent || '';
				})()
			`, sel.Response),
			&response,
		),
	)
	if err != nil {
		return "", fmt.Errorf("extract response: %w", err)
	}

	return response, nil
}

// SelectorSet contains CSS selectors for a specific chat website.
type SelectorSet struct {
	URL      string // Chat page URL (e.g., "https://chatgpt.com")
	Input    string // CSS selector for the text input area
	Submit   string // CSS selector for the send/submit button
	Response string // CSS selector for response text blocks
	Loading  string // CSS selector for loading/typing indicator
}

// ChatGPTSelectors returns the default selectors for ChatGPT.
func ChatGPTSelectors() SelectorSet {
	return SelectorSet{
		URL:      "https://chatgpt.com",
		Input:    "#prompt-textarea",
		Submit:   "[data-testid='send-button']",
		Response: ".markdown.prose",
		Loading:  ".result-streaming",
	}
}

// GeminiSelectors returns the default selectors for Google Gemini.
func GeminiSelectors() SelectorSet {
	return SelectorSet{
		URL:      "https://gemini.google.com",
		Input:    ".ql-editor",
		Submit:   ".send-button",
		Response: ".response-content",
		Loading:  ".loading-indicator",
	}
}
