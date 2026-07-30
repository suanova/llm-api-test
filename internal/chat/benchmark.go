package chat

import (
	"context"
	"encoding/json"
	"time"

	"llm-api-test/internal/runner"
)

// BenchmarkNonStreaming sends a non-streaming Chat Completions request and
// returns latency metrics. Used as a fallback when streaming is not supported
// by the server.
func (c *Client) BenchmarkNonStreaming(ctx context.Context, req *Request) runner.StreamMetrics {
	start := time.Now()
	resp, err := c.CreateChatCompletion(ctx, req)
	if err != nil {
		return runner.StreamMetrics{Err: err}
	}
	totalTime := time.Since(start)

	text := ""
	if len(resp.Choices) > 0 {
		text = resp.Choices[0].Message.Content
	}

	tokens := 0
	var usage chatUsage
	if resp.Usage != nil {
		json.Unmarshal(resp.Usage, &usage)
	}
	tokens = usage.CompletionTokens
	if tokens <= 0 {
		tokens = countApproxTokens(text)
	}

	return runner.StreamMetrics{
		TTFB:             totalTime,
		TTFT:             totalTime,
		TotalTime:        totalTime,
		CompletionTokens: tokens,
		PromptTokens:     usage.PromptTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		ContentLen:       len(text),
		ChunkCount:       1,
		NonStreaming:     true,
	}
}
