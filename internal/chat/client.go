// Package chat implements the OpenAI Chat Completions API format
// (POST /v1/chat/completions): the client, compatibility cases, and the
// benchmark case. It deliberately avoids the official SDK so we exercise the
// real wire format.
package chat

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

// Message is a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
}

// ToolCall is a tool call in an assistant response.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Tool is a tool definition in the request.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction is the function part of a tool definition.
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Request is the Chat Completions request body.
type Request struct {
	Model               string          `json:"model"`
	Messages            []Message       `json:"messages"`
	Stream              bool            `json:"stream,omitempty"`
	Tools               []Tool          `json:"tools,omitempty"`
	ResponseFormat      json.RawMessage `json:"response_format,omitempty"`
	Seed                *int            `json:"seed,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
}

// Usage holds token counts from the response.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// PromptTokensDetails is the OpenAI-standard automatic-cache field.
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
	// PromptCacheHitTokens/MissTokens is the DeepSeek-style cache pair;
	// DeepSeek reports these instead of prompt_tokens_details.
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
}

// Response is the non-streaming Chat Completions response body.
type Response struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

// Result is the outcome of one chat completion request, shared by the
// compatibility cases (content, tool calls) and the benchmark (metrics).
type Result struct {
	Content   string
	ToolCalls []ToolCall
	Usage     *Usage
	Raw       string
	Metrics   registry.Metrics
}

// Client sends chat completion requests, streamed or not.
type Client struct {
	oc     *openai.Client
	Stream bool
}

// New creates a chat client.
func New(baseURL, apiKey string, debug io.Writer, stream bool) *Client {
	return &Client{oc: openai.New(baseURL, apiKey, debug), Stream: stream}
}

// Send posts a chat completion request.
func (c *Client) Send(ctx context.Context, req *Request) (*Result, error) {
	if c.Stream {
		return c.sendStream(ctx, req)
	}
	return c.sendPlain(ctx, req)
}

// sendPlain sends a non-streaming request and parses the JSON response.
func (c *Client) sendPlain(ctx context.Context, req *Request) (*Result, error) {
	start := time.Now()
	status, data, err := c.oc.PostJSON(ctx, "/chat/completions", req)
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
	if len(out.Choices) > 0 {
		res.Content = out.Choices[0].Message.Content
		res.ToolCalls = out.Choices[0].Message.ToolCalls
	}
	if out.Usage != nil {
		res.Usage = out.Usage
		res.Metrics.PromptTokens = out.Usage.PromptTokens
		res.Metrics.CompletionTokens = out.Usage.CompletionTokens
	}
	return res, nil
}
