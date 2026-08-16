package chat

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// SystemMessageCase verifies that the endpoint accepts a system message and
// follows it.
type SystemMessageCase struct {
	client *Client
}

func (c *SystemMessageCase) ID() string   { return "chat:system-message" }
func (c *SystemMessageCase) Name() string { return "system-message" }
func (c *SystemMessageCase) Desc() string {
	return "POST /v1/chat/completions accepts and follows a system message"
}

func (c *SystemMessageCase) Run(ctx context.Context, model string) *registry.CompatResult {
	zero := 0.0
	req := &Request{
		Model: model,
		Messages: []Message{
			{Role: "system", Content: "Reply with exactly: hello"},
			{Role: "user", Content: "Say: hello"},
		},
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
		return cases.FailRaw(res.Raw, "system message not followed (got %q)", res.Content)
	}
	return cases.Pass("system message followed", res.Raw)
}
