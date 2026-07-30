// Package responses defines compatibility cases for the OpenAI Responses API
// (POST /responses). Each case holds an *openai.Client and is constructed via
// All, which wires them to a single shared client.
package responses

import (
	"encoding/json"
	"strings"

	"llm-api-test/internal/openai"
	"llm-api-test/internal/runner"
)

// All returns the ordered list of Responses-API test cases, all sharing the
// given client.
func All(c *openai.Client) []runner.Case {
	return []runner.Case{
		&BasicCase{Client: c},
		&StreamCase{Client: c},
		&InstructionsCase{Client: c},
		&VerbosityCase{Client: c},
		&TextFormatCase{Client: c},
		&PromptCacheKeyCase{Client: c},
		&ReasoningCase{Client: c},
		&ToolCallCase{Client: c},
	}
}

// mustInput wraps a plain string prompt as a JSON-encoded "input" value.
func mustInput(s string) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
}

// outputText concatenates all message.text content items from the response
// output array. Returns "" if none found.
func outputText(out []openai.OutputItem) string {
	var b strings.Builder
	for _, item := range out {
		if item.Type != "message" || len(item.Content) == 0 {
			continue
		}
		var contents []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(item.Content, &contents); err != nil {
			continue
		}
		for _, c := range contents {
			if c.Type == "output_text" || c.Type == "text" {
				b.WriteString(c.Text)
			}
		}
	}
	return b.String()
}

// hasItemType reports whether the output array contains an item of the given type.
func hasItemType(out []openai.OutputItem, typ string) bool {
	for _, item := range out {
		if item.Type == typ {
			return true
		}
	}
	return false
}
