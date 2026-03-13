package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// DoJSONRequest sends a JSON request and decodes a JSON response with basic retries.
// It is intended for outbound calls to LLM providers and other HTTP JSON APIs.
func DoJSONRequest(
	ctx context.Context,
	client *http.Client,
	method string,
	url string,
	reqBody any,
	respBody any,
	headers map[string]string,
) error {
	if client == nil {
		client = http.DefaultClient
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("httpx: marshal request: %w", err)
	}

	var lastErr error
	baseBackoff := 200 * time.Millisecond
	maxAttempts := 3

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter could be added here if needed.
			time.Sleep(baseBackoff * time.Duration(1<<attempt))
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("httpx: new request: %w", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		logrus.WithFields(logrus.Fields{
			"method":  method,
			"url":     url,
			"attempt": attempt + 1,
		}).Debug("httpx: sending JSON request")

		resp, err := client.Do(req)
		if err != nil {
			// Retry on temporary network errors.
			if isRetryableError(err) && attempt < maxAttempts-1 {
				logrus.WithError(err).WithFields(logrus.Fields{
					"method":  method,
					"url":     url,
					"attempt": attempt + 1,
				}).Warn("httpx: retrying after network error")
				lastErr = err
				continue
			}
			return fmt.Errorf("httpx: do request: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("httpx: read response: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			// Retry 5xx errors.
			if resp.StatusCode >= 500 && attempt < maxAttempts-1 {
				logrus.WithFields(logrus.Fields{
					"method":     method,
					"url":        url,
					"statusCode": resp.StatusCode,
					"attempt":    attempt + 1,
				}).Warn("httpx: server error, will retry")
				lastErr = fmt.Errorf("httpx: unexpected status %d: %s", resp.StatusCode, string(body))
				continue
			}
			return fmt.Errorf("httpx: unexpected status %d: %s", resp.StatusCode, string(body))
		}

		if respBody != nil {
			if err := json.Unmarshal(body, respBody); err != nil {
				return fmt.Errorf("httpx: decode response: %w", err)
			}
		}
		logrus.WithFields(logrus.Fields{
			"method":     method,
			"url":        url,
			"statusCode": resp.StatusCode,
		}).Debug("httpx: request succeeded")
		return nil
	}

	if lastErr != nil {
		logrus.WithError(lastErr).WithFields(logrus.Fields{
			"method": method,
			"url":    url,
		}).Error("httpx: request failed after retries")
		return lastErr
	}
	return fmt.Errorf("httpx: request failed after %d attempts", maxAttempts)
}

func isRetryableError(err error) bool {
	var netErr net.Error
	if ok := errorAs(err, &netErr); ok {
		return netErr.Timeout() || netErr.Temporary()
	}
	// Fallback: treat all errors as non-retryable unless identified above.
	return false
}

// errorAs is a small wrapper to avoid importing errors in every file.
func errorAs(err error, target any) bool {
	switch t := target.(type) {
	case *net.Error:
		if ne, ok := err.(net.Error); ok {
			*t = ne
			return true
		}
	}
	return false
}
