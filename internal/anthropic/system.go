package anthropic

import (
	"context"
	"encoding/json"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/runner"
)

// SystemCase verifies that the top-level `system` parameter is accepted and
// steers behavior. We give an instruction that forces a fixed reply, then check
// the model follows it (best-effort behavioral check, not a hard wire-format
// assertion).
type SystemCase struct {
	Client *Client
}

func (*SystemCase) Name() string { return "messages-system" }
func (*SystemCase) Desc() string { return "POST /v1/messages accepts and follows a system prompt" }

func (tc *SystemCase) Run(ctx context.Context, model string) *runner.Result {
	req := &Request{
		Model:     model,
		MaxTokens: 64,
		System:    json.RawMessage(`"You are a parity responder. For any user input, reply with exactly: PARITY_OK"`),
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}
	resp, err := tc.Client.CreateMessage(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with `system` failed: %v", err)
	}
	text := textContent(resp.Content)
	if text == "" {
		if hasBlockType(resp.Content, "thinking") {
			return cases.Pass("system message accepted (reasoning model: thinking block, empty text)", resp.Raw)
		}
		return cases.Fail(resp.Raw, "no text content block in response")
	}
	// Behavioral check: the system message should be followed. We allow partial
	// match since the model may add punctuation/whitespace.
	if !cases.ContainsFold(text, "PARITY_OK") {
		return cases.Fail(resp.Raw, "system message ignored: expected 'PARITY_OK', got %q", text)
	}
	return cases.Pass("system message accepted and followed", resp.Raw)
}
