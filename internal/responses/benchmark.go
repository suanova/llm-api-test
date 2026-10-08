package responses

import (
	"context"

	"llm-api-test/internal/registry"
)

// BenchmarkCase measures latency/throughput of single responses requests.
type BenchmarkCase struct {
	client          *Client
	maxTokens       int    // generation cap; 0 = default 4096
	reasoningEffort string // passed as reasoning.effort; empty = provider default
}

func (c *BenchmarkCase) ID() string   { return "responses:benchmark" }
func (c *BenchmarkCase) Desc() string { return "POST /responses latency/throughput" }

func (c *BenchmarkCase) Run(ctx context.Context, model, prompt string) *registry.Metrics {
	maxTokens := c.maxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	req := &Request{
		Model: model,
		Input: prompt,
		// Bound generation: without a cap, a thorough prompt can run for
		// minutes, and output length is not what throughput measures.
		MaxOutputTokens: &maxTokens,
	}
	if c.reasoningEffort != "" {
		req.Reasoning = &Reasoning{Effort: c.reasoningEffort}
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return &registry.Metrics{Err: err}
	}
	m := res.Metrics
	return &m
}
