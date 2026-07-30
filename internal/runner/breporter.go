package runner

import (
	"fmt"
	"io"
)

// TextBenchmarkReporter prints progress during a benchmark run.
type TextBenchmarkReporter struct {
	W io.Writer
}

func (t *TextBenchmarkReporter) BenchmarkStart(name, desc string, iterations, concurrency int) {
	fmt.Fprintf(t.W, "Benchmark: %s — %s (%d iters × %d concurrency)\n", name, desc, iterations, concurrency)
}

func (t *TextBenchmarkReporter) BenchmarkIteration(current, total int) {
	fmt.Fprintf(t.W, "  iteration %d/%d complete\n", current, total)
}

func (t *TextBenchmarkReporter) BenchmarkResult(r *BenchmarkResult) {
	// Result formatting is handled by FormatBenchmarkResult in the cmd layer.
}
