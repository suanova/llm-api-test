package responses

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/openai"
	"llm-api-test/internal/runner"
)

// VerbosityCase verifies that the `text.verbosity` parameter is accepted by
// sending a request with verbosity=low and checking for a 2xx response with
// output text. (We don't assert length, since verbosity semantics vary.)
type VerbosityCase struct {
	Client *openai.Client
}

func (*VerbosityCase) Name() string { return "responses-verbosity" }
func (*VerbosityCase) Desc() string { return "POST /responses accepts `text.verbosity`" }

func (tc *VerbosityCase) Run(ctx context.Context, model string) *runner.Result {
	req := &openai.Request{
		Model: model,
		Input: mustInput("In one sentence, what is the Responses API?"),
		Text:  &openai.Text{Verbosity: "low"},
	}
	resp, err := tc.Client.CreateResponse(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with `text.verbosity` failed: %v", err)
	}
	if outputText(resp.Output) == "" {
		return cases.Fail(resp.Raw, "no output_text in response")
	}
	return cases.Pass("text.verbosity accepted", resp.Raw)
}
