package responses

import (
	"context"
	"encoding/json"
	"fmt"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/openai"
	"llm-api-test/internal/runner"
)

// TextFormatCase verifies that `text.format` with type=json_schema is accepted
// and the model returns output_text that parses as JSON matching the schema.
//
// We ask for a person object {name, age} and assert the response output_text is
// valid JSON with a string `name` and a number `age`. With strict=true the API
// should guarantee schema conformance; we validate the two required fields
// rather than doing full JSON-Schema validation.
type TextFormatCase struct {
	Client *openai.Client
}

func (*TextFormatCase) Name() string { return "responses-text-format" }
func (*TextFormatCase) Desc() string {
	return "POST /responses accepts `text.format` (json_schema) and returns schema-conformant JSON"
}

func (tc *TextFormatCase) Run(ctx context.Context, model string) *runner.Result {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "number"},
		},
		"required":             []string{"name", "age"},
		"additionalProperties": false,
	})
	strict := true

	req := &openai.Request{
		Model: model,
		Input: mustInput("Tell me about Ada Lovelace. Respond with her name and birth year as age (year only)."),
		Text: &openai.Text{
			Format: &openai.Format{
				Type:   "json_schema",
				Name:   "person",
				Schema: schema,
				Strict: &strict,
			},
		},
	}

	resp, err := tc.Client.CreateResponse(ctx, req)
	if err != nil {
		return cases.Fail(nil, "request with `text.format=json_schema` failed: %v", err)
	}

	text := outputText(resp.Output)
	if text == "" {
		return cases.Fail(resp.Raw, "no output_text in response")
	}

	// The output_text must be valid JSON.
	var person struct {
		Name string `json:"name"`
		Age  any    `json:"age"`
	}
	if err := json.Unmarshal([]byte(text), &person); err != nil {
		shown := text
		if len(shown) > 200 {
			shown = shown[:200] + "..."
		}
		return cases.Fail(resp.Raw, "output_text is not valid JSON: %v (got: %s)", err, shown)
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

	return cases.Pass(fmt.Sprintf("output_text is schema-conformant JSON: %s", text), resp.Raw)
}
