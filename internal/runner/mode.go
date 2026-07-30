package runner

// Mode selects between compatibility (pass/fail) and benchmark (latency/throughput)
// test execution.
type Mode string

const (
	ModeCompat    Mode = "compatibility"
	ModeBenchmark Mode = "benchmark"
)

// BenchmarkConfig holds the parameters for benchmark mode.
type BenchmarkConfig struct {
	// Iterations is the number of times each case is repeated (default 10).
	Iterations int
	// Concurrency is the number of concurrent goroutines per iteration (default 5).
	Concurrency int
	// Prompt selects the benchmark prompt style: "pong" (minimal, latency-only)
	// or "long" (paragraph, for throughput measurement). Default "pong".
	Prompt string
}

// PromptPong is the prompt style that generates a minimal single-token reply.
// Use this for measuring pure latency (TTFB, TTFT, Total). TPOT/TPS are not
// meaningful with this prompt and are hidden from output.
const PromptPong = "pong"

// PromptLong is the prompt style that generates a paragraph-length reply.
// Use this for measuring throughput (TPOT, TPS) in addition to latency.
const PromptLong = "long"
