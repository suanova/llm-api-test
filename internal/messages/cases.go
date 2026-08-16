package messages

import (
	"llm-api-test/internal/registry"
)

// Format returns the messages format descriptor.
func Format() registry.Format {
	return registry.Format{
		Name: "messages",
		Desc: "Anthropic Messages (POST /v1/messages)",
		Cases: func(p registry.Params) []registry.CompatCase {
			client := New(p.Config.BaseURL, p.Config.APIKey, p.Debug, p.Stream)
			return []registry.CompatCase{
				&BasicCase{client: client},
				&SystemCase{client: client},
				&ThinkingCase{client: client},
				&CacheControlCase{client: client},
				&ToolUseCase{client: client},
			}
		},
		Benchmark: func(p registry.Params) registry.BenchmarkCase {
			return &BenchmarkCase{client: New(p.Config.BaseURL, p.Config.APIKey, p.Debug, p.Stream)}
		},
	}
}
