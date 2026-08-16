package chat

import (
	"llm-api-test/internal/registry"
)

// Format returns the chat format descriptor.
func Format() registry.Format {
	return registry.Format{
		Name: "chat",
		Desc: "OpenAI Chat Completions (POST /v1/chat/completions)",
		Cases: func(p registry.Params) []registry.CompatCase {
			client := New(p.Config.BaseURL, p.Config.APIKey, p.Debug, p.Stream)
			return []registry.CompatCase{
				&BasicCase{client: client},
				&SystemMessageCase{client: client},
				&ResponseFormatCase{client: client},
				&SeedCase{client: client},
				&ToolCallCase{client: client},
			}
		},
		Benchmark: func(p registry.Params) registry.BenchmarkCase {
			return &BenchmarkCase{client: New(p.Config.BaseURL, p.Config.APIKey, p.Debug, p.Stream)}
		},
	}
}
