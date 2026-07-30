package chat

import (
	"context"
	"fmt"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/runner"
)

// StreamCase verifies that the Chat Completions API supports streaming via
// stream=true. It sends a simple streaming request and checks that SSE
// content deltas are received and the accumulated content is non-empty.
type StreamCase struct {
	Client *Client
}

func (*StreamCase) Name() string { return "chat-stream" }
func (*StreamCase) Desc() string {
	return "POST /v1/chat/completions with stream=true returns SSE content deltas"
}

func (tc *StreamCase) Run(ctx context.Context, model string) *runner.Result {
	req := &Request{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: "Reply with exactly the word: pong"},
		},
	}

	result, err := tc.Client.StreamChatCompletion(ctx, req)
	if err != nil {
		return cases.Fail(nil, "streaming request failed: %v", err)
	}

	if result.ChunkCount == 0 {
		if result.Content == "" {
			return cases.Fail(result.Raw, "stream=true produced no SSE content deltas and no content")
		}
		return cases.Fail(result.Raw, "stream=true produced no SSE content deltas (server may not support streaming; content received via non-streaming fallback)")
	}

	return cases.Pass(fmt.Sprintf("SSE content deltas received, chunks=%d", result.ChunkCount), result.Raw)
}
