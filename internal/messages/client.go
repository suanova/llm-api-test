// Package messages implements the Anthropic Messages API format
// (POST /v1/messages): the client, compatibility cases, and the benchmark
// case. It uses the raw wire format (no SDK).
package messages

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
	"llm-api-test/internal/registry"
)

// CacheControl marks a system block as cacheable.
type CacheControl struct {
	Type string `json:"type"`
}

// SystemBlock is one system prompt block.
type SystemBlock struct {
	Type         string        `json:"type,omitempty"`
	Text         string        `json:"text,omitempty"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// Message is a single user/assistant message.
type Message struct {
	Role         string        `json:"role"`
	Content      string        `json:"content,omitempty"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// Thinking configures extended thinking.
type Thinking struct {
	Type         string `json:"type,omitempty"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// Tool is a tool definition in the request.
type Tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
}

// Request is the Messages API request body.
type Request struct {
	Model       string        `json:"model"`
	MaxTokens   int           `json:"max_tokens"`
	System      []SystemBlock `json:"system,omitempty"`
	Messages    []Message     `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Tools       []Tool        `json:"tools,omitempty"`
	Thinking    *Thinking     `json:"thinking,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
}

// Usage holds token counts from the response.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// ContentBlock is one entry in the response content array.
type ContentBlock struct {
	Type  string          `json:"type"` // text | tool_use | thinking
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// Response is the non-streaming Messages API response body.
type Response struct {
	Content []ContentBlock `json:"content"`
	Usage   *Usage         `json:"usage"`
}

// ToolCall is a tool use in the response.
type ToolCall struct {
	Name  string
	Input json.RawMessage
}

// Result is the outcome of one messages request, shared by the compatibility
// cases (content, tool calls) and the benchmark (metrics).
type Result struct {
	Content   string
	ToolCalls []ToolCall
	Usage     *Usage
	Raw       string
	Metrics   registry.Metrics
}

// Client sends messages requests, streamed or not. Unlike the OpenAI
// formats, authentication is via the x-api-key header.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	debug   io.Writer
	Stream  bool
}

// New creates a messages client.
func New(baseURL, apiKey string, debug io.Writer, stream bool) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{},
		debug:   debug,
		Stream:  stream,
	}
}

// Send posts a messages request.
func (c *Client) Send(ctx context.Context, req *Request) (*Result, error) {
	if c.Stream {
		return c.sendStream(ctx, req)
	}
	return c.sendPlain(ctx, req)
}

// do posts the request and returns the raw response.
func (c *Client) do(ctx context.Context, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if c.debug != nil {
		httpx.DumpRequest(c.debug, httpReq, body)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	return resp, nil
}

// sendPlain sends a non-streaming request and parses the JSON response.
func (c *Client) sendPlain(ctx context.Context, req *Request) (*Result, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	start := time.Now()
	resp, err := c.do(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if c.debug != nil {
		httpx.DumpResponse(c.debug, resp, data)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, httpx.Truncate(string(data), 500))
	}
	var out Response
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	res := &Result{
		Raw:     string(data),
		Metrics: registry.Metrics{Total: time.Since(start)},
	}
	for _, block := range out.Content {
		switch block.Type {
		case "text":
			res.Content += block.Text
		case "tool_use":
			res.ToolCalls = append(res.ToolCalls, ToolCall{Name: block.Name, Input: block.Input})
		}
	}
	if out.Usage != nil {
		res.Usage = out.Usage
		res.Metrics.PromptTokens = out.Usage.InputTokens
		res.Metrics.CompletionTokens = out.Usage.OutputTokens
	}
	return res, nil
}
