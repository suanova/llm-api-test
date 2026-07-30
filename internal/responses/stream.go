package responses

import (
	"context"
	"fmt"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/openai"
	"llm-api-test/internal/runner"
)

// StreamCase verifies that the Responses API supports streaming via
// stream=true. It sends a simple streaming request and checks that SSE
// content deltas are received and the accumulated content is non-empty.
type StreamCase struct {
	Client *openai.Client
}

func (*StreamCase) Name() string { return "responses-stream" }
func (*StreamCase) Desc() string {
	return "POST /responses with stream=true returns SSE content deltas"
}

func (tc *StreamCase) Run(ctx context.Context, model string) *runner.Result {
	req := &openai.Request{
		Model: model,
		Input: mustInput("Reply with exactly the word: pong"),
	}

	result, err := tc.Client.StreamResponse(ctx, req)
	if err != nil {
		return cases.Fail(nil, "streaming request failed: %v", err)
	}

	if result.ChunkCount == 0 {
		// The server may have returned a non-streaming response (ignoring
		// stream=true). Check if we got any content at all.
		if result.Text == "" {
			return cases.Fail(result.Raw, "stream=true produced no SSE content deltas and no content")
		}
		return cases.Fail(result.Raw, "stream=true produced no SSE content deltas (server may not support streaming; content received as non-streaming fallback)")
	}

	return cases.Pass(fmt.Sprintf("SSE content deltas received, chunks=%d", result.ChunkCount), result.Raw)
}
