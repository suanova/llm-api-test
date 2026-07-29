package responses

import (
	"context"
	"encoding/json"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/openai"
	"llm-api-test/internal/runner"
)

// PromptCacheKeyCase verifies that the `prompt_cache_key` request parameter is
// accepted. We send it twice with the same key and check both succeed; the
// second call may report a cache hit in usage, but we only assert acceptance
// (no error), since cache-hit reporting is provider-specific.
type PromptCacheKeyCase struct {
	Client *openai.Client
}

func (*PromptCacheKeyCase) Name() string { return "responses-prompt-cache-key" }
func (*PromptCacheKeyCase) Desc() string { return "POST /responses accepts `prompt_cache_key`" }

func (tc *PromptCacheKeyCase) Run(ctx context.Context, model string) *runner.Result {
	const key = "llm-api-test:cache-key-probe"
	req := &openai.Request{
		Model: model,
		Input: mustInput("Say: cached"),
	}
	req.SetExtra("prompt_cache_key", key)

	// First call: primes the cache (or is simply accepted).
	resp1, err := tc.Client.CreateResponse(ctx, req)
	if err != nil {
		return cases.Fail(nil, "first request with `prompt_cache_key` failed: %v", err)
	}
	if outputText(resp1.Output) == "" {
		return cases.Fail(resp1.Raw, "no output_text in first response")
	}

	// Second call with the same key: should also succeed.
	resp2, err := tc.Client.CreateResponse(ctx, req)
	if err != nil {
		return cases.Fail(nil, "second request with `prompt_cache_key` failed: %v", err)
	}
	if outputText(resp2.Output) == "" {
		return cases.Fail(resp2.Raw, "no output_text in second response")
	}

	// Best-effort: report whether usage hinted at a cache hit.
	detail := "prompt_cache_key accepted (2 calls ok)"
	if hit := cacheHitHint(resp2.Usage); hit {
		detail = "prompt_cache_key accepted (2 calls ok, 2nd usage hints cache hit)"
	}
	return cases.Pass(detail, append(append(resp1.Raw, '\n'), resp2.Raw...))
}

// cacheHitHint looks for common cache-hit fields in usage. OpenAI reports
// prompt_tokens_details.cached_tokens; we accept any of a few shapes.
func cacheHitHint(usage json.RawMessage) bool {
	if len(usage) == 0 {
		return false
	}
	var u struct {
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	if err := json.Unmarshal(usage, &u); err != nil {
		return false
	}
	return u.PromptTokensDetails.CachedTokens > 0
}
