package chat

import (
	"context"

	"llm-api-test/internal/registry"
)

// BenchmarkCase measures latency/throughput of single chat completions.
type BenchmarkCase struct {
	client *Client
}

func (c *BenchmarkCase) ID() string   { return "chat:benchmark" }
func (c *BenchmarkCase) Desc() string { return "POST /v1/chat/completions latency/throughput" }

func (c *BenchmarkCase) Run(ctx context.Context, model, prompt string) *registry.Metrics {
	maxTokens := 4096
	req := &Request{
		Model: model,
		Messages: []Message{{Role: "user", Content: prompt}},
		// Bound generation: without a cap, a thorough prompt can run for
		// minutes, and output length is not what throughput measures. Note
		// that some providers (e.g. DeepSeek v4) ignore max_completion_tokens
		// entirely; the benchmark context timeout is the backstop there.
		MaxCompletionTokens: &maxTokens,
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return &registry.Metrics{Err: err}
	}
	m := res.Metrics
	return &m
}
