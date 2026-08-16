// Package responses implements the OpenAI Responses API format
// (POST /responses): the client, compatibility cases, and the benchmark case.
// It deliberately avoids the official SDK so we exercise the real wire format.
package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"llm-api-test/internal/httpx"
	"llm-api-test/internal/openai"
	"llm-api-test/internal/registry"
)

// Tool is a tool definition in the request.
type Tool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Reasoning configures reasoning behavior.
type Reasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// Text configures the text response format.
type Text struct {
	Verbosity string          `json:"verbosity,omitempty"`
	Format    json.RawMessage `json:"format,omitempty"`
}

// Request is the Responses API request body.
type Request struct {
	Model          string     `json:"model"`
	Input          string     `json:"input"`
	Instructions   *string    `json:"instructions,omitempty"`
	Stream         bool       `json:"stream,omitempty"`
	Tools          []Tool     `json:"tools,omitempty"`
	Reasoning      *Reasoning `json:"reasoning,omitempty"`
	Text           *Text      `json:"text,omitempty"`
	PromptCacheKey   string     `json:"prompt_cache_key,omitempty"`
	Temperature      *float64   `json:"temperature,omitempty"`
	MaxOutputTokens  *int       `json:"max_output_tokens,omitempty"`
}

// Usage holds token counts from the response.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// OutputItem is one entry in the response output array.
type OutputItem struct {
	Type    string `json:"type"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Response is the non-streaming Responses API response body.
type Response struct {
	Output []OutputItem `json:"output"`
	Usage  *Usage       `json:"usage"`
}

// ToolCall is a function call in the response.
type ToolCall struct {
	Name      string
	Arguments string
}

// Result is the outcome of one responses request, shared by the compatibility
// cases (content, tool calls) and the benchmark (metrics).
type Result struct {
	Content   string
	ToolCalls []ToolCall
	Usage     *Usage
	Raw       string
	Metrics   registry.Metrics
}

// Client sends responses requests, streamed or not.
type Client struct {
	oc     *openai.Client
	Stream bool
}

// New creates a responses client.
func New(baseURL, apiKey string, debug io.Writer, stream bool) *Client {
	return &Client{oc: openai.New(baseURL, apiKey, debug), Stream: stream}
}

// Send posts a responses request.
func (c *Client) Send(ctx context.Context, req *Request) (*Result, error) {
	if c.Stream {
		return c.sendStream(ctx, req)
	}
	return c.sendPlain(ctx, req)
}

// sendPlain sends a non-streaming request and parses the JSON response.
func (c *Client) sendPlain(ctx context.Context, req *Request) (*Result, error) {
	start := time.Now()
	status, data, err := c.oc.PostJSON(ctx, "/responses", req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", status, httpx.Truncate(string(data), 500))
	}
	var out Response
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	res := &Result{
		Raw:     string(data),
		Metrics: registry.Metrics{Total: time.Since(start)},
	}
	for _, item := range out.Output {
		switch item.Type {
		case "message":
			for _, block := range item.Content {
				if block.Type == "output_text" {
					res.Content += block.Text
				}
			}
		case "function_call":
			res.ToolCalls = append(res.ToolCalls, ToolCall{Name: item.Name, Arguments: item.Arguments})
		}
	}
	if out.Usage != nil {
		res.Usage = out.Usage
		res.Metrics.PromptTokens = out.Usage.InputTokens
		res.Metrics.CompletionTokens = out.Usage.OutputTokens
	}
	return res, nil
}
