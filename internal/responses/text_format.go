package responses

import (
	"context"
	"encoding/json"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// TextFormatCase verifies that the endpoint accepts text.format (json_schema)
// and returns schema-conformant JSON.
type TextFormatCase struct {
	client *Client
}

func (c *TextFormatCase) ID() string   { return "responses:text.format" }
func (c *TextFormatCase) Name() string { return "text.format" }
func (c *TextFormatCase) Desc() string {
	return "POST /responses accepts `text.format` (json_schema) and returns schema-conformant JSON"
}

// personSchema is the JSON schema the response must conform to.
var personSchema = cases.MustJSON(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name": map[string]any{"type": "string"},
	},
	"required": []string{"name"},
})

func (c *TextFormatCase) Run(ctx context.Context, model string) *registry.CompatResult {
	req := &Request{
		Model: model,
		Input: "Who are you?",
		Text: &Text{
			Format: cases.MustJSON(map[string]any{
				"type":   "json_schema",
				"name":   "person",
				"schema": personSchema,
				"strict": true,
			}),
		},
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	var v struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(res.Content), &v); err != nil {
		return cases.FailRaw(res.Raw, "response is not schema-conformant JSON: %v", err)
	}
	if v.Name == "" {
		return cases.FailRaw(res.Raw, "response JSON misses required field 'name'")
	}
	return cases.Pass("schema-conformant JSON", res.Raw)
}
