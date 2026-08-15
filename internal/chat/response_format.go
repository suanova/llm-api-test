package chat

import (
	"context"
	"encoding/json"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// ResponseFormatCase verifies that the endpoint accepts response_format
// (json_object) and returns JSON.
type ResponseFormatCase struct {
	client *Client
}

func (c *ResponseFormatCase) ID() string   { return "chat:response_format" }
func (c *ResponseFormatCase) Name() string { return "response_format" }
func (c *ResponseFormatCase) Desc() string {
	return "POST /v1/chat/completions accepts response_format (json_object) and returns JSON"
}

// responseFormatPrompt must ask for JSON: with json_object mode the provider
// (e.g. DeepSeek) requires the word "json" in the prompt, otherwise the model
// may emit whitespace until the token limit.
const responseFormatPrompt = `Who are you? Reply in JSON format with a "name" field, e.g. {"name": "..."}`

func (c *ResponseFormatCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model:    model,
		Messages: []Message{{Role: "user", Content: responseFormatPrompt}},
		ResponseFormat: cases.MustJSON(map[string]any{
			"type": "json_object",
		}),
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	var v struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(res.Content), &v); err != nil {
		return cases.FailRaw(res.Raw, "response is not valid JSON: %v", err)
	}
	if v.Name == "" {
		return cases.FailRaw(res.Raw, "response JSON misses required field 'name'")
	}
	return cases.Pass("JSON response", res.Raw)
}
