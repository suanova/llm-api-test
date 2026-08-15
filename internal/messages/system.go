package messages

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// SystemCase verifies that the endpoint accepts and follows a system prompt.
type SystemCase struct {
	client *Client
}

func (c *SystemCase) ID() string   { return "messages:system" }
func (c *SystemCase) Name() string { return "system" }
func (c *SystemCase) Desc() string { return "POST /v1/messages accepts and follows a system prompt" }

func (c *SystemCase) Run(ctx context.Context, model string) *registry.CompatResult {
	zero := 0.0
	req := &Request{
		Model:     model,
		MaxTokens: 1024,
		System:    []SystemBlock{{Type: "text", Text: "Reply with exactly: hello"}},
		Messages:  []Message{{Role: "user", Content: "Say: hello"}},
		// The assertion below is an exact match on model output, so the
		// request must be deterministic: providers default to temperature 1.0,
		// and reasoning models can reinterpret open-ended questions, both of
		// which make the test flaky.
		Temperature: &zero,
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if res.Content != "hello" {
		return cases.FailRaw(res.Raw, "system prompt not followed (got %q)", res.Content)
	}
	return cases.Pass("system prompt followed", res.Raw)
}
