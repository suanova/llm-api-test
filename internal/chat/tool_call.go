package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/runner"
)

// ToolCallCase verifies that the endpoint supports custom function tools: it
// defines a `get_weather` function tool and a prompt that should trigger a
// tool_calls entry in the assistant message. We assert that at least one
// tool_call has function.name=="get_weather" with a parseable JSON arguments
// object containing the location we asked about.
type ToolCallCase struct {
	Client *Client
}

func (*ToolCallCase) Name() string { return "chat-tool-call" }
func (*ToolCallCase) Desc() string {
	return "POST /v1/chat/completions supports custom function tools (tool_calls output)"
}

func (tc *ToolCallCase) Run(ctx context.Context, model string) *runner.Result {
	fnDef, _ := json.Marshal(map[string]any{
		"name":        "get_weather",
		"description": "Get the current weather for a city",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string"},
			},
			"required": []string{"location"},
		},
	})

	req := &Request{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: "What's the weather in Tokyo? Use the get_weather tool."},
		},
		Tools: []Tool{
			{Type: "function", Function: fnDef},
		},
		// Force the tool so the model calls it rather than answering from
		// parametric knowledge. tool_choice can be a string ("auto"|"required")
		// or an object; we use the object form {"type":"function","name":...}.
		ToolChoice: cases.MustJSON(map[string]any{"type": "function", "function": map[string]any{"name": "get_weather"}}),
	}

	resp, err := tc.Client.CreateChatCompletion(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with function tool failed: %v", err)
	}
	if len(resp.Choices) == 0 {
		return cases.Fail(resp.Raw, "no choices in response")
	}

	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) == 0 {
		return cases.Fail(resp.Raw, "no tool_calls in assistant message (finish_reason=%q)", resp.Choices[0].FinishReason)
	}

	// Find a get_weather tool call.
	var tc2 *ToolCall
	for i := range msg.ToolCalls {
		if msg.ToolCalls[i].Function.Name == "get_weather" {
			tc2 = &msg.ToolCalls[i]
			break
		}
	}
	if tc2 == nil {
		names := make([]string, len(msg.ToolCalls))
		for i, c := range msg.ToolCalls {
			names[i] = c.Function.Name
		}
		return cases.Fail(resp.Raw, "no tool_call to get_weather (got: %v)", names)
	}

	// Arguments should be a JSON object with location containing "Tokyo".
	var args map[string]any
	if err := json.Unmarshal([]byte(tc2.Function.Arguments), &args); err != nil {
		return cases.Fail(resp.Raw, "tool_call arguments not valid JSON: %v (raw: %s)", err, tc2.Function.Arguments)
	}
	loc, _ := args["location"].(string)
	if loc == "" {
		return cases.Fail(resp.Raw, "tool_call arguments missing 'location' (raw: %s)", tc2.Function.Arguments)
	}
	if !cases.ContainsFold(loc, "Tokyo") {
		return cases.Fail(resp.Raw, "tool_call location=%q, want it to contain 'Tokyo'", loc)
	}
	return cases.Pass(fmt.Sprintf("tool_call to %s with args=%s", tc2.Function.Name, tc2.Function.Arguments), resp.Raw)
}
