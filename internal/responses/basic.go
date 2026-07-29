package responses

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/openai"
	"llm-api-test/internal/runner"
)

// BasicCase verifies that the Responses API endpoint returns an output message
// with non-empty text for a simple prompt.
type BasicCase struct {
	Client *openai.Client
}

func (*BasicCase) Name() string { return "responses-basic" }
func (*BasicCase) Desc() string { return "POST /responses returns output text for a simple prompt" }

func (tc *BasicCase) Run(ctx context.Context, model string) *runner.Result {
	req := &openai.Request{
		Model: model,
		Input: mustInput("Reply with exactly the word: pong"),
	}
	resp, err := tc.Client.CreateResponse(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request failed: %v", err)
	}
	text := outputText(resp.Output)
	if text == "" {
		return cases.Fail(resp.Raw, "no output_text in response output (output items: %d)", len(resp.Output))
	}
	return cases.Pass("output_text present", resp.Raw)
}
