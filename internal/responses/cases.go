package responses

import (
	"llm-api-test/internal/registry"
)

// Format returns the responses format descriptor.
func Format() registry.Format {
	return registry.Format{
		Name: "responses",
		Desc: "OpenAI Responses (POST /responses)",
		Cases: func(p registry.Params) []registry.CompatCase {
			client := New(p.Config.BaseURL, p.Config.APIKey, p.Debug, p.Stream)
			return []registry.CompatCase{
				&BasicCase{client: client},
				&InstructionsCase{client: client},
				&ReasoningCase{client: client},
				&TextFormatCase{client: client},
				&VerbosityCase{client: client},
				&PromptCacheKeyCase{client: client},
				&ToolCallCase{client: client},
			}
		},
		Benchmark: func(p registry.Params) registry.BenchmarkCase {
			return &BenchmarkCase{client: New(p.Config.BaseURL, p.Config.APIKey, p.Debug, p.Stream)}
		},
	}
}
