// Package knowledge provides the RAG (Retrieval-Augmented Generation) engine.
package knowledge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"openbot/internal/domain"
)

// Engine manages the knowledge base: adding documents, chunking, and searching.
type Engine struct {
	store     KnowledgeStorer
	chunkSize int
	overlap   int
	logger    *slog.Logger
}

// KnowledgeStorer is the storage interface for the knowledge engine.
type KnowledgeStorer interface {
	AddDocument(ctx context.Context, doc domain.Document, chunks []domain.DocumentChunk) error
	SearchKnowledge(ctx context.Context, query string, topK int) ([]domain.KnowledgeSearchResult, error)
	ListDocuments(ctx context.Context) ([]domain.Document, error)
	DeleteDocument(ctx context.Context, id string) error
}

type EngineConfig struct {
	Store     KnowledgeStorer
	ChunkSize int // tokens per chunk (default: 512)
	Overlap   int // overlap tokens between chunks (default: 50)
	Logger    *slog.Logger
}

func NewEngine(cfg EngineConfig) *Engine {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 512
	}
	if cfg.Overlap < 0 {
		cfg.Overlap = 50
	}
	return &Engine{
		store:     cfg.Store,
		chunkSize: cfg.ChunkSize,
		overlap:   cfg.Overlap,
		logger:    cfg.Logger,
	}
}

// AddDocument adds a document to the knowledge base by chunking its content
// and storing both the document metadata and chunks.
func (e *Engine) AddDocument(ctx context.Context, name, mimeType, content string) (*domain.Document, error) {
	// Generate document ID from content hash
	hash := sha256.Sum256([]byte(content))
	docID := fmt.Sprintf("%x", hash[:8])

	chunks := e.chunkText(content, docID)

	doc := domain.Document{
		ID:         docID,
		Name:       name,
		MimeType:   mimeType,
		Size:       int64(len(content)),
		ChunkCount: len(chunks),
		CreatedAt:  time.Now(),
	}

	if err := e.store.AddDocument(ctx, doc, chunks); err != nil {
		return nil, fmt.Errorf("store document: %w", err)
	}

	e.logger.Info("document added to knowledge base",
		"name", name, "chunks", len(chunks), "size", len(content))

	return &doc, nil
}

// Search queries the knowledge base and returns relevant chunks.
func (e *Engine) Search(ctx context.Context, query string, topK int) ([]domain.KnowledgeSearchResult, error) {
	if topK <= 0 {
		topK = 5
	}
	return e.store.SearchKnowledge(ctx, query, topK)
}

// ListDocuments returns all documents in the knowledge base.
func (e *Engine) ListDocuments(ctx context.Context) ([]domain.Document, error) {
	return e.store.ListDocuments(ctx)
}

// DeleteDocument removes a document from the knowledge base.
func (e *Engine) DeleteDocument(ctx context.Context, id string) error {
	return e.store.DeleteDocument(ctx, id)
}

// BuildContext generates a context string from search results for prompt injection.
func (e *Engine) BuildContext(results []domain.KnowledgeSearchResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Relevant Knowledge\n\n")
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("### Source: %s (chunk %d)\n", r.DocName, r.Chunk.ChunkIndex))
		sb.WriteString(r.Chunk.Content)
		if i < len(results)-1 {
			sb.WriteString("\n\n---\n\n")
		}
	}
	return sb.String()
}

// chunkText splits text into overlapping chunks of approximately chunkSize words.
func (e *Engine) chunkText(text, docID string) []domain.DocumentChunk {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	var chunks []domain.DocumentChunk
	step := e.chunkSize - e.overlap
	if step <= 0 {
		step = e.chunkSize
	}

	for i := 0; i < len(words); i += step {
		end := i + e.chunkSize
		if end > len(words) {
			end = len(words)
		}

		content := strings.Join(words[i:end], " ")
		chunks = append(chunks, domain.DocumentChunk{
			ID:         fmt.Sprintf("%s_%d", docID, len(chunks)),
			DocumentID: docID,
			Content:    content,
			ChunkIndex: len(chunks),
			TokenCount: end - i,
		})

		if end >= len(words) {
			break
		}
	}

	return chunks
}
