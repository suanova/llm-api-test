package runner

import (
	"encoding/json"
	"os"
	"time"
)

// The JSON report types below are the machine-readable output written by
// -o/--out (refactor_design.md, "JSON report"). Durations are integer
// milliseconds; rates are floats.

// StatsJSON is a duration/token summary in JSON form.
type StatsJSON struct {
	P50 int64 `json:"p50"`
	P95 int64 `json:"p95"`
	P99 int64 `json:"p99"`
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

// FloatStatsJSON is a float summary (TPS).
type FloatStatsJSON struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

func statsJSON(s Summary) StatsJSON {
	ms := func(d time.Duration) int64 { return d.Milliseconds() }
	return StatsJSON{P50: ms(s.P50), P95: ms(s.P95), P99: ms(s.P99), Min: ms(s.Min), Max: ms(s.Max)}
}

func floatStatsJSON(s FloatSummary) FloatStatsJSON {
	return FloatStatsJSON{P50: s.P50, P95: s.P95, P99: s.P99, Min: s.Min, Max: s.Max}
}

func intStatsJSON(s IntSummary) StatsJSON {
	return StatsJSON{P50: int64(s.P50), P95: int64(s.P95), P99: int64(s.P99), Min: int64(s.Min), Max: int64(s.Max)}
}

// TokensJSON holds token-count summaries.
type TokensJSON struct {
	Completion StatsJSON `json:"completion"`
	Prompt     StatsJSON `json:"prompt"`
}

// CompatJSONReport is one (model, api_format) compatibility result.
type CompatJSONReport struct {
	Model     string           `json:"model"`
	BaseURL   string           `json:"base_url"`
	APIFormat string           `json:"api_format"`
	Stream    bool             `json:"stream"`
	Support   bool             `json:"support"` // true iff all cases pass
	Cases     []CompatJSONCase `json:"cases"`
}

// CompatJSONCase is one compatibility test result. Detail is the failure
// reason ("skipped: basic failed" for gated tests) and is omitted on pass.
type CompatJSONCase struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Support bool   `json:"support"`
	Detail  string `json:"detail,omitempty"`
}

// BuildCompatJSON converts compatibility results into a JSON report.
func BuildCompatJSON(model, baseURL, apiFormat string, stream bool, results []CaseResult) CompatJSONReport {
	rep := CompatJSONReport{
		Model:     model,
		BaseURL:   baseURL,
		APIFormat: apiFormat,
		Stream:    stream,
		Support:   !AnyFailed(results),
		Cases:     make([]CompatJSONCase, 0, len(results)),
	}
	for _, r := range results {
		c := CompatJSONCase{ID: r.Case.ID(), Name: r.Case.Name(), Support: r.Result.Pass}
		if !r.Result.Pass {
			c.Detail = r.Result.Detail
		}
		rep.Cases = append(rep.Cases, c)
	}
	return rep
}

// BenchmarkJSONReport is one benchmark run.
type BenchmarkJSONReport struct {
	Model           string          `json:"model"`
	BaseURL         string          `json:"base_url"`
	APIFormat       string          `json:"api_format"`
	CaseID          string          `json:"case_id"`
	Mode            string          `json:"mode"`   // latency | throughput
	Prompt          string          `json:"prompt"` // pong | long
	Stream          bool            `json:"stream"`
	Iterations      int             `json:"iterations"`
	Concurrency     int             `json:"concurrency"`
	TotalRequests   int             `json:"total_requests"` // iterations * concurrency
	Failed          int             `json:"failed"`
	Errors          []string        `json:"errors,omitempty"` // per-request error text of failed requests
	TTFB            *StatsJSON      `json:"ttfb,omitempty"`   // omitted when !stream
	TTFT            *StatsJSON      `json:"ttft,omitempty"`
	Total           StatsJSON       `json:"total"`
	TPOT            *StatsJSON      `json:"tpot,omitempty"` // throughput mode, streamed
	TPS             *FloatStatsJSON `json:"tps,omitempty"`
	Tokens          *TokensJSON     `json:"tokens,omitempty"`
	AvgContentBytes int64           `json:"avg_content_bytes,omitempty"`
	AvgChunks       int64           `json:"avg_chunks,omitempty"`
	RPS             float64         `json:"rps"`
	TokensPerSec    float64         `json:"tokens_per_sec,omitempty"`
	ElapsedMS       int64           `json:"elapsed_ms"`
}

