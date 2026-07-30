// Package chat is a thin raw HTTP client for the OpenAI Chat Completions API
// (POST /v1/chat/completions). Like internal/openai it avoids the SDK so we
// exercise the real wire format.
package chat

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

// Client posts to /v1/chat/completions on a Chat-Completions-compatible server.
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
		// No Timeout on the client — streaming responses can take a long time.
		// Per-request timeouts are set via context in each method call.
		HTTP: &http.Client{},
	}
}

// Message is a single chat message. Content is a string for simple roles; for
// tool/assistant messages it may be omitted. ToolCalls is set on assistant
// messages that requested tool calls. ToolCallID links a tool-role message to
// the call it answers.
type Message struct {
	Role       string      `json:"role"`
	Content    string      `json:"content,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Name       string      `json:"name,omitempty"`
}

// ToolCall is a single tool call in an assistant message.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction is the function part of a tool call.
type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is a tool definition in the request.
type Tool struct {
	Type     string          `json:"type"`
	Function json.RawMessage `json:"function"`
}

// Request is a minimal subset of the Chat Completions request body. Extra
// params (temperature, seed, n, logprobs, ...) go through Extra.
type Request struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Tools          []Tool          `json:"tools,omitempty"`
	ToolChoice     json.RawMessage `json:"tool_choice,omitempty"` // string or object
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`
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

// Response is a minimal model of the Chat Completions response body. Raw is
// kept for cases to inspect anything not modeled here.
type Response struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Model   string          `json:"model"`
	Choices []Choice        `json:"choices"`
	Usage   json.RawMessage `json:"usage"`
	Raw     json.RawMessage `json:"-"`
}

// Choice is a single completion alternative.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// CreateChatCompletion posts the request to /v1/chat/completions and returns
// the parsed body. On non-2xx it returns a rich error including the raw body.
func (c *Client) CreateChatCompletion(ctx context.Context, req *Request) (*Response, error) {
	body, err := encodeRequest(req)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

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
