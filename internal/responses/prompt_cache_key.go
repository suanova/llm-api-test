package responses

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// PromptCacheKeyCase verifies that the endpoint accepts prompt_cache_key.
type PromptCacheKeyCase struct {
	client *Client
}

func (c *PromptCacheKeyCase) ID() string   { return "responses:prompt_cache_key" }
func (c *PromptCacheKeyCase) Name() string { return "prompt_cache_key" }
func (c *PromptCacheKeyCase) Desc() string { return "POST /responses accepts `prompt_cache_key`" }

func (c *PromptCacheKeyCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model:          model,
		Input:          "Reply with exactly the word: pong",
		PromptCacheKey: "test-cache-key",
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if res.Content == "" && len(res.ToolCalls) == 0 {
		return cases.FailRaw(res.Raw, "no output text")
	}
	return cases.Pass("prompt_cache_key accepted", res.Raw)
}
