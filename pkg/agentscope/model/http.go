package model

import (
	"net/http"
	"time"
)

// defaultModelTimeout is the HTTP timeout applied to all model API calls
// when the caller does not supply a custom http.Client.
const defaultModelTimeout = 60 * time.Second

// defaultHTTPClient returns an *http.Client suitable for LLM API calls.
// If a non-nil client is provided it is returned unchanged; otherwise a
// new client with defaultModelTimeout is created.
func defaultHTTPClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: defaultModelTimeout}
}
