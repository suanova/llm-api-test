package messages

import (
	"context"

	"llm-api-test/internal/registry"
)

// BenchmarkCase measures latency/throughput of single messages requests.
type BenchmarkCase struct {
	client    *Client
	maxTokens int // generation cap; 0 = default 4096
}

func (c *BenchmarkCase) ID() string   { return "messages:benchmark" }
func (c *BenchmarkCase) Desc() string { return "POST /v1/messages latency/throughput" }

func (c *BenchmarkCase) Run(ctx context.Context, model, prompt string) *registry.Metrics {
	maxTokens := c.maxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	// Bound generation: without a cap, a thorough prompt can run for
	// minutes, and output length is not what throughput measures.
	req := &Request{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  []Message{{Role: "user", Content: prompt}},
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return &registry.Metrics{Err: err}
	}
	m := res.Metrics
	return &m
}
