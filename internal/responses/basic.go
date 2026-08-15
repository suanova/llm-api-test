package responses

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// BasicCase verifies that the endpoint returns output text for a simple
// prompt.
type BasicCase struct {
	client *Client
}

func (c *BasicCase) ID() string   { return "responses:basic" }
func (c *BasicCase) Name() string { return "basic" }
func (c *BasicCase) Desc() string { return "POST /responses returns output text for a simple prompt" }

func (c *BasicCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model: model,
		Input: "Reply with exactly the word: pong",
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if res.Content == "" && len(res.ToolCalls) == 0 {
		return cases.FailRaw(res.Raw, "no output text")
	}
	return cases.Pass("output text present", res.Raw)
}
