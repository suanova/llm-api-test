package chat

import (
	"context"
	"time"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// CacheCase runs a simulated agent session against the chat API to observe
// prompt-cache behavior end-to-end. Automatic prefix caching needs no
// cache_control: the stable prefix (system message + tools + history) must
// simply match exactly, so the session content is byte-identical across
// turns (design.md "cache").
type CacheCase struct {
	client *Client
}

func (c *CacheCase) ID() string { return "chat:cache" }
func (c *CacheCase) Desc() string {
	return "POST /v1/chat/completions prompt-cache hit rate over a simulated session"
}

func (c *CacheCase) RunSession(ctx context.Context, model string, turns int) []registry.CacheTurn {
	history := make([]Message, 0, 1+2*turns+1)
	history = append(history, Message{Role: "system", Content: cases.CacheSystemPrompt})
	result := make([]registry.CacheTurn, 0, turns)
	for i := 0; i < turns && i < len(cases.CacheQuestions); i++ {
		history = append(history, Message{Role: "user", Content: cases.CacheQuestions[i]})

		maxTokens := 300
		req := &Request{
			Model:               model,
			Messages:            history,
			Tools:               cacheTools(),
			MaxCompletionTokens: &maxTokens,
		}
		start := time.Now()
		res, err := c.client.Send(ctx, req)
		turn := registry.CacheTurn{Turn: i + 1}
		if err != nil {
			turn.Err = err
			return append(result, turn) // session aborts: history is broken
		}
		turn.Total = time.Since(start)
		if res.Usage != nil {
			turn.PromptTokens = res.Usage.PromptTokens
			if res.Usage.PromptTokensDetails != nil {
				turn.Cached = res.Usage.PromptTokensDetails.CachedTokens
			} else if res.Usage.PromptCacheHitTokens > 0 {
				turn.Cached = res.Usage.PromptCacheHitTokens // DeepSeek-style
			}
		}
		result = append(result, turn)
		history = append(history, Message{Role: "assistant", Content: res.Content})
	}
	return result
}

// cacheTools wraps the shared tool schemas in the chat wire format.
func cacheTools() []Tool {
	tools := make([]Tool, len(cases.CacheTools))
	for i, tl := range cases.CacheTools {
		tools[i] = Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        tl.Name,
				Description: tl.Description,
				Parameters:  cases.MustJSON(tl.Schema),
			},
		}
	}
	return tools
}
