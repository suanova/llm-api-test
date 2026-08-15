package responses

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// InstructionsCase verifies that the endpoint accepts instructions and
// follows them.
type InstructionsCase struct {
	client *Client
}

func (c *InstructionsCase) ID() string   { return "responses:instructions" }
func (c *InstructionsCase) Name() string { return "instructions" }
func (c *InstructionsCase) Desc() string {
	return "POST /responses accepts `instructions` and follows them"
}

func (c *InstructionsCase) Run(ctx context.Context, model string) *registry.CompatResult {
	instructions := "Reply with exactly: hello"
	zero := 0.0
	req := &Request{
		Model:        model,
		Input:        "Say: hello",
		Instructions: &instructions,
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
		return cases.FailRaw(res.Raw, "instructions not followed (got %q)", res.Content)
	}
	return cases.Pass("instructions followed", res.Raw)
}
