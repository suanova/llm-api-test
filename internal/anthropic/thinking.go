package anthropic

import (
	"context"
	"encoding/json"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/runner"
)

// ThinkingCase verifies that the `thinking` parameter (extended thinking) is
// accepted. We request thinking with a budget_tokens. For models that support
// it, the response may include thinking content blocks; we assert only that the
// request is accepted (2xx with output), not that thinking blocks are present,
// since non-reasoning models or routers may legitimately ignore the field.
type ThinkingCase struct {
	Client *Client
}

func (*ThinkingCase) Name() string { return "messages-thinking" }
func (*ThinkingCase) Desc() string { return "POST /v1/messages accepts `thinking` (extended thinking)" }

func (tc *ThinkingCase) Run(ctx context.Context, model string) *runner.Result {
	thinking, _ := json.Marshal(map[string]any{
		"type":          "enabled",
		"budget_tokens": 128,
	})

	req := &Request{
		Model:     model,
		MaxTokens: 256,
		Thinking:  thinking,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What is 17 * 23? Think briefly and answer."`)},
		},
	}
	resp, err := tc.Client.CreateMessage(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with `thinking` failed: %v", err)
	}
	text := textContent(resp.Content)
	if text == "" {
		return cases.Fail(resp.Raw, "no text content block in response")
	}

	// Best-effort: report if thinking blocks were returned.
	detail := "thinking accepted"
	for _, block := range resp.Content {
		if block.Type == "thinking" {
			detail = "thinking accepted (thinking blocks present)"
			break
		}
	}
	return cases.Pass(detail, resp.Raw)
}
