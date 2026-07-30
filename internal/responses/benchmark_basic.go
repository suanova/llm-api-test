package responses

import (
	"context"

	"llm-api-test/internal/httpx"
	"llm-api-test/internal/openai"
	"llm-api-test/internal/runner"
)

// BenchmarkBasicCase measures latency/throughput for a Responses API request.
// It tries streaming first; if the server does not support streaming, it falls
// back to a non-streaming request.
// The Prompt field controls the request style:
//   - "pong" (default): minimal single-token reply, latency-only
//   - "long": paragraph reply, includes throughput metrics
type BenchmarkBasicCase struct {
	Client *openai.Client
	Prompt string
}

func (*BenchmarkBasicCase) Name() string { return "benchmark-responses-basic" }
func (*BenchmarkBasicCase) Desc() string {
	return "Responses API: latency/throughput benchmark"
}

func (tc *BenchmarkBasicCase) RunBenchmark(ctx context.Context, model string) runner.StreamMetrics {
	prompt := tc.Prompt
	if prompt == "" {
		prompt = runner.PromptPong
	}

	req := &openai.Request{
		Model: model,
		Input: mustInput(benchmarkPrompt(prompt)),
	}

	if prompt == runner.PromptLong {
		req.SetExtra("max_output_tokens", 256)
	}

	// Try streaming first.
	result, err := tc.Client.StreamResponse(ctx, req)
	if err == nil {
		return result.Metrics
	}

	// If streaming failed with an API error (e.g. server returns 200 with
	// error body when stream=true is set), fall back to non-streaming.
	// Build a fresh request without streaming params — the original req
	// may have had stream=true added by StreamResponse.
	if _, ok := err.(*httpx.APIError); ok {
		fallbackReq := &openai.Request{
			Model: model,
			Input: mustInput(benchmarkPrompt(prompt)),
		}
		if prompt == runner.PromptLong {
			fallbackReq.SetExtra("max_output_tokens", 256)
		}
		return tc.Client.BenchmarkNonStreaming(ctx, fallbackReq)
	}

	// Other errors (network, context) are not recoverable.
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
