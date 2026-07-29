package chat

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/runner"
)

// SeedCase verifies that the `seed` request parameter is accepted. We send a
// seed and check for a 2xx response with output; we do not assert determinism,
// since seed support and its reliability vary by provider.
type SeedCase struct {
	Client *Client
}

func (*SeedCase) Name() string { return "chat-seed" }
func (*SeedCase) Desc() string { return "POST /v1/chat/completions accepts `seed`" }

func (tc *SeedCase) Run(ctx context.Context, model string) *runner.Result {
	req := &Request{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: "In one sentence, what is the Chat Completions API?"},
		},
	}
	req.SetExtra("seed", 42)

	resp, err := tc.Client.CreateChatCompletion(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with `seed` failed: %v", err)
	}
	if len(resp.Choices) == 0 {
		return cases.Fail(resp.Raw, "no choices in response")
	}
	if resp.Choices[0].Message.Content == "" {
		return cases.Fail(resp.Raw, "empty assistant message")
	}
	return cases.Pass("seed accepted", resp.Raw)
}
