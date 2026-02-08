package tool

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileAttachConfig configures the file attachment tool.
type FileAttachConfig struct {
	StoragePath string // base directory for stored attachments
	MaxSizeBytes int64 // max file size in bytes (default: 50MB)
	DB          *sql.DB
	Logger      *slog.Logger
}

// FileAttachTool handles file uploads, storage, and retrieval.
type FileAttachTool struct {
	storagePath  string
	maxSizeBytes int64
	db           *sql.DB
	logger       *slog.Logger
}

// NewFileAttachTool creates a new file attachment handler.
func NewFileAttachTool(cfg FileAttachConfig) (*FileAttachTool, error) {
	storage := cfg.StoragePath
	if storage == "" {
		home, _ := os.UserHomeDir()
		storage = filepath.Join(home, ".openbot", "attachments")
	}
	if err := os.MkdirAll(storage, 0o755); err != nil {
		return nil, fmt.Errorf("create attachment storage: %w", err)
	}

	maxSize := cfg.MaxSizeBytes
	if maxSize <= 0 {
		maxSize = 50 * 1024 * 1024 // 50MB default
	}

	return &FileAttachTool{
		storagePath:  storage,
		maxSizeBytes: maxSize,
		db:           cfg.DB,
		logger:       cfg.Logger,
	}, nil
}

// AttachmentInfo describes a stored attachment.
type AttachmentInfo struct {
	ID             string
	ConversationID string
	Filename       string
	MimeType       string
	Size           int64
	StoragePath    string
	CreatedAt      time.Time
}

// Store saves a file from an io.Reader and records it in the database.
func (f *FileAttachTool) Store(ctx context.Context, convID, filename, mimeType string, reader io.Reader) (*AttachmentInfo, error) {
	// Generate unique ID
	id := generateAttachmentID(convID, filename)

	// Create storage path
	ext := filepath.Ext(filename)
	storageName := id + ext
	storagePath := filepath.Join(f.storagePath, storageName)

	// Write to disk
	outFile, err := os.Create(storagePath)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}

	limitedReader := io.LimitReader(reader, f.maxSizeBytes+1)
	written, err := io.Copy(outFile, limitedReader)
	outFile.Close()
	if err != nil {
		os.Remove(storagePath)
		return nil, fmt.Errorf("write file: %w", err)
	}
	if written > f.maxSizeBytes {
		os.Remove(storagePath)
		return nil, fmt.Errorf("file too large: %d bytes (max: %d)", written, f.maxSizeBytes)
	}

	info := &AttachmentInfo{
		ID:             id,
		ConversationID: convID,
		Filename:       filename,
		MimeType:       mimeType,
		Size:           written,
		StoragePath:    storagePath,
		CreatedAt:      time.Now(),
	}

	// Record in database
	if f.db != nil {
		_, err := f.db.ExecContext(ctx,
			`INSERT INTO attachments (id, conversation_id, filename, mime_type, size, storage_path)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			info.ID, info.ConversationID, info.Filename, info.MimeType, info.Size, info.StoragePath,
		)
		if err != nil {
			f.logger.Warn("failed to record attachment in database", "err", err)
		}
	}

	f.logger.Info("file stored",
		"id", info.ID,
		"filename", filename,
		"size", written,
		"mime_type", mimeType,
	)

	return info, nil
}

// ReadText reads the text content of a stored attachment.
// Supports text files, CSV, and basic text extraction.
func (f *FileAttachTool) ReadText(info *AttachmentInfo) (string, error) {
	data, err := os.ReadFile(info.StoragePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	mime := strings.ToLower(info.MimeType)
	switch {
	case strings.HasPrefix(mime, "text/"),
		mime == "application/json",
		mime == "application/xml",
		mime == "application/csv":
		return string(data), nil
	default:
		// For binary files, return basic info
		return fmt.Sprintf("[Binary file: %s, size: %d bytes, type: %s]", info.Filename, info.Size, info.MimeType), nil
	}
}

// List returns all attachments for a conversation.
func (f *FileAttachTool) List(ctx context.Context, convID string) ([]AttachmentInfo, error) {
	if f.db == nil {
		return nil, nil
	}

	rows, err := f.db.QueryContext(ctx,
		`SELECT id, conversation_id, filename, mime_type, size, storage_path, created_at
		 FROM attachments WHERE conversation_id = ? ORDER BY created_at DESC`,
		convID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []AttachmentInfo
	for rows.Next() {
		var a AttachmentInfo
		if err := rows.Scan(&a.ID, &a.ConversationID, &a.Filename, &a.MimeType, &a.Size, &a.StoragePath, &a.CreatedAt); err != nil {
			continue
		}
		attachments = append(attachments, a)
	}
	return attachments, nil
}

func generateAttachmentID(convID, filename string) string {
	h := sha256.New()
	h.Write([]byte(convID))
	h.Write([]byte(filename))
	h.Write([]byte(time.Now().String()))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// IsSupportedType checks if a MIME type is supported for processing.
func IsSupportedType(mimeType string) bool {
	mime := strings.ToLower(mimeType)
	supported := []string{
		"text/", "application/json", "application/xml", "application/csv",
		"image/jpeg", "image/png", "image/gif", "image/webp",
		"application/pdf",
	}
	for _, prefix := range supported {
		if strings.HasPrefix(mime, prefix) || mime == prefix {
			return true
		}
	}
	return false
}
