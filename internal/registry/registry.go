// Package registry defines the extension points for API formats: the format
// descriptor, the compatibility-case and benchmark-case interfaces, and the
// result and metrics types shared by the runner and the format packages
// (chat, responses, messages).
package registry

import (
	"context"
	"io"
	"time"

	"llm-api-test/internal/config"
)

// CompatResult is the outcome of a single compatibility test.
type CompatResult struct {
	Pass   bool
	Detail string
	Raw    string // response body, shown with --verbose
}

// CompatCase is one compatibility test. The first case of a format is the
// basic test: when it fails, the runner skips the remaining cases.
type CompatCase interface {
	ID() string   // e.g. "chat:seed"
	Name() string // e.g. "seed"
	Desc() string
	Run(ctx context.Context, model string) *CompatResult
}

// Metrics captures timing and token data from a single benchmark request.
// TTFB, TTFT, and TPOTs are only measured for streamed requests.
type Metrics struct {
	TTFB             time.Duration
	TTFT             time.Duration
	Total            time.Duration
	TPOTs            []time.Duration // time between consecutive content chunks
	CompletionTokens int
	PromptTokens     int
	ContentBytes     int
	Chunks           int
	Err              error // non-nil when the request failed
}

// BenchmarkCase is one benchmark scenario; a format provides exactly one.
type BenchmarkCase interface {
	ID() string
	Desc() string
	Run(ctx context.Context, model, prompt string) *Metrics
}

// Params carries run-wide settings into format construction.
type Params struct {
	Config *config.Config
	Stream bool
	Debug  io.Writer // receives --http-debug dumps
}

// Format describes one API format (chat, responses, messages). The format
// packages build their cases from Params.
type Format struct {
	Name      string
	Desc      string
	Cases     func(Params) []CompatCase // ordered: basic first
	Benchmark func(Params) BenchmarkCase
}
