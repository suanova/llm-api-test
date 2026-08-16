package messages

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// ThinkingCase verifies that the endpoint accepts extended thinking.
type ThinkingCase struct {
	client *Client
}

func (c *ThinkingCase) ID() string   { return "messages:thinking" }
func (c *ThinkingCase) Name() string { return "thinking" }
func (c *ThinkingCase) Desc() string {
	return "POST /v1/messages accepts `thinking` (extended thinking)"
}

func (c *ThinkingCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model:     model,
		MaxTokens: 1024,
		Messages:  []Message{{Role: "user", Content: "Reply with exactly the word: pong"}},
		Thinking:  &Thinking{Type: "enabled", BudgetTokens: 4096},
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if res.Content == "" && len(res.ToolCalls) == 0 {
		return cases.FailRaw(res.Raw, "no text content block")
	}
	return cases.Pass("thinking accepted", res.Raw)
}
