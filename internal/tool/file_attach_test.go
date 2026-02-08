package tool

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewFileAttachTool_DefaultStorage(t *testing.T) {
	dir := t.TempDir()
	tool, err := NewFileAttachTool(FileAttachConfig{
		StoragePath: filepath.Join(dir, "attachments"),
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err != nil {
		t.Fatal(err)
	}
	if tool.storagePath == "" {
		t.Error("storage path should be set")
	}
}

func TestNewFileAttachTool_DefaultMaxSize(t *testing.T) {
	tool, err := NewFileAttachTool(FileAttachConfig{
		StoragePath: t.TempDir(),
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err != nil {
		t.Fatal(err)
	}
	if tool.maxSizeBytes != 50*1024*1024 {
		t.Errorf("expected default 50MB, got %d", tool.maxSizeBytes)
	}
}

func TestFileAttachTool_Store(t *testing.T) {
	dir := t.TempDir()
	tool, err := NewFileAttachTool(FileAttachConfig{
		StoragePath: dir,
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err != nil {
		t.Fatal(err)
	}

	content := "Hello, World!"
	reader := strings.NewReader(content)

	info, err := tool.Store(context.Background(), "conv-1", "test.txt", "text/plain", reader)
	if err != nil {
		t.Fatal(err)
	}

	if info.Filename != "test.txt" {
		t.Errorf("expected filename test.txt, got %s", info.Filename)
	}
	if info.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), info.Size)
	}
	if info.MimeType != "text/plain" {
		t.Errorf("expected mime type text/plain, got %s", info.MimeType)
	}
	// Verify file exists on disk
	if _, err := os.Stat(info.StoragePath); err != nil {
		t.Errorf("stored file not found: %v", err)
	}
}

func TestFileAttachTool_Store_TooLarge(t *testing.T) {
	dir := t.TempDir()
	tool, err := NewFileAttachTool(FileAttachConfig{
		StoragePath:  dir,
		MaxSizeBytes: 10, // 10 bytes max
		Logger:       slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err != nil {
		t.Fatal(err)
	}

	content := "This is definitely more than 10 bytes of content"
	reader := strings.NewReader(content)

	_, err = tool.Store(context.Background(), "conv-1", "large.txt", "text/plain", reader)
	if err == nil {
		t.Error("expected error for oversized file")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("expected 'too large' error, got: %v", err)
	}
}

func TestFileAttachTool_ReadText_TextFile(t *testing.T) {
	dir := t.TempDir()
	tool, err := NewFileAttachTool(FileAttachConfig{
		StoragePath: dir,
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err != nil {
		t.Fatal(err)
	}

	content := "Hello, this is test content"
	info, err := tool.Store(context.Background(), "conv-1", "test.txt", "text/plain", strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}

	text, err := tool.ReadText(info)
	if err != nil {
		t.Fatal(err)
	}
	if text != content {
		t.Errorf("expected %q, got %q", content, text)
	}
}

func TestFileAttachTool_ReadText_BinaryFile(t *testing.T) {
	dir := t.TempDir()
	tool, err := NewFileAttachTool(FileAttachConfig{
		StoragePath: dir,
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err != nil {
		t.Fatal(err)
	}

	info, err := tool.Store(context.Background(), "conv-1", "image.png", "image/png", bytes.NewReader([]byte{0x89, 0x50, 0x4E, 0x47}))
	if err != nil {
		t.Fatal(err)
	}

	text, err := tool.ReadText(info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Binary file") {
		t.Errorf("expected binary file message, got %q", text)
	}
}

func TestIsSupportedType(t *testing.T) {
	tests := []struct {
		mimeType string
		want     bool
	}{
		{"text/plain", true},
		{"text/html", true},
		{"application/json", true},
		{"application/xml", true},
		{"image/jpeg", true},
		{"image/png", true},
		{"image/gif", true},
		{"image/webp", true},
		{"application/pdf", true},
		{"application/octet-stream", false},
		{"video/mp4", false},
		{"audio/mpeg", false},
	}

	for _, tt := range tests {
		got := IsSupportedType(tt.mimeType)
		if got != tt.want {
			t.Errorf("IsSupportedType(%q) = %v, want %v", tt.mimeType, got, tt.want)
		}
	}
}

func TestGenerateAttachmentID(t *testing.T) {
	id1 := generateAttachmentID("conv-1", "file.txt")
	id2 := generateAttachmentID("conv-1", "file.txt")
	// Should generate different IDs (time-based component)
	if id1 == id2 {
		t.Log("IDs may be same if generated too quickly; this is expected")
	}
	if len(id1) != 16 {
		t.Errorf("expected 16 char ID, got %d", len(id1))
	}
}
