package anthropic

import (
	"context"
	"encoding/json"
	"fmt"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/runner"
)

// ToolUseCase verifies that the endpoint supports custom function tools: it
// defines a `get_weather` tool and a prompt that should trigger a tool_use
// content block. We assert that at least one block has type=="tool_use" with
// name=="get_weather" and a parseable JSON input object containing the
// location we asked about.
type ToolUseCase struct {
	Client *Client
}

func (*ToolUseCase) Name() string { return "messages-tool-use" }
func (*ToolUseCase) Desc() string {
	return "POST /v1/messages supports custom function tools (tool_use content block)"
}

func (tc *ToolUseCase) Run(ctx context.Context, model string) *runner.Result {
	inputSchema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{"type": "string"},
		},
		"required": []string{"location"},
	})

	req := &Request{
		Model:     model,
		MaxTokens: 256,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What is the weather in Tokyo? Use the get_weather tool."`)},
		},
		Tools: []Tool{
			{
				Name:        "get_weather",
				Description: "Get the current weather for a city",
				InputSchema: inputSchema,
			},
		},
		// Force the tool so the model calls it rather than answering from
		// parametric knowledge. tool_choice is an object: {"type":"tool","name":...}.
		ToolChoice: cases.MustJSON(map[string]any{"type": "tool", "name": "get_weather"}),
	}

	resp, err := tc.Client.CreateMessage(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with tool failed: %v", err)
	}

	// Find a tool_use content block.
	var block *ContentBlock
	for i := range resp.Content {
		if resp.Content[i].Type == "tool_use" {
			block = &resp.Content[i]
			break
		}
	}
	if block == nil {
		types := make([]string, len(resp.Content))
		for i, b := range resp.Content {
			types[i] = b.Type
		}
		return cases.Fail(resp.Raw, "no tool_use block in response (blocks: %v, stop_reason=%q)", types, resp.StopReason)
	}
	if block.Name != "get_weather" {
		return cases.Fail(resp.Raw, "tool_use name=%q, want get_weather", block.Name)
	}

	// Input should be a JSON object with location containing "Tokyo".
	var input map[string]any
	if err := json.Unmarshal(block.Input, &input); err != nil {
		return cases.Fail(resp.Raw, "tool_use input not valid JSON: %v (raw: %s)", err, block.Input)
	}
	loc, _ := input["location"].(string)
	if loc == "" {
		return cases.Fail(resp.Raw, "tool_use input missing 'location' (raw: %s)", block.Input)
	}
	if !cases.ContainsFold(loc, "Tokyo") {
		return cases.Fail(resp.Raw, "tool_use location=%q, want it to contain 'Tokyo'", loc)
	}
	return cases.Pass(fmt.Sprintf("tool_use to %s with input=%s", block.Name, block.Input), resp.Raw)
}
