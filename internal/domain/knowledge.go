package domain

import (
	"context"
	"time"
)

// Document represents an uploaded document in the knowledge base.
type Document struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	MimeType  string    `json:"mime_type"`
	Size      int64     `json:"size"`
	ChunkCount int      `json:"chunk_count"`
	CreatedAt time.Time `json:"created_at"`
}

// DocumentChunk represents a single chunk of a document, indexed for search.
type DocumentChunk struct {
	ID         string `json:"id"`
	DocumentID string `json:"document_id"`
	Content    string `json:"content"`
	ChunkIndex int    `json:"chunk_index"`
	TokenCount int    `json:"token_count"`
}

// KnowledgeSearchResult represents a search hit in the knowledge base.
type KnowledgeSearchResult struct {
	Chunk    DocumentChunk `json:"chunk"`
	DocName  string        `json:"doc_name"`
	Score    float64       `json:"score"`
}

// KnowledgeStore defines the interface for the knowledge engine (RAG).
type KnowledgeStore interface {
	// AddDocument stores a document and its chunks.
	AddDocument(ctx context.Context, doc Document, chunks []DocumentChunk) error

	// Search performs full-text search and returns the top-k results.
	Search(ctx context.Context, query string, topK int) ([]KnowledgeSearchResult, error)

	// ListDocuments returns all stored documents.
	ListDocuments(ctx context.Context) ([]Document, error)

	// DeleteDocument removes a document and all its chunks.
	DeleteDocument(ctx context.Context, id string) error
}
