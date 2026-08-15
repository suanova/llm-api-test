package messages

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// BasicCase verifies that the endpoint returns a text content block for a
// simple prompt.
type BasicCase struct {
	client *Client
}

func (c *BasicCase) ID() string   { return "messages:basic" }
func (c *BasicCase) Name() string { return "basic" }
func (c *BasicCase) Desc() string { return "POST /v1/messages returns a text content block" }

func (c *BasicCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model:     model,
		MaxTokens: 1024,
		Messages:  []Message{{Role: "user", Content: "Reply with exactly the word: pong"}},
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if res.Content == "" && len(res.ToolCalls) == 0 {
		return cases.FailRaw(res.Raw, "no text content block")
	}
	return cases.Pass("text content block present", res.Raw)
}
