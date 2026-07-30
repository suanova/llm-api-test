// Package apis registers the API surfaces the CLI can test (Responses, Chat
// Completions, ...). Each surface knows its display name and can build a case
// set wired to a client, and list its case names+descriptions. This lets the
// CLI build subcommands and run cases without importing each surface's package
// directly.
package apis

import (
	"io"

	"llm-api-test/internal/anthropic"
	"llm-api-test/internal/chat"
	"llm-api-test/internal/openai"
	"llm-api-test/internal/responses"
	"llm-api-test/internal/runner"
)

// Surface is one testable API (e.g. OpenAI Responses, Chat Completions).
type Surface struct {
	// Name is the identifier used in --api and subcommand grouping, e.g.
	// "responses" or "chat".
	Name string
	// Desc is a short human-readable label.
	Desc string
	// BenchmarkDesc is the label used in benchmark mode.
	BenchmarkDesc string
	// build returns the compatibility case set for this surface, all sharing one client.
	build func(baseURL, apiKey string, debug io.Writer) []runner.Case
	// benchmarkBuild returns the benchmark case set for this surface. The prompt
	// parameter selects the benchmark prompt style ("pong" or "long").
	benchmarkBuild func(baseURL, apiKey string, debug io.Writer, prompt string) []runner.BenchmarkCase
}

// All is the ordered list of registered API surfaces.
var All = []Surface{
	{
		Name:          "responses",
		Desc:          "OpenAI Responses API (POST /responses)",
		BenchmarkDesc: "OpenAI Responses API (POST /responses) Benchmark",
		build: func(baseURL, apiKey string, debug io.Writer) []runner.Case {
			c := openai.New(baseURL, apiKey)
			c.DebugWriter = debug
			return responses.All(c)
		},
		benchmarkBuild: func(baseURL, apiKey string, debug io.Writer, prompt string) []runner.BenchmarkCase {
			c := openai.New(baseURL, apiKey)
			c.DebugWriter = debug
			return []runner.BenchmarkCase{&responses.BenchmarkBasicCase{Client: c, Prompt: prompt}}
		},
	},
	{
		Name:          "chat",
		Desc:          "OpenAI Chat Completions API (POST /v1/chat/completions)",
		BenchmarkDesc: "OpenAI Chat Completions API (POST /v1/chat/completions) Benchmark",
		build: func(baseURL, apiKey string, debug io.Writer) []runner.Case {
			c := chat.New(baseURL, apiKey)
			c.DebugWriter = debug
			return chat.All(c)
		},
		benchmarkBuild: func(baseURL, apiKey string, debug io.Writer, prompt string) []runner.BenchmarkCase {
			c := chat.New(baseURL, apiKey)
			c.DebugWriter = debug
			return []runner.BenchmarkCase{&chat.BenchmarkBasicCase{Client: c, Prompt: prompt}}
		},
	},
	{
		Name:          "messages",
		Desc:          "Anthropic Messages API (POST /v1/messages)",
		BenchmarkDesc: "Anthropic Messages API (POST /v1/messages) Benchmark",
		build: func(baseURL, apiKey string, debug io.Writer) []runner.Case {
			c := anthropic.New(baseURL, apiKey)
			c.DebugWriter = debug
			return anthropic.All(c)
		},
		benchmarkBuild: func(baseURL, apiKey string, debug io.Writer, prompt string) []runner.BenchmarkCase {
			c := anthropic.New(baseURL, apiKey)
			c.DebugWriter = debug
			return []runner.BenchmarkCase{&anthropic.BenchmarkBasicCase{Client: c, Prompt: prompt}}
		},
	},
}

// Find returns the surface with the given name, or nil.
func Find(name string) *Surface {
	for i := range All {
		if All[i].Name == name {
			return &All[i]
		}
	}
	return nil
}

// CaseInfo is a case's name and description, for listing/subcommand building.
type CaseInfo struct {
	Name string
	Desc string
}

// CaseMeta returns the name+desc of each case in this surface, without needing
// a real client (Name/Desc don't touch the client; build is called with empty
// endpoint values purely to materialize the case set).
func (s *Surface) CaseMeta() []CaseInfo {
	cases := s.build("", "", nil)
	out := make([]CaseInfo, len(cases))
	for i, c := range cases {
		out[i] = CaseInfo{Name: c.Name(), Desc: c.Desc()}
	}
	return out
}

// BenchmarkCaseMeta returns the name+desc of each benchmark case in this surface.
func (s *Surface) BenchmarkCaseMeta() []CaseInfo {
	cases := s.benchmarkBuild("", "", nil, "")
	out := make([]CaseInfo, len(cases))
	for i, c := range cases {
		out[i] = CaseInfo{Name: c.Name(), Desc: c.Desc()}
	}
	return out
}

// Build returns the compatibility case set for this surface, wired to a client targeting the
// given endpoint. If debug is non-nil, HTTP request/response dumps are written
// there.
func (s *Surface) Build(baseURL, apiKey string, debug io.Writer) []runner.Case {
	return s.build(baseURL, apiKey, debug)
}

// BuildBenchmark returns the benchmark case set for this surface. The prompt
// parameter selects the benchmark prompt style ("pong" or "long").
func (s *Surface) BuildBenchmark(baseURL, apiKey string, debug io.Writer, prompt string) []runner.BenchmarkCase {
	return s.benchmarkBuild(baseURL, apiKey, debug, prompt)
}
