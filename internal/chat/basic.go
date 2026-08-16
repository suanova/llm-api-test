package chat

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// BasicCase verifies that the endpoint returns an assistant message for a
// simple prompt.
type BasicCase struct {
	client *Client
}

func (c *BasicCase) ID() string   { return "chat:basic" }
func (c *BasicCase) Name() string { return "basic" }
func (c *BasicCase) Desc() string { return "POST /v1/chat/completions returns an assistant message" }

func (c *BasicCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model:    model,
		Messages: []Message{{Role: "user", Content: "Reply with exactly the word: pong"}},
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if res.Content == "" && len(res.ToolCalls) == 0 {
		return cases.FailRaw(res.Raw, "no assistant output")
	}
	return cases.Pass("assistant message present", res.Raw)
}
