package chat

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// ToolCallCase verifies that the endpoint supports custom function tools and
// returns tool_calls in the response.
type ToolCallCase struct {
	client *Client
}

func (c *ToolCallCase) ID() string   { return "chat:tool-call" }
func (c *ToolCallCase) Name() string { return "tool-call" }
func (c *ToolCallCase) Desc() string {
	return "POST /v1/chat/completions supports custom function tools (tool_calls output)"
}

func (c *ToolCallCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model:    model,
		Messages: []Message{{Role: "user", Content: "What is the weather in Shanghai?"}},
		Tools: []Tool{{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_weather",
				Description: "Get the weather for a location",
				Parameters: cases.MustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{"type": "string"},
					},
					"required": []string{"location"},
				}),
			},
		}},
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if len(res.ToolCalls) == 0 {
		return cases.FailRaw(res.Raw, "no tool_calls in response")
	}
	if res.ToolCalls[0].Function.Name != "get_weather" {
		return cases.FailRaw(res.Raw, "unexpected tool call %q", res.ToolCalls[0].Function.Name)
	}
	return cases.Pass("tool_calls returned", res.Raw)
}