// JSON converts a benchmark report into machine-readable form. Stream-only
// and throughput-only indicators are omitted when not meaningful.
func (r BenchmarkReport) JSON(model, baseURL, apiFormat string) BenchmarkJSONReport {
	j := BenchmarkJSONReport{
		Model:           model,
		BaseURL:         baseURL,
		APIFormat:       apiFormat,
		CaseID:          r.CaseID,
		Mode:            r.Mode,
		Prompt:          r.Prompt,
		Stream:          r.Stream,
		Iterations:      r.Iterations,
		Concurrency:     r.Concurrency,
		TotalRequests:   r.TotalRequests,
		Failed:          r.Failed,
		Errors:          r.Errors,
		Total:           statsJSON(r.Total),
		AvgContentBytes: r.AvgContentBytes,
		AvgChunks:       r.AvgChunks,
		RPS:             r.RPS,
		TokensPerSec:    r.TokensPerSec,
		ElapsedMS:       r.Elapsed.Milliseconds(),
	}
	if r.Stream {
		ttfb := statsJSON(r.TTFB)
		ttft := statsJSON(r.TTFT)
		j.TTFB = &ttfb
		j.TTFT = &ttft
	}
	if r.Mode == "throughput" && r.Stream {
		tpot := statsJSON(r.TPOT)
		tps := floatStatsJSON(r.TPS)
		tokens := TokensJSON{
			Completion: intStatsJSON(r.Tokens.Completion),
			Prompt:     intStatsJSON(r.Tokens.Prompt),
		}
		j.TPOT = &tpot
		j.TPS = &tps
		j.Tokens = &tokens
	}
	return j
}

// CacheJSONTurn is one session turn in JSON form.
type CacheJSONTurn struct {
	Turn         int    `json:"turn"`
	PromptTokens int    `json:"prompt_tokens"`
	Cached       int    `json:"cached"`
	CacheWrite   int    `json:"cache_write"`
	Miss         int    `json:"miss"`
	TotalMS      int64  `json:"total_ms"`
	Error        string `json:"error,omitempty"`
}

// CacheJSONReport is one cache session.
type CacheJSONReport struct {
	Model           string          `json:"model"`
	BaseURL         string          `json:"base_url"`
	APIFormat       string          `json:"api_format"`
	CaseID          string          `json:"case_id"`
	Turns           []CacheJSONTurn `json:"turns"`
	SessionHitRate  float64         `json:"session_token_hit_rate"`
	WarmHitRate     float64         `json:"warm_token_hit_rate"`
	RequestLevelHit float64         `json:"warm_request_hit_rate"`
	CachedTokens    int64           `json:"cached_tokens_total"`
	PromptTokens    int64           `json:"prompt_tokens_total"`
	ColdTotalMS     int64           `json:"cold_total_ms"`
	WarmTotalP50MS  int64           `json:"warm_total_p50_ms"`
	FailedTurns     int             `json:"failed_turns"`
	Verdict         string          `json:"verdict"`
}

// CacheJSON converts a cache report into machine-readable form.
func (r CacheReport) CacheJSON(model, baseURL, apiFormat string) CacheJSONReport {
	j := CacheJSONReport{
		Model:           model,
		BaseURL:         baseURL,
		APIFormat:       apiFormat,
		CaseID:          r.CaseID,
		SessionHitRate:  r.SessionHitRate,
		WarmHitRate:     r.WarmHitRate,
		RequestLevelHit: r.RequestLevelHit,
		CachedTokens:    r.CachedTokens,
		PromptTokens:    r.PromptTokens,
		ColdTotalMS:     r.ColdTotal.Milliseconds(),
		WarmTotalP50MS:  r.WarmTotalP50.Milliseconds(),
		FailedTurns:     r.FailedTurns,
		Verdict:         r.Verdict,
		Turns:           make([]CacheJSONTurn, 0, len(r.Turns)),
	}
	for _, t := range r.Turns {
		jt := CacheJSONTurn{
			Turn:         t.Turn,
			PromptTokens: t.PromptTokens,
			Cached:       t.Cached,
			CacheWrite:   t.CacheWrite,
			Miss:         t.PromptTokens - t.Cached - t.CacheWrite,
			TotalMS:      t.Total.Milliseconds(),
		}
		if t.Err != nil {
			jt.Error = t.Err.Error()
		}
		j.Turns = append(j.Turns, jt)
	}
	return j
}

// WriteJSON writes v as indented JSON to path.
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
