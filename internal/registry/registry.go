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

// CacheTurn is the observation from one session turn.
type CacheTurn struct {
	Turn         int
	PromptTokens int // total prompt tokens this request
	Cached       int // tokens served from cache (read)
	CacheWrite   int // tokens written to cache; 0 for chat (automatic cache)
	Total        time.Duration
	Err          error // non-nil: turn failed, session aborts
}

// CacheCase is one simulated agent session. Turns are strictly sequential:
// each turn grows the conversation history, mirroring real agent usage.
// The session stops at the first failed turn. progress, when non-nil, is
// called with the number of completed turns after each turn.
type CacheCase interface {
	ID() string
	Desc() string
	RunSession(ctx context.Context, model string, turns int, progress func(done int)) []CacheTurn
}

// SoakClass is the coarse outcome of one soak turn (design.md "soak").
type SoakClass string

const (
	SoakOK      SoakClass = "ok"      // request completed, stream ended with its completion marker
	SoakConn    SoakClass = "conn"    // transport-level failure (connection reset, refused, EOF)
	SoakTimeout SoakClass = "timeout" // the per-turn budget expired
	SoakStall   SoakClass = "stall"   // the stream produced no event for the stall window
	SoakDropped SoakClass = "dropped" // stream ended before its completion marker (mid-stream cut)
	SoakHTTP429 SoakClass = "http-429"
	SoakHTTP5xx SoakClass = "http-5xx"
	SoakHTTP4xx SoakClass = "http-4xx"
	SoakOther   SoakClass = "other"
)

// SoakTurn is the observation from one soak turn.
type SoakTurn struct {
	Turn  int
	Long  bool // used the long generation budget
	Class SoakClass
	Err   error // set when Class != SoakOK
	Total time.Duration
}

// SoakCase is one format's soak turn: a short streamed request that must run
// to its completion marker. Soak turns are independent (no history), so the
// runner keeps going after a failed turn. Stall is the no-data watchdog
// window; the turn must classify itself when it fires. long selects a larger
// generation budget so the stream stays open longer.
type SoakCase interface {
	ID() string
	Desc() string
	RunTurn(ctx context.Context, model string, long bool, stall time.Duration) *SoakTurn
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
	Cache     func(Params) CacheCase // nil when the format has no cache test
	Soak      func(Params) SoakCase  // nil when the format has no soak test
}
