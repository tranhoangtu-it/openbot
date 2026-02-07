package agent

import "testing"

func TestGenerateTitle_Normal(t *testing.T) {
	title := generateTitle("Hello, how are you doing today?")
	if title == "" || title == "New conversation" {
		t.Fatalf("expected meaningful title, got %q", title)
	}
	if title != "Hello, how are you doing today?" {
		t.Fatalf("short message should be used as-is, got %q", title)
	}
}

func TestGenerateTitle_Empty(t *testing.T) {
	title := generateTitle("")
	if title != "New conversation" {
		t.Fatalf("expected 'New conversation', got %q", title)
	}
}

func TestGenerateTitle_Whitespace(t *testing.T) {
	title := generateTitle("   ")
	if title != "New conversation" {
		t.Fatalf("expected 'New conversation' for whitespace, got %q", title)
	}
}

func TestGenerateTitle_LongMessage(t *testing.T) {
	long := "This is a very long message that exceeds the sixty character limit and should be truncated with an ellipsis"
	title := generateTitle(long)
	if len(title) > 70 {
		t.Fatalf("title too long: %d chars: %q", len(title), title)
	}
	if title[len(title)-3:] != "..." {
		t.Fatalf("expected ellipsis at end, got %q", title)
	}
}

func TestGenerateTitle_Multiline(t *testing.T) {
	title := generateTitle("First line\nSecond line\nThird line")
	if title != "First line" {
		t.Fatalf("expected only first line, got %q", title)
	}
}

func TestGenerateTitle_ExactlyAtLimit(t *testing.T) {
	// 60 characters exactly — should not truncate
	msg := "123456789012345678901234567890123456789012345678901234567890"
	title := generateTitle(msg)
	if title != msg {
		t.Fatalf("60-char message should be kept as-is, got %q (len %d)", title, len(title))
	}
}

func TestGenerateTitle_61Chars(t *testing.T) {
	// 61 chars — should truncate
	msg := "This is exactly sixty one characters long with spaces in it.!"
	title := generateTitle(msg)
	if len(title) > 65 { // some buffer for "..."
		t.Fatalf("61-char message should be truncated, got len=%d: %q", len(title), title)
	}
}
