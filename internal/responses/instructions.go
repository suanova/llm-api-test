package responses

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/openai"
	"llm-api-test/internal/runner"
)

// InstructionsCase verifies that the `instructions` parameter is accepted and
// steers behavior. We give an instruction that forces a fixed reply, then check
// the model follows it (best-effort behavioral check, not a hard wire-format
// assertion).
type InstructionsCase struct {
	Client *openai.Client
}

func (*InstructionsCase) Name() string { return "responses-instructions" }
func (*InstructionsCase) Desc() string {
	return "POST /responses accepts `instructions` and follows them"
}

func (tc *InstructionsCase) Run(ctx context.Context, model string) *runner.Result {
	instr := "You are a parity responder. For any user input, reply with exactly: PARITY_OK"
	req := &openai.Request{
		Model:        model,
		Input:        mustInput("hello"),
		Instructions: &instr,
	}
	resp, err := tc.Client.CreateResponse(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with `instructions` failed: %v", err)
	}
	text := outputText(resp.Output)
	if text == "" {
		return cases.Fail(resp.Raw, "no output_text in response")
	}
	// Behavioral check: the instruction should be followed. We allow partial
	// match since the model may add punctuation/whitespace.
	if !cases.ContainsFold(text, "PARITY_OK") {
		return cases.Fail(resp.Raw, "instructions ignored: expected 'PARITY_OK', got %q", text)
	}
	return cases.Pass("instructions accepted and followed", resp.Raw)
}
