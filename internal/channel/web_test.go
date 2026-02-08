package channel

import (
	"bytes"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"openbot/internal/config"
	"openbot/internal/domain"
	"openbot/internal/tool"
)

func TestHandleSend_WithFileAttachment_PublishesAttachmentContent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Capture published InboundMessage (buffered chan so Publish doesn't block in handler)
	var captured domain.InboundMessage
	var captureMu sync.Mutex
	capCh := make(chan struct{}, 1)
	bus := newCaptureBus(func(msg domain.InboundMessage) {
		captureMu.Lock()
		captured = msg
		captureMu.Unlock()
		select {
		case capCh <- struct{}{}:
		default:
		}
	})

	// FileAttachTool with temp dir (no DB for simplicity)
	attachDir := t.TempDir()
	fileAttach, err := tool.NewFileAttachTool(tool.FileAttachConfig{
		StoragePath:  attachDir,
		MaxSizeBytes: 10 << 20,
		Logger:       logger,
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.Channels.Web.Auth.Enabled = false
	w := NewWeb(WebConfig{
		Host:       "127.0.0.1",
		Port:       0,
		Logger:     logger,
		Config:     cfg,
		FileAttach: fileAttach,
	})
	w.SetBus(bus)

	// Build multipart: message=hi, files= one file "a.txt" with content "file body" (text/plain so IsSupportedType accepts it)
	body := &bytes.Buffer{}
	mp := multipart.NewWriter(body)
	_ = mp.WriteField("message", "hi")
	_ = mp.WriteField("stream", "true")
	part, _ := mp.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="files"; filename="a.txt"`},
		"Content-Type":        {"text/plain"},
	})
	_, _ = part.Write([]byte("file body"))
	_ = mp.Close()

	req := httptest.NewRequest(http.MethodPost, "/chat/send", body)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	rec := httptest.NewRecorder()

	w.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	<-capCh
	captureMu.Lock()
	msg := captured
	captureMu.Unlock()

	if msg.Content != "hi" {
		t.Errorf("Content: got %q", msg.Content)
	}
	if msg.Channel != "web" {
		t.Errorf("Channel: got %q", msg.Channel)
	}
	if !strings.Contains(msg.AttachmentContent, "[File: a.txt]") {
		t.Errorf("AttachmentContent should contain [File: a.txt], got %q", msg.AttachmentContent)
	}
	if !strings.Contains(msg.AttachmentContent, "file body") {
		t.Errorf("AttachmentContent should contain file body, got %q", msg.AttachmentContent)
	}
}

func TestHandleSend_EmptyMessage_Returns400(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	bus := newCaptureBus(nil)
	cfg := &config.Config{}
	cfg.Channels.Web.Auth.Enabled = false
	w := NewWeb(WebConfig{Host: "127.0.0.1", Port: 0, Logger: logger, Config: cfg})
	w.SetBus(bus)

	body := &bytes.Buffer{}
	mp := multipart.NewWriter(body)
	_ = mp.WriteField("message", "")
	_ = mp.WriteField("stream", "true")
	_ = mp.Close()

	req := httptest.NewRequest(http.MethodPost, "/chat/send", body)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	rec := httptest.NewRecorder()
	w.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty message, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("expected JSON content-type, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestHandleSend_TextOnly_Accepted(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	var captured domain.InboundMessage
	capCh := make(chan struct{}, 1)
	bus := newCaptureBus(func(msg domain.InboundMessage) {
		captured = msg
		select { case capCh <- struct{}{}: default: }
	})
	cfg := &config.Config{}
	cfg.Channels.Web.Auth.Enabled = false
	w := NewWeb(WebConfig{Host: "127.0.0.1", Port: 0, Logger: logger, Config: cfg})
	w.SetBus(bus)

	body := &bytes.Buffer{}
	mp := multipart.NewWriter(body)
	_ = mp.WriteField("message", "hello world")
	_ = mp.WriteField("stream", "true")
	_ = mp.Close()

	req := httptest.NewRequest(http.MethodPost, "/chat/send", body)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	rec := httptest.NewRecorder()
	w.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	<-capCh
	if captured.Content != "hello world" {
		t.Errorf("Content: got %q", captured.Content)
	}
	if captured.Channel != "web" {
		t.Errorf("Channel: got %q", captured.Channel)
	}
}

func TestStatus_ReturnsJSON(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := &config.Config{}
	w := NewWeb(WebConfig{Host: "127.0.0.1", Port: 0, Logger: logger, Config: cfg, Version: "0.2.0"})
	w.SetBus(newCaptureBus(nil))

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	w.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" && !strings.HasPrefix(ct, "application/json;") {
		t.Errorf("expected application/json, got %s", ct)
	}
	// Body should contain status and version
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Errorf("body should contain status: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "0.2.0") {
		t.Errorf("body should contain version: %s", rec.Body.String())
	}
}

// captureBus is a minimal MessageBus that calls onPublish for each Publish and satisfies other interface methods.
type captureBus struct {
	onPublish func(domain.InboundMessage)
	inbound    chan domain.InboundMessage
	handlers   map[string]func(domain.OutboundMessage)
}

func newCaptureBus(onPublish func(domain.InboundMessage)) *captureBus {
	return &captureBus{
		onPublish: onPublish,
		inbound:   make(chan domain.InboundMessage, 10),
		handlers:  make(map[string]func(domain.OutboundMessage)),
	}
}

func (c *captureBus) Publish(msg domain.InboundMessage) {
	if c.onPublish != nil {
		c.onPublish(msg)
	}
	select {
	case c.inbound <- msg:
	default:
	}
}

func (c *captureBus) Subscribe() <-chan domain.InboundMessage { return c.inbound }
func (c *captureBus) SendOutbound(msg domain.OutboundMessage) {}
func (c *captureBus) OnOutbound(channelName string, handler func(domain.OutboundMessage)) {
	c.handlers[channelName] = handler
}
func (c *captureBus) Close() {}
