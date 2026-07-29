package anthropic

import (
	"context"
	"encoding/json"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/runner"
)

// CacheControlCase verifies that the `cache_control` parameter on a system
// block is accepted. We send a system block with cache_control set to
// ephemeral, and assert the request succeeds (2xx with output). We don't
// assert cache-hit behavior since that is provider-specific and may require
// a second request.
type CacheControlCase struct {
	Client *Client
}

func (*CacheControlCase) Name() string { return "messages-cache-control" }
func (*CacheControlCase) Desc() string { return "POST /v1/messages accepts `cache_control` on system blocks" }

func (tc *CacheControlCase) Run(ctx context.Context, model string) *runner.Result {
	// System is a block array with cache_control.
	system, _ := json.Marshal([]map[string]any{
		{
			"type":          "text",
			"text":          "You are a test assistant.",
			"cache_control": map[string]any{"type": "ephemeral"},
		},
	})

	req := &Request{
		Model:     model,
		MaxTokens: 64,
		System:    system,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"Say: cached"`)},
		},
	}
	resp, err := tc.Client.CreateMessage(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with `cache_control` failed: %v", err)
	}
	text := textContent(resp.Content)
	if text == "" {
		return cases.Fail(resp.Raw, "no text content block in response")
	}
	return cases.Pass("cache_control accepted", resp.Raw)
}
