package openai

import (
	"context"
	"encoding/json"
	"time"

	"llm-api-test/internal/runner"
)

// BenchmarkNonStreaming sends a non-streaming Responses API request and returns
// latency metrics. Used as a fallback when streaming is not supported by the
// server.
func (c *Client) BenchmarkNonStreaming(ctx context.Context, req *Request) runner.StreamMetrics {
	start := time.Now()
	resp, err := c.CreateResponse(ctx, req)
	if err != nil {
		return runner.StreamMetrics{Err: err}
	}
	totalTime := time.Since(start)

	text := outputTextFromResponse(resp.Output)
	tokens := 0
	var usage responsesStreamUsage
	if resp.Usage != nil {
		json.Unmarshal(resp.Usage, &usage)
	}
	tokens = usage.OutputTokens
	if tokens <= 0 {
		tokens = countApproxTokens(text)
	}

	return runner.StreamMetrics{
		TTFB:             totalTime, // all content at once
		TTFT:             totalTime, // same as total for non-streaming
		TotalTime:        totalTime,
		CompletionTokens: tokens,
		PromptTokens:     usage.InputTokens,
		ContentLen:       len(text),
		ChunkCount:       1,
		NonStreaming:     true,
	}
}
