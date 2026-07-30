package anthropic

import (
	"context"
	"encoding/json"
	"time"

	"llm-api-test/internal/runner"
)

// BenchmarkNonStreaming sends a non-streaming Anthropic Messages request and
// returns latency metrics. Used as a fallback when streaming is not supported.
func (c *Client) BenchmarkNonStreaming(ctx context.Context, req *Request) runner.StreamMetrics {
	start := time.Now()
	resp, err := c.CreateMessage(ctx, req)
	if err != nil {
		return runner.StreamMetrics{Err: err}
	}
	totalTime := time.Since(start)

	text := ""
	for _, block := range resp.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}

	tokens := 0
	var u struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
	if resp.Usage != nil {
		json.Unmarshal(resp.Usage, &u)
	}
	tokens = u.OutputTokens
	if tokens <= 0 {
		tokens = countApproxTokens(text)
	}

	return runner.StreamMetrics{
		TTFB:             totalTime,
		TTFT:             totalTime,
		TotalTime:        totalTime,
		CompletionTokens: tokens,
		PromptTokens:     u.InputTokens,
		ContentLen:       len(text),
		ChunkCount:       1,
		NonStreaming:     true,
	}
}
