// Package cases provides shared helpers for the format packages: result
// constructors for compatibility cases, the benchmark prompt texts, and a
// JSON marshaling helper.
package cases

import (
	"encoding/json"
	"fmt"
	"strings"

	"llm-api-test/internal/registry"
)

// Fail returns a failing CompatResult.
func Fail(format string, args ...any) *registry.CompatResult {
	return &registry.CompatResult{Detail: fmt.Sprintf(format, args...)}
}

// FailRaw is Fail with the raw response body attached (shown with --verbose).
func FailRaw(raw string, format string, args ...any) *registry.CompatResult {
	return &registry.CompatResult{Detail: fmt.Sprintf(format, args...), Raw: raw}
}

// Pass returns a passing CompatResult.
func Pass(detail string, raw string) *registry.CompatResult {
	return &registry.CompatResult{Pass: true, Detail: detail, Raw: raw}
}

// MustJSON marshals v to JSON, panicking only on impossible inputs. Handy for
// building request bodies from literals.
func MustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// TemperatureRejected reports whether err is the provider rejecting the
// request's temperature parameter. Models differ: some reject temperature
// outright, others accept only their own default. Cases that assert on exact
// model output send temperature 0 to stay deterministic, so they retry once
// without it rather than reporting the feature under test as unsupported.
func TemperatureRejected(err error) bool {
	return err != nil && strings.Contains(err.Error(), "temperature")
}

// PongPrompt is the short prompt used by latency benchmarks.
const PongPrompt = "Reply with exactly the word: pong"

// LongPrompt is the long prompt used by throughput benchmarks. It asks for a
// fixed-length article: the explicit length target makes output length
// predictable and keeps the model writing, while a simple instruction keeps
// the reasoning phase short and stable, so the measured TPS/TPOT mostly
// reflect steady-state generation. ~3000 English words ≈ 4096 tokens, aligned
// with the benchmark's generation cap.
const LongPrompt = "Write a detailed article of about 3000 words about the " +
	"history of the internet. Keep a consistent informative style with " +
	"concrete examples, and do not stop until the article is complete."
