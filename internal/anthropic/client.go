// Package anthropic is a thin raw HTTP client for the Anthropic Messages API
// (POST /v1/messages). Like the other clients it avoids any SDK so we exercise
// the real wire format.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"llm-api-test/internal/httpx"
)

const defaultTimeout = 120 * time.Second

// Client posts to /v1/messages on an Anthropic-Messages-compatible server.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	// DebugWriter, if non-nil, receives a human-readable dump of each request
	// and response. Sensitive headers are redacted.
	DebugWriter io.Writer
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: defaultTimeout},
	}
}

// Message is a single message in the request messages array. Content is a
// string for simple user messages; for tool_result messages it's a block array.
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or []ContentBlock
}

// Tool is a tool definition in the request.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Request is a minimal subset of the Anthropic Messages request body. Extra
// params go through Extra.
type Request struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []Message       `json:"messages"`
	System    json.RawMessage `json:"system,omitempty"` // string or []SystemBlock
	Tools     []Tool          `json:"tools,omitempty"`
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"` // object, e.g. {"type":"auto"}
	Thinking  json.RawMessage `json:"thinking,omitempty"`    // object, e.g. {"type":"enabled","budget_tokens":N}
	// Extra passthrough params.
	Extra map[string]json.RawMessage `json:"-"`
}

// SetExtra attaches a raw-JSON extra param to the request.
func (r *Request) SetExtra(key string, val any) {
	if r.Extra == nil {
		r.Extra = map[string]json.RawMessage{}
	}
	b, err := json.Marshal(val)
	if err != nil {
		return
	}
	r.Extra[key] = b
}

// Response is a minimal model of the Anthropic Messages response body.
type Response struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Role        string         `json:"role"`
	Content     []ContentBlock `json:"content"`
	Model       string         `json:"model"`
	StopReason  string         `json:"stop_reason"`
	Usage       json.RawMessage `json:"usage"`
	Raw         json.RawMessage `json:"-"`
}

// ContentBlock is a single entry in the response content array.
type ContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// CreateMessage posts the request to /v1/messages and returns the parsed body.
// On non-2xx it returns a rich error including the raw body.
func (c *Client) CreateMessage(ctx context.Context, req *Request) (*Response, error) {
	body, err := encodeRequest(req)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	if c.DebugWriter != nil {
		httpx.DumpRequest(c.DebugWriter, httpReq, body)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if c.DebugWriter != nil {
		httpx.DumpResponse(c.DebugWriter, resp, raw)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &httpx.APIError{Status: resp.StatusCode, Body: raw}
	}

	var out Response
	out.Raw = raw
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, httpx.Truncate(string(raw), 500))
	}
	return &out, nil
}

// encodeRequest marshals the request, merging Extra fields into the top-level
// JSON object so callers can add arbitrary params.
func encodeRequest(req *Request) ([]byte, error) {
	type alias Request
	b, err := json.Marshal((*alias)(req))
	if err != nil {
		return nil, err
	}
	if len(req.Extra) == 0 {
		return b, nil
	}
	merged := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &merged); err != nil {
		return nil, err
	}
	for k, v := range req.Extra {
		merged[k] = v
	}
	return json.Marshal(merged)
}
