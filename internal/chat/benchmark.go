package chat

import (
	"context"

	"llm-api-test/internal/registry"
)

// BenchmarkCase measures latency/throughput of single chat completions.
type BenchmarkCase struct {
	client          *Client
	maxTokens       int    // generation cap; 0 = default 4096
	reasoningEffort string // passed as reasoning_effort; empty = provider default
}

func (c *BenchmarkCase) ID() string   { return "chat:benchmark" }
func (c *BenchmarkCase) Desc() string { return "POST /v1/chat/completions latency/throughput" }

func (c *BenchmarkCase) Run(ctx context.Context, model, prompt string) *registry.Metrics {
	maxTokens := c.maxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	req := &Request{
		Model:    model,
		Messages: []Message{{Role: "user", Content: prompt}},
		// Bound generation: without a cap, a thorough prompt can run for
		// minutes, and output length is not what throughput measures. Note
		// that some providers (e.g. DeepSeek v4) ignore max_completion_tokens
		// entirely; the benchmark context timeout is the backstop there.
		MaxCompletionTokens: &maxTokens,
		ReasoningEffort:     c.reasoningEffort,
	}
	if c.client.Stream {
		// Streamed responses carry usage only on request; the benchmark's
		// token, cache, and throughput metrics depend on it.
		req.StreamOptions = &StreamOptions{IncludeUsage: true}
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return &registry.Metrics{Err: err}
	}
	m := res.Metrics
	return &m
}
