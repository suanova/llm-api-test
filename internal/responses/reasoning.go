package responses

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// ReasoningCase verifies that the endpoint accepts reasoning.effort and
// reasoning.summary.
type ReasoningCase struct {
	client *Client
}

func (c *ReasoningCase) ID() string   { return "responses:reasoning" }
func (c *ReasoningCase) Name() string { return "reasoning" }
func (c *ReasoningCase) Desc() string {
	return "POST /responses accepts `reasoning.effort` and `reasoning.summary`"
}

func (c *ReasoningCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model: model,
		Input: "Reply with exactly the word: pong",
		Reasoning: &Reasoning{
			Effort:  "high",
			Summary: "concise",
		},
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if res.Content == "" && len(res.ToolCalls) == 0 {
		return cases.FailRaw(res.Raw, "no output text")
	}
	return cases.Pass("reasoning accepted", res.Raw)
}
