package anthropic

import (
	"context"
	"encoding/json"

	"llm-api-test/internal/httpx"
	"llm-api-test/internal/runner"
)

// BenchmarkBasicCase measures latency/throughput for an Anthropic Messages
// request. It tries streaming first; if the server does not support streaming,
// it falls back to a non-streaming request.
// The Prompt field controls the request style:
//   - "pong" (default): minimal single-token reply, latency-only
//   - "long": paragraph reply, includes throughput metrics
type BenchmarkBasicCase struct {
	Client *Client
	Prompt string
}

func (*BenchmarkBasicCase) Name() string { return "benchmark-messages-basic" }
func (*BenchmarkBasicCase) Desc() string {
	return "Anthropic Messages API: latency/throughput benchmark"
}

func (tc *BenchmarkBasicCase) RunBenchmark(ctx context.Context, model string) runner.StreamMetrics {
	prompt := tc.Prompt
	if prompt == "" {
		prompt = runner.PromptPong
	}

	maxTokens := 64
	if prompt == runner.PromptLong {
		maxTokens = 256
	}

	req := &Request{
		Model:     model,
		MaxTokens: maxTokens,
		Messages: []Message{
			{Role: "user", Content: rawJSON(benchmarkPrompt(prompt))},
		},
	}

	// Try streaming first.
	result, err := tc.Client.StreamMessage(ctx, req)
	if err == nil {
		return result.Metrics
	}

	// If streaming failed with an API error, fall back to non-streaming.
	// Build a fresh request without streaming params.
	if _, ok := err.(*httpx.APIError); ok {
		fallbackMaxTokens := 64
		if prompt == runner.PromptLong {
			fallbackMaxTokens = 256
		}
		fallbackReq := &Request{
			Model:     model,
			MaxTokens: fallbackMaxTokens,
			Messages: []Message{
				{Role: "user", Content: rawJSON(benchmarkPrompt(prompt))},
			},
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

// rawJSON marshals a string to json.RawMessage for the content field.
func rawJSON(s string) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
}
