package responses

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// VerbosityCase verifies that the endpoint accepts text.verbosity.
type VerbosityCase struct {
	client *Client
}

func (c *VerbosityCase) ID() string   { return "responses:text.verbosity" }
func (c *VerbosityCase) Name() string { return "text.verbosity" }
func (c *VerbosityCase) Desc() string { return "POST /responses accepts `text.verbosity`" }

func (c *VerbosityCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model: model,
		Input: "Reply with exactly the word: pong",
		Text:  &Text{Verbosity: "low"},
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if res.Content == "" && len(res.ToolCalls) == 0 {
		return cases.FailRaw(res.Raw, "no output text")
	}
	return cases.Pass("text.verbosity accepted", res.Raw)
}
