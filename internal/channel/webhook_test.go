package channel

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func testWebhookLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestVerifyHMAC_Valid(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"content":"hello"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !verifyHMAC(body, secret, sig) {
		t.Error("valid HMAC should verify")
	}
}

func TestVerifyHMAC_Invalid(t *testing.T) {
	if verifyHMAC([]byte("body"), "secret", "sha256=invalid") {
		t.Error("invalid HMAC should not verify")
	}
}

func TestVerifyHMAC_Empty(t *testing.T) {
	if verifyHMAC([]byte("body"), "secret", "") {
		t.Error("empty signature should not verify")
	}
}

func TestWebhookPayload_Unmarshal(t *testing.T) {
	data := `{"channel":"test","chat_id":"chat1","user_id":"user1","content":"hello"}`
	var payload WebhookPayload
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Content != "hello" {
		t.Errorf("expected hello, got %s", payload.Content)
	}
	if payload.Channel != "test" {
		t.Errorf("expected test, got %s", payload.Channel)
	}
}

func TestSplitMessage_Short(t *testing.T) {
	chunks := splitMessage("short message", 100)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestSplitMessage_Long(t *testing.T) {
	long := ""
	for i := 0; i < 100; i++ {
		long += "word "
	}
	chunks := splitMessage(long, 50)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(chunks))
	}
	// All chunks should be <= maxLen
	for i, c := range chunks {
		if len(c) > 50 {
			t.Errorf("chunk %d too long: %d", i, len(c))
		}
	}
}

func TestSplitMessage_Empty(t *testing.T) {
	chunks := splitMessage("", 100)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk for empty, got %d", len(chunks))
	}
}

func TestWebhookHandler_MethodNotAllowed(t *testing.T) {
	w := &Webhook{logger: testWebhookLogger()}
	req := httptest.NewRequest("GET", "/webhook", nil)
	rr := httptest.NewRecorder()

	w.handleWebhook(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestWebhookHandler_EmptyContent(t *testing.T) {
	w := &Webhook{logger: testWebhookLogger()}
	body := `{"channel":"test","content":""}`
	req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	w.handleWebhook(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestWebhookHandler_InvalidJSON(t *testing.T) {
	w := &Webhook{logger: testWebhookLogger()}
	req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString("not json"))
	rr := httptest.NewRecorder()

	w.handleWebhook(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestWebhookHandler_MissingSignature(t *testing.T) {
	w := &Webhook{secret: "my-secret", logger: testWebhookLogger()}
	body := `{"content":"hello"}`
	req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	w.handleWebhook(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestWebhookHandler_InvalidSignature(t *testing.T) {
	w := &Webhook{secret: "my-secret", logger: testWebhookLogger()}
	body := `{"content":"hello"}`
	req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(body))
	req.Header.Set("X-Signature-256", "sha256=invalid")
	rr := httptest.NewRecorder()

	w.handleWebhook(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}
