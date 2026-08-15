package messages

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// ToolUseCase verifies that the endpoint supports custom function tools and
// returns tool_use content blocks.
type ToolUseCase struct {
	client *Client
}

func (c *ToolUseCase) ID() string   { return "messages:tool-use" }
func (c *ToolUseCase) Name() string { return "tool-use" }
func (c *ToolUseCase) Desc() string {
	return "POST /v1/messages supports custom function tools (tool_use content block)"
}

func (c *ToolUseCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model:     model,
		MaxTokens: 1024,
		Messages:  []Message{{Role: "user", Content: "What is the weather in Shanghai?"}},
		Tools: []Tool{{
			Name:        "get_weather",
			Description: "Get the weather for a location",
			InputSchema: cases.MustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{"type": "string"},
				},
				"required": []string{"location"},
			}),
		}},
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if len(res.ToolCalls) == 0 {
		return cases.FailRaw(res.Raw, "no tool_use content block")
	}
	if res.ToolCalls[0].Name != "get_weather" {
		return cases.FailRaw(res.Raw, "unexpected tool use %q", res.ToolCalls[0].Name)
	}
	return cases.Pass("tool_use returned", res.Raw)
}
