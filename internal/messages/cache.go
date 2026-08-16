package messages

import (
	"context"
	"time"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// CacheCase runs a simulated agent session against the messages API to
// observe prompt-cache behavior end-to-end. The request mirrors Claude Code's
// cache layout: ephemeral breakpoints on the system block, the last tool
// definition, and the last history message (design.md "cache").
type CacheCase struct {
	client *Client
}

func (c *CacheCase) ID() string { return "messages:cache" }
func (c *CacheCase) Desc() string {
	return "POST /v1/messages prompt-cache hit rate over a simulated session"
}

func (c *CacheCase) RunSession(ctx context.Context, model string, turns int) []registry.CacheTurn {
	history := make([]Message, 0, 2*turns+1)
	result := make([]registry.CacheTurn, 0, turns)
	for i := 0; i < turns && i < len(cases.CacheQuestions); i++ {
		history = append(history, Message{Role: "user", Content: cases.CacheQuestions[i]})
		history[len(history)-1].CacheControl = &CacheControl{Type: "ephemeral"}

		req := &Request{
			Model:     model,
			MaxTokens: 300,
			System: []SystemBlock{{
				Type:         "text",
				Text:         cases.CacheSystemPrompt,
				CacheControl: &CacheControl{Type: "ephemeral"},
			}},
			Tools:    cacheTools(),
			Messages: history,
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
			turn.PromptTokens = res.Usage.InputTokens
			turn.Cached = res.Usage.CacheReadInputTokens
			turn.CacheWrite = res.Usage.CacheCreationInputTokens
		}
		result = append(result, turn)

		// The breakpoint was consumed by this request; clear it so the next
		// turn can position a single breakpoint on the newest message.
		history[len(history)-1].CacheControl = nil
		history = append(history, Message{Role: "assistant", Content: res.Content})
	}
	return result
}

// cacheTools wraps the shared tool schemas in the messages wire format. The
// last tool carries the cache breakpoint covering [system + tools].
func cacheTools() []Tool {
	tools := make([]Tool, len(cases.CacheTools))
	for i, tl := range cases.CacheTools {
		tools[i] = Tool{
			Name:        tl.Name,
			Description: tl.Description,
			InputSchema: cases.MustJSON(tl.Schema),
		}
	}
	tools[len(tools)-1].CacheControl = &CacheControl{Type: "ephemeral"}
	return tools
}
