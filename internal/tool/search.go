package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	searchTimeout   = 15 * time.Second
	fetchMaxBytes   = 100 * 1024 // 100KB
	fetchMaxOutput  = 10000
	userAgentString = "OpenBot/0.1"
)

// WebSearchTool searches the web using DuckDuckGo Instant Answer API.
type WebSearchTool struct {
	client *http.Client
}

func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{
		client: &http.Client{Timeout: searchTimeout},
	}
}

func (t *WebSearchTool) Name() string { return "web_search" }
func (t *WebSearchTool) Description() string {
	return "Search the web for information. Returns a summary of search results. Use for current events, facts, or anything you're unsure about."
}
func (t *WebSearchTool) Parameters() map[string]any {
	return ToolParameters(
		map[string]Param{
			"query": {Type: "string", Description: "Search query to look up on the web"},
		},
		[]string{"query"},
	)
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query := ArgsString(args, "query")
	if query == "" {
		return "", fmt.Errorf("missing argument: query")
	}

	// Use DuckDuckGo Instant Answer API (no key required)
	endpoint := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgentString)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var ddg ddgResponse
	if err := json.Unmarshal(body, &ddg); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	var results []string

	if ddg.Abstract != "" {
		results = append(results, fmt.Sprintf("## %s\n%s\nSource: %s", ddg.Heading, ddg.Abstract, ddg.AbstractURL))
	}

	if ddg.Answer != "" {
		results = append(results, fmt.Sprintf("Answer: %s", ddg.Answer))
	}

	for i, topic := range ddg.RelatedTopics {
		if i >= 5 {
			break
		}
		if topic.Text != "" {
			results = append(results, fmt.Sprintf("- %s", topic.Text))
		}
	}

	if len(results) == 0 {
		return fmt.Sprintf("No instant results found for: %s. Try a more specific query.", query), nil
	}

	return strings.Join(results, "\n\n"), nil
}

// WebFetchTool fetches content from a URL.
type WebFetchTool struct {
	client *http.Client
}

func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{
		client: &http.Client{Timeout: searchTimeout},
	}
}

func (t *WebFetchTool) Name() string { return "web_fetch" }
func (t *WebFetchTool) Description() string {
	return "Fetch the content of a web page by URL. Returns the text content (HTML stripped). Useful for reading articles, documentation, etc."
}
func (t *WebFetchTool) Parameters() map[string]any {
	return ToolParameters(
		map[string]Param{
			"url": {Type: "string", Description: "Full URL to fetch (must start with http:// or https://)"},
		},
		[]string{"url"},
	)
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	rawURL := ArgsString(args, "url")
	if rawURL == "" {
		return "", fmt.Errorf("missing argument: url")
	}

	// Validate URL scheme to prevent SSRF
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme: %s (only http/https allowed)", parsed.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", userAgentString)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	limitReader := io.LimitReader(resp.Body, fetchMaxBytes)
	body, err := io.ReadAll(limitReader)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	text := stripHTMLTags(string(body))
	if len(text) > fetchMaxOutput {
		text = text[:fetchMaxOutput] + "\n... (truncated)"
	}

	return text, nil
}

// stripHTMLTags removes HTML tags from a string (simple approach).
func stripHTMLTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	text := result.String()
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}

// DuckDuckGo response types
type ddgResponse struct {
	Abstract      string     `json:"Abstract"`
	AbstractURL   string     `json:"AbstractURL"`
	Heading       string     `json:"Heading"`
	Answer        string     `json:"Answer"`
	RelatedTopics []ddgTopic `json:"RelatedTopics"`
}

type ddgTopic struct {
	Text     string `json:"Text"`
	FirstURL string `json:"FirstURL"`
}
