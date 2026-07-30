package chat

import (
	"context"

	"llm-api-test/internal/httpx"
	"llm-api-test/internal/runner"
)

// BenchmarkBasicCase measures latency/throughput for a Chat Completions request.
// It tries streaming first; if the server does not support streaming, it falls
// back to a non-streaming request.
// The Prompt field controls the request style:
//   - "pong" (default): minimal single-token reply, latency-only
//   - "long": paragraph reply, includes throughput metrics
type BenchmarkBasicCase struct {
	Client *Client
	Prompt string
}

func (*BenchmarkBasicCase) Name() string { return "benchmark-chat-basic" }
func (*BenchmarkBasicCase) Desc() string {
	return "Chat Completions API: latency/throughput benchmark"
}

func (tc *BenchmarkBasicCase) RunBenchmark(ctx context.Context, model string) runner.StreamMetrics {
	prompt := tc.Prompt
	if prompt == "" {
		prompt = runner.PromptPong
	}

	req := &Request{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: benchmarkPrompt(prompt)},
		},
	}

	// Request usage in stream (required for accurate token counts from OpenAI).
	req.SetExtra("stream_options", map[string]any{"include_usage": true})

	if prompt == runner.PromptLong {
		req.SetExtra("max_tokens", 256)
	}

	// Try streaming first.
	result, err := tc.Client.StreamChatCompletion(ctx, req)
	if err == nil {
		return result.Metrics
	}

	// If streaming failed with an API error, fall back to non-streaming.
	// Build a fresh request without streaming params.
	if _, ok := err.(*httpx.APIError); ok {
		fallbackReq := &Request{
			Model: model,
			Messages: []Message{
				{Role: "user", Content: benchmarkPrompt(prompt)},
			},
		}
		if prompt == runner.PromptLong {
			fallbackReq.SetExtra("max_tokens", 256)
		}
		return tc.Client.BenchmarkNonStreaming(ctx, fallbackReq)
	}

	return runner.StreamMetrics{Err: err}
}

// benchmarkPrompt returns the user message for the given prompt style.
func benchmarkPrompt(style string) string {
	switch style {
	case runner.PromptLong:
		return "Write a short paragraph about the weather."
	default:
		return "Reply with exactly the word: pong"
	}
}
