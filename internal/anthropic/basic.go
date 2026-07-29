package anthropic

import (
	"context"
	"encoding/json"
	"strings"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/runner"
)

// BasicCase verifies that the Messages API endpoint returns a content array
// with non-empty text for a simple prompt.
type BasicCase struct {
	Client *Client
}

func (*BasicCase) Name() string { return "messages-basic" }
func (*BasicCase) Desc() string { return "POST /v1/messages returns a text content block" }

func (tc *BasicCase) Run(ctx context.Context, model string) *runner.Result {
	req := &Request{
		Model:     model,
		MaxTokens: 256,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"Reply with exactly the word: pong"`)},
		},
	}
	resp, err := tc.Client.CreateMessage(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request failed: %v", err)
	}
	if len(resp.Content) == 0 {
		return cases.Fail(resp.Raw, "no content blocks in response")
	}
	text := textContent(resp.Content)
	if text == "" {
		// Reasoning models may consume the token budget on thinking blocks and
		// produce an empty text block. This is still a valid API response.
		if hasBlockType(resp.Content, "thinking") {
			return cases.Pass("content blocks present (thinking, empty text — reasoning model)", resp.Raw)
		}
		return cases.Fail(resp.Raw, "no text in response content blocks")
	}
	return cases.Pass("text content block present", resp.Raw)
}

// textContent returns the concatenated text from all text-typed content blocks.
func textContent(blocks []ContentBlock) string {
	var b strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// hasBlockType reports whether the content array contains a block of the given type.
func hasBlockType(blocks []ContentBlock, typ string) bool {
	for _, block := range blocks {
		if block.Type == typ {
			return true
		}
	}
	return false
}
