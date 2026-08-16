package messages

import (
	"context"

	"llm-api-test/internal/registry"
)

// BenchmarkCase measures latency/throughput of single messages requests.
type BenchmarkCase struct {
	client *Client
}

func (c *BenchmarkCase) ID() string   { return "messages:benchmark" }
func (c *BenchmarkCase) Desc() string { return "POST /v1/messages latency/throughput" }

func (c *BenchmarkCase) Run(ctx context.Context, model, prompt string) *registry.Metrics {
	// Bound generation: without a cap, a thorough prompt can run for
	// minutes, and output length is not what throughput measures.
	req := &Request{
		Model:     model,
		MaxTokens: 4096,
		Messages:  []Message{{Role: "user", Content: prompt}},
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return &registry.Metrics{Err: err}
	}
	m := res.Metrics
	return &m
}
