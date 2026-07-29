package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/runner"
)

// ResponseFormatCase verifies that response_format with json_schema is accepted
// and the model returns content that parses as JSON matching the schema.
//
// We ask for a person object {name, age} and assert the assistant message
// content is valid JSON with a string `name` and a number `age`. We validate
// the two required fields rather than doing full JSON-Schema validation.
type ResponseFormatCase struct {
	Client *Client
}

func (*ResponseFormatCase) Name() string { return "chat-response-format" }
func (*ResponseFormatCase) Desc() string {
	return "POST /v1/chat/completions accepts `response_format` (json_schema) and returns schema-conformant JSON"
}

func (tc *ResponseFormatCase) Run(ctx context.Context, model string) *runner.Result {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "number"},
		},
		"required":             []string{"name", "age"},
		"additionalProperties": false,
	})
	responseFormat := cases.MustJSON(map[string]any{
		"type":        "json_schema",
		"json_schema": map[string]any{"name": "person", "schema": schema, "strict": true},
	})

	req := &Request{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: "Tell me about Ada Lovelace. Respond with her name and birth year as age (year only)."},
		},
		ResponseFormat: responseFormat,
	}

	resp, err := tc.Client.CreateChatCompletion(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with `response_format=json_schema` failed: %v", err)
	}
	if len(resp.Choices) == 0 {
		return cases.Fail(resp.Raw, "no choices in response")
	}
	text := resp.Choices[0].Message.Content
	if text == "" {
		return cases.Fail(resp.Raw, "empty assistant message")
	}

	// The content must be valid JSON.
	var person struct {
		Name string `json:"name"`
		Age  any    `json:"age"`
	}
	if err := json.Unmarshal([]byte(text), &person); err != nil {
		shown := text
		if len(shown) > 200 {
			shown = shown[:200] + "..."
		}
		return cases.Fail(resp.Raw, "content is not valid JSON: %v (got: %s)", err, shown)
	}
	if person.Name == "" {
		return cases.Fail(resp.Raw, "JSON missing required string 'name' (got: %s)", text)
	}
	if person.Age == nil {
		return cases.Fail(resp.Raw, "JSON missing required field 'age' (got: %s)", text)
	}
	// age must be a number (JSON numbers decode to float64).
	if _, ok := person.Age.(float64); !ok {
		return cases.Fail(resp.Raw, "'age' is not a number (got %T: %v)", person.Age, person.Age)
	}

	return cases.Pass(fmt.Sprintf("content is schema-conformant JSON: %s", text), resp.Raw)
}
