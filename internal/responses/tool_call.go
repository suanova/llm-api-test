package responses

import (
	"context"
	"encoding/json"
	"fmt"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/openai"
	"llm-api-test/internal/runner"
)

// ToolCallCase verifies that the endpoint supports custom function tools: it
// defines a `get_weather` function tool and a prompt that should trigger a
// function_call output item. We assert that at least one output item has
// type=="function_call" with a parseable `arguments` JSON object containing
// the location we asked about.
type ToolCallCase struct {
	Client *openai.Client
}

func (*ToolCallCase) Name() string { return "responses-tool-call" }
func (*ToolCallCase) Desc() string {
	return "POST /responses supports custom function tools (function_call output)"
}

func (tc *ToolCallCase) Run(ctx context.Context, model string) *runner.Result {
	params, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{"type": "string"},
		},
		"required": []string{"location"},
	})

	req := &openai.Request{
		Model: model,
		Input: mustInput("What's the weather in Tokyo? Use the get_weather tool."),
		Tools: []openai.Tool{
			{
				Type:        "function",
				Name:        "get_weather",
				Description: "Get the current weather for a city",
				Parameters:  params,
			},
		},
		// Force the tool so the model calls it rather than answering from
		// parametric knowledge. tool_choice can be a string ("auto"|"required")
		// or an object; we use the object form {"type":"function","name":...}.
		ToolChoice: cases.MustJSON(map[string]any{"type": "function", "name": "get_weather"}),
	}

	resp, err := tc.Client.CreateResponse(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with function tool failed: %v", err)
	}

	// Find a function_call item.
	var fc *openai.OutputItem
	for i := range resp.Output {
		if resp.Output[i].Type == "function_call" {
			fc = &resp.Output[i]
			break
		}
	}
	if fc == nil {
		return cases.Fail(resp.Raw, "no function_call item in output (items: %s)", describeTypes(resp.Output))
	}
	if fc.Name != "get_weather" {
		return cases.Fail(resp.Raw, "function_call name=%q, want get_weather", fc.Name)
	}

	// Arguments should be a JSON object with location containing "Tokyo".
	var args map[string]any
	if err := json.Unmarshal([]byte(fc.Arguments), &args); err != nil {
		return cases.Fail(resp.Raw, "function_call arguments not valid JSON: %v (raw: %s)", err, fc.Arguments)
	}
	loc, _ := args["location"].(string)
	if loc == "" {
		return cases.Fail(resp.Raw, "function_call arguments missing 'location' (raw: %s)", fc.Arguments)
	}
	if !cases.ContainsFold(loc, "Tokyo") {
		return cases.Fail(resp.Raw, "function_call location=%q, want it to contain 'Tokyo'", loc)
	}
	return cases.Pass(fmt.Sprintf("function_call to %s with args=%s", fc.Name, fc.Arguments), resp.Raw)
}

func describeTypes(items []openai.OutputItem) string {
	types := make([]string, len(items))
	for i, it := range items {
		types[i] = it.Type
	}
	return fmt.Sprintf("%v", types)
}
