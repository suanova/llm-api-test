package messages

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// CacheControlCase verifies that the endpoint accepts cache_control on system
// blocks.
type CacheControlCase struct {
	client *Client
}

func (c *CacheControlCase) ID() string   { return "messages:cache_control" }
func (c *CacheControlCase) Name() string { return "cache_control" }
func (c *CacheControlCase) Desc() string {
	return "POST /v1/messages accepts `cache_control` on system blocks"
}

func (c *CacheControlCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model:     model,
		MaxTokens: 1024,
		System: []SystemBlock{{
			Type:         "text",
			Text:         "You are a helpful assistant.",
			CacheControl: &CacheControl{Type: "ephemeral"},
		}},
		Messages: []Message{{Role: "user", Content: "Reply with exactly the word: pong"}},
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if res.Content == "" && len(res.ToolCalls) == 0 {
		return cases.FailRaw(res.Raw, "no text content block")
	}
	return cases.Pass("cache_control accepted", res.Raw)
}
