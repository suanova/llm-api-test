// Package chat defines compatibility cases for the OpenAI Chat Completions API
// (POST /v1/chat/completions). Each case holds a *chat.Client and is constructed
// via All, which wires them to a single shared client.
package chat

import (
	"llm-api-test/internal/runner"
)

// All returns the ordered list of Chat Completions test cases, all sharing the
// given client.
func All(c *Client) []runner.Case {
	return []runner.Case{
		&BasicCase{Client: c},
		&StreamCase{Client: c},
		&SystemCase{Client: c},
		&ToolCallCase{Client: c},
		&ResponseFormatCase{Client: c},
		&SeedCase{Client: c},
	}
}
