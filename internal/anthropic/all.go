// Package anthropic defines compatibility cases for the Anthropic Messages API
// (POST /v1/messages). Each case holds a *anthropic.Client and is constructed
// via All, which wires them to a single shared client.
package anthropic

import (
	"llm-api-test/internal/runner"
)

// All returns the ordered list of Anthropic Messages test cases, all sharing the
// given client.
func All(c *Client) []runner.Case {
	return []runner.Case{
		&BasicCase{Client: c},
		&SystemCase{Client: c},
		&ToolUseCase{Client: c},
		&CacheControlCase{Client: c},
		&ThinkingCase{Client: c},
	}
}
