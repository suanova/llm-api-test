package chat

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/runner"
)

// SystemCase verifies that a system message is accepted and steers behavior. We
// give an instruction that forces a fixed reply, then check the model follows
// it (best-effort behavioral check, not a hard wire-format assertion).
type SystemCase struct {
	Client *Client
}

func (*SystemCase) Name() string { return "chat-system-message" }
func (*SystemCase) Desc() string { return "POST /v1/chat/completions accepts and follows a system message" }

func (tc *SystemCase) Run(ctx context.Context, model string) *runner.Result {
	req := &Request{
		Model: model,
		Messages: []Message{
			{Role: "system", Content: "You are a parity responder. For any user input, reply with exactly: PARITY_OK"},
			{Role: "user", Content: "hello"},
		},
	}
	resp, err := tc.Client.CreateChatCompletion(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with system message failed: %v", err)
	}
	if len(resp.Choices) == 0 {
		return cases.Fail(resp.Raw, "no choices in response")
	}
	text := resp.Choices[0].Message.Content
	if text == "" {
		return cases.Fail(resp.Raw, "empty assistant message")
	}
	// Behavioral check: the system message should be followed. We allow partial
	// match since the model may add punctuation/whitespace.
	if !cases.ContainsFold(text, "PARITY_OK") {
		return cases.Fail(resp.Raw, "system message ignored: expected 'PARITY_OK', got %q", text)
	}
	return cases.Pass("system message accepted and followed", resp.Raw)
}
