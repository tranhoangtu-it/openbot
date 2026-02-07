package provider

import (
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	sharedClientOnce sync.Once
	sharedClient     *http.Client
	sharedTransport  *http.Transport
)

// SharedHTTPClient returns a singleton HTTP client with optimized connection pooling.
// All providers share the same client and transport to maximize connection reuse.
func SharedHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	sharedClientOnce.Do(func() {
		sharedTransport = &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout:  timeout,
			ExpectContinueTimeout:  1 * time.Second,
			ForceAttemptHTTP2:      true,
		}
		sharedClient = &http.Client{
			Timeout:   timeout,
			Transport: sharedTransport,
		}
	})
	return sharedClient
}
