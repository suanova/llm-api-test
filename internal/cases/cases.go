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

// FillerPrompt returns a deterministic filler prompt of roughly n input
// tokens (backing --input-tokens). English prose tokenizes at about 1.33
// tokens per word, so it emits about 0.75*n words; the provider's usage
// report is the authoritative token count. The trailing instruction asks for
// a long continuation so generation runs to the output cap, preserving the
// ~100:1 input/output ratio of the vendor benchmark's load shape.
func FillerPrompt(n int) string {
	if n <= 0 {
		return ""
	}
	target := max(n*3/4, 1)
	var b strings.Builder
	for i, words := 1, 0; words < target; i++ {
		line := fmt.Sprintf("Item %d: %s\n", i, fillerSentences[i%len(fillerSentences)])
		b.WriteString(line)
		words += len(strings.Fields(line))
	}
	b.WriteString("\nContinue this document with a detailed technical section about each item above.")
	return b.String()
}

// fillerSentences cycle in FillerPrompt. Distinct, ordinary sentences keep
// the filler reading as real prose rather than degenerate repetition.
var fillerSentences = []string{
	"The gateway routes each request to a backend worker and records the queueing delay separately from the decode time.",
	"Capacity planning assumes a steady arrival rate, so bursts above the plan are absorbed by the admission queue first.",
	"Operators page on sustained error rates, while transient failures are retried by the client with exponential backoff.",
	"The scheduler batches independent requests together so that a single prefill pass can serve several callers at once.",
	"Memory pressure is managed by evicting the least recently used prefix blocks before admitting a new session.",
	"Each replica publishes latency histograms, token counters, and queue depths to the monitoring pipeline every few seconds.",
	"Rolling upgrades drain a replica before restarting it, so in-flight generations finish on the old process.",
	"A request that exceeds its deadline is cancelled and its reserved capacity is returned to the pool immediately.",
}
