package responses

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/openai"
	"llm-api-test/internal/runner"
)

// ReasoningCase verifies that the `reasoning` parameter (effort + summary) is
// accepted. We request effort=low and summary=auto. For models that support
// reasoning, the response may include reasoning items; we assert only that the
// request is accepted (2xx with output), not that reasoning items are present,
// since non-reasoning models legitimately ignore the field.
type ReasoningCase struct {
	Client *openai.Client
}

func (*ReasoningCase) Name() string { return "responses-reasoning" }
func (*ReasoningCase) Desc() string {
	return "POST /responses accepts `reasoning.effort` and `reasoning.summary`"
}

func (tc *ReasoningCase) Run(ctx context.Context, model string) *runner.Result {
	req := &openai.Request{
		Model: model,
		Input: mustInput("What is 17 * 23? Think briefly and answer."),
		Reasoning: &openai.Reasoning{
			Effort:  "low",
			Summary: "auto",
		},
	}
	resp, err := tc.Client.CreateResponse(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with `reasoning` failed: %v", err)
	}
	if outputText(resp.Output) == "" {
		return cases.Fail(resp.Raw, "no output_text in response")
	}

	// Best-effort: report if reasoning items were returned.
	detail := "reasoning.effort/summary accepted"
	if hasItemType(resp.Output, "reasoning") {
		detail = "reasoning.effort/summary accepted (reasoning items present)"
	}
	return cases.Pass(detail, resp.Raw)
}
