package chat

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/runner"
)

// BasicCase verifies that the Chat Completions endpoint returns a non-empty
// assistant message for a simple prompt.
type BasicCase struct {
	Client *Client
}

func (*BasicCase) Name() string { return "chat-basic" }
func (*BasicCase) Desc() string { return "POST /v1/chat/completions returns an assistant message" }

func (tc *BasicCase) Run(ctx context.Context, model string) *runner.Result {
	req := &Request{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: "Reply with exactly the word: pong"},
		},
	}
	resp, err := tc.Client.CreateChatCompletion(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request failed: %v", err)
	}
	if len(resp.Choices) == 0 {
		return cases.Fail(resp.Raw, "no choices in response")
	}
	text := resp.Choices[0].Message.Content
	if text == "" {
		return cases.Fail(resp.Raw, "empty assistant message (choices: %d)", len(resp.Choices))
	}
	return cases.Pass("assistant message present", resp.Raw)
}
