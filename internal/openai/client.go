// Package openai is the shared HTTP plumbing for the OpenAI chat and
// responses API formats: auth header, --http-debug dumps, and response body
// handling. It deliberately avoids the official SDK so we exercise the real
// wire format.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"llm-api-test/internal/httpx"
)

// Client posts to OpenAI-compatible endpoints.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	// Debug, if non-nil, receives a human-readable dump of each request and
	// response. Sensitive headers are redacted.
	Debug io.Writer
}

// New creates a client. There is no HTTP client timeout: streaming responses
// can take a long time, so callers bound requests via context.
func New(baseURL, apiKey string, debug io.Writer) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{},
		Debug:   debug,
	}
}

// Do posts a pre-encoded JSON body to baseURL+path and returns the raw
// response. The caller owns reading and closing the body (streaming reads it
// incrementally). The request is dumped to Debug when set.
func (c *Client) Do(ctx context.Context, path string, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	if c.Debug != nil {
		httpx.DumpRequest(c.Debug, httpReq, body)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	return resp, nil
}

// PostJSON posts body to baseURL+path and reads the whole response. The
// caller checks the status code (non-2xx responses are returned, not errors).
func (c *Client) PostJSON(ctx context.Context, path string, body any) (int, []byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("encode request: %w", err)
	}
	resp, err := c.Do(ctx, path, raw)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read body: %w", err)
	}
	if c.Debug != nil {
		httpx.DumpResponse(c.Debug, resp, data)
	}
	return resp.StatusCode, data, nil
}
