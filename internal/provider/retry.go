package provider

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"
)

const maxRetries = 3

// retryableError indicates a transient failure that can be retried.
type retryableError struct {
	statusCode int
	body       string
}

func (e *retryableError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.statusCode, e.body)
}

// doWithRetry executes an HTTP request with exponential backoff retry
// for transient errors (network failures, 5xx, 429).
func doWithRetry(ctx context.Context, client *http.Client, buildReq func() (*http.Request, error), logger *slog.Logger) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter to prevent thundering herd.
			base := time.Duration(attempt*attempt) * time.Second
			jitter := time.Duration(rand.Int64N(int64(base/2 + 1)))
			backoff := base + jitter
			logger.Warn("retrying request", "attempt", attempt+1, "backoff", backoff)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := buildReq()
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				logger.Warn("request failed, will retry", "error", err)
				continue
			}
			return nil, fmt.Errorf("request failed after %d retries: %w", maxRetries, err)
		}

		// Retry on 5xx server errors and 429 rate-limit.
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = &retryableError{statusCode: resp.StatusCode, body: string(body)}
			if attempt < maxRetries {
				logger.Warn("server error, will retry",
					"status", resp.StatusCode, "body", string(body))
				continue
			}
			return nil, fmt.Errorf("server error after %d retries: %w", maxRetries, lastErr)
		}

		return resp, nil
	}

	return nil, lastErr
}
