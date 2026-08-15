package responses

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// ToolCallCase verifies that the endpoint supports custom function tools and
// returns function calls in the response.
type ToolCallCase struct {
	client *Client
}

func (c *ToolCallCase) ID() string   { return "responses:tool-call" }
func (c *ToolCallCase) Name() string { return "tool-call" }
func (c *ToolCallCase) Desc() string {
	return "POST /responses supports custom function tools (function_call output)"
}

func (c *ToolCallCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model: model,
		Input: "What is the weather in Shanghai?",
		Tools: []Tool{{
			Type:        "function",
			Name:        "get_weather",
			Description: "Get the weather for a location",
			Parameters: cases.MustJSON(map[string]any{
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
		return cases.FailRaw(res.Raw, "no function_call in response")
	}
	if res.ToolCalls[0].Name != "get_weather" {
		return cases.FailRaw(res.Raw, "unexpected function call %q", res.ToolCalls[0].Name)
	}
	return cases.Pass("function_call returned", res.Raw)
}
