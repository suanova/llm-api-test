package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"llm-api-test/internal/chat"
	"llm-api-test/internal/messages"
	"llm-api-test/internal/responses"
	"llm-api-test/internal/runner"
)

// writeConfig writes a minimal config file pointing at the given base URL.
func writeConfig(t *testing.T, dir, baseURL string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	content := fmt.Sprintf("base_url: %s\napi_key: test-key\nmodels: [m1]\n", baseURL)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// runRoot executes the CLI with the given args and returns the exit code.
func runRoot(t *testing.T, args ...string) (int, string) {
	t.Helper()
	exitCode = 0
	root := NewRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		return 2, buf.String()
	}
	return exitCode, buf.String()
}

// lineHas reports whether a report line containing id also contains mark
// (report lines pad the case ID, so substring checks across the padding are
// fragile).
func lineHas(out, id, mark string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, id) && strings.Contains(line, mark) {
			return true
		}
	}
	return false
}

// sse writes an SSE event.
func sse(w http.ResponseWriter, payload string) {
	io.WriteString(w, "data: "+payload+"\n\n")
}

// allCaseIDs are the compatibility tests across all three formats.
var allCaseIDs = []string{
	"chat:basic", "chat:system-message", "chat:response_format", "chat:seed", "chat:tool-call",
	"responses:basic", "responses:instructions", "responses:reasoning", "responses:text.format",
	"responses:text.verbosity", "responses:prompt_cache_key", "responses:tool-call",
	"messages:basic", "messages:system", "messages:thinking", "messages:cache_control", "messages:tool-use",
}

// apiMockHandler serves passable responses for all three formats, switching
// between SSE and JSON on the request's Stream flag.
func apiMockHandler(t *testing.T) http.HandlerFunc {
	// payload holds the SSE events and the non-streaming JSON reply.
	type payload struct {
		events []string
		json   string
	}

	chatCacheTurns := 0
	chatPayload := func(req *chat.Request) payload {
		switch {
		case len(req.Tools) > 0 && len(req.Messages) > 0 && req.Messages[0].Role == "system":
			// cache session: system message + tools + growing history
			chatCacheTurns++
			if chatCacheTurns == 1 {
				return payload{json: `{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":5000,"completion_tokens":20}}`}
			}
			return payload{json: `{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":5030,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":4970}}}`}
		case len(req.Tools) > 0:
			return payload{
				events: []string{
					`{"choices":[{"delta":{"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]}}]}`,
					`{"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
					"[DONE]",
				},
				json: `{"choices":[{"message":{"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			}
		case req.ResponseFormat != nil:
			return payload{
				events: []string{
					`{"choices":[{"delta":{"content":"{\"name\":\"Alice\"}"}}]}`,
					`{"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
					"[DONE]",
				},
				json: `{"choices":[{"message":{"content":"{\"name\":\"Alice\"}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			}
		case len(req.Messages) > 0 && req.Messages[0].Role == "system":
			return payload{
				events: []string{
					`{"choices":[{"delta":{"content":"hello"}}]}`,
					`{"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
					"[DONE]",
				},
				json: `{"choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			}
		default:
			return payload{
				events: []string{
					`{"choices":[{"delta":{"content":"pong"}}]}`,
					`{"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
					"[DONE]",
				},
				json: `{"choices":[{"message":{"content":"pong"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			}
		}
	}
	responsesPayload := func(req *responses.Request) payload {
		switch {
		case len(req.Tools) > 0:
			// Real wire shape: the function name is on output_item.added/done,
			// never on function_call_arguments.done.
			return payload{
				events: []string{
					`{"type":"response.output_item.added","item":{"type":"function_call","id":"call_1","name":"get_weather","arguments":"","status":"in_progress"}}`,
					`{"type":"response.function_call_arguments.delta","delta":"{}"}`,
					`{"type":"response.function_call_arguments.done","arguments":"{}","item_id":"call_1"}`,
					`{"type":"response.output_item.done","item":{"type":"function_call","id":"call_1","name":"get_weather","arguments":"{}","status":"completed"}}`,
					"[DONE]",
				},
				json: `{"output":[{"type":"function_call","name":"get_weather","arguments":"{}"}],"usage":{"input_tokens":10,"output_tokens":5}}`,
			}
		case req.Instructions != nil:
			return payload{
				events: []string{
					`{"type":"response.output_text.delta","delta":"hello"}`,
					`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5}}}`,
					"[DONE]",
				},
				json: `{"output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":5}}`,
			}
		case req.Text != nil && req.Text.Format != nil:
			return payload{
				events: []string{
					`{"type":"response.output_text.delta","delta":"{\"name\":\"Alice\"}"}`,
					`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5}}}`,
					"[DONE]",
				},
				json: `{"output":[{"type":"message","content":[{"type":"output_text","text":"{\"name\":\"Alice\"}"}]}],"usage":{"input_tokens":10,"output_tokens":5}}`,
			}
		default:
			return payload{
				events: []string{
					`{"type":"response.output_text.delta","delta":"pong"}`,
					`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5}}}`,
					"[DONE]",
				},
				json: `{"output":[{"type":"message","content":[{"type":"output_text","text":"pong"}]}],"usage":{"input_tokens":10,"output_tokens":5}}`,
			}
		}
	}
	msgCacheTurns := 0
	messagesPayload := func(req *messages.Request) payload {
		switch {
		case len(req.System) > 0 && len(req.Tools) > 0:
			// cache session: system block + tools + growing history
			msgCacheTurns++
			if msgCacheTurns == 1 {
				return payload{json: `{"content":[{"type":"text","text":"answer"}],"usage":{"input_tokens":5000,"output_tokens":20,"cache_creation_input_tokens":5000}}`}
			}
			return payload{json: `{"content":[{"type":"text","text":"answer"}],"usage":{"input_tokens":5030,"output_tokens":20,"cache_read_input_tokens":4970}}`}
		case len(req.Tools) > 0:
			return payload{
				events: []string{
					`{"type":"content_block_start","content_block":{"type":"tool_use","id":"tu1","name":"get_weather","input":{}}}`,
					`{"type":"content_block_stop"}`,
					`{"type":"message_stop"}`,
				},
				json: `{"content":[{"type":"tool_use","id":"tu1","name":"get_weather","input":{}}],"usage":{"input_tokens":10,"output_tokens":5}}`,
			}
		case len(req.System) > 0:
			return payload{
				events: []string{
					`{"type":"content_block_start","content_block":{"type":"text","text":""}}`,
					`{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`,
					`{"type":"content_block_stop"}`,
					`{"type":"message_delta","usage":{"input_tokens":10,"output_tokens":5}}`,
					`{"type":"message_stop"}`,
				},
				json: `{"content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":10,"output_tokens":5}}`,
			}
		default:
			return payload{
				events: []string{
					`{"type":"content_block_start","content_block":{"type":"text","text":""}}`,
					`{"type":"content_block_delta","delta":{"type":"text_delta","text":"pong"}}`,
					`{"type":"content_block_stop"}`,
					`{"type":"message_delta","usage":{"input_tokens":10,"output_tokens":5}}`,
					`{"type":"message_stop"}`,
				},
				json: `{"content":[{"type":"text","text":"pong"}],"usage":{"input_tokens":10,"output_tokens":5}}`,
			}
		}
	}

	serve := func(w http.ResponseWriter, stream bool, p payload) {
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, ev := range p.events {
				sse(w, ev)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, p.json)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			var req chat.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode chat request: %v", err)
			}
			serve(w, req.Stream, chatPayload(&req))
		case strings.HasSuffix(r.URL.Path, "/responses"):
			var req responses.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode responses request: %v", err)
			}
			serve(w, req.Stream, responsesPayload(&req))
		case strings.HasSuffix(r.URL.Path, "/messages"):
			var req messages.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode messages request: %v", err)
			}
			serve(w, req.Stream, messagesPayload(&req))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}
}

func TestCompatibilityAllPass(t *testing.T) {
	server := httptest.NewServer(apiMockHandler(t))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)

	code, out := runRoot(t, "--config", cfg, "compatibility")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, id := range allCaseIDs {
		if !lineHas(out, id, "PASS") {
			t.Errorf("output missing %q PASS\noutput:\n%s", id, out)
		}
	}
}

func TestCompatibilityFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"nope"}`)
	}))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)

	code, out := runRoot(t, "--config", cfg, "compatibility")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !lineHas(out, "chat:basic", "FAIL") {
		t.Errorf("output missing basic failure\noutput:\n%s", out)
	}
	if !strings.Contains(out, "skipped: basic failed") {
		t.Errorf("output missing skip note\noutput:\n%s", out)
	}
}

func TestCompatibilityTestSelection(t *testing.T) {
	server := httptest.NewServer(apiMockHandler(t))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)

	code, out := runRoot(t, "--config", cfg, "compatibility", "chat:seed")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "chat:basic") {
		t.Errorf("output includes chat:basic, want only chat:seed\noutput:\n%s", out)
	}
	if !lineHas(out, "chat:seed", "PASS") {
		t.Errorf("output missing chat:seed\noutput:\n%s", out)
	}

	code, out = runRoot(t, "--config", cfg, "compatibility", "nope")
	if code != 2 {
		t.Errorf("exit code = %d for unknown test, want 2", code)
	}
	if !strings.Contains(out, "test not found: nope") {
		t.Errorf("output missing 'test not found'\noutput:\n%s", out)
	}
}

func TestUnknownAPIGFormat(t *testing.T) {
	cfg := writeConfig(t, t.TempDir(), "http://mock.invalid")
	code, out := runRoot(t, "--config", cfg, "--api-format", "bogus", "compatibility")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(out, "unknown --api-format") {
		t.Errorf("output missing 'unknown --api-format'\noutput:\n%s", out)
	}
}

func TestList(t *testing.T) {
	cfg := writeConfig(t, t.TempDir(), "http://mock.invalid")
	code, out := runRoot(t, "--config", cfg, "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, id := range []string{"chat:seed", "responses:reasoning", "messages:thinking"} {
		if !strings.Contains(out, id) {
			t.Errorf("list missing %q\noutput:\n%s", id, out)
		}
	}
}

func TestLatencyRun(t *testing.T) {
	server := httptest.NewServer(apiMockHandler(t))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)

	code, out := runRoot(t, "--config", cfg, "latency", "--iterations", "2", "--concurrency", "2")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "4 requests") {
		t.Errorf("output missing request count\noutput:\n%s", out)
	}
	if !strings.Contains(out, "TTFB:") || !strings.Contains(out, "TTFT:") {
		t.Errorf("streamed latency benchmark should report TTFB/TTFT\noutput:\n%s", out)
	}
	if !strings.Contains(out, "RPS:") {
		t.Errorf("output missing RPS\noutput:\n%s", out)
	}
}

func TestLatencyNoStream(t *testing.T) {
	server := httptest.NewServer(apiMockHandler(t))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)

	code, out := runRoot(t, "--config", cfg, "--no-stream", "latency", "--iterations", "2", "--concurrency", "2")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "TTFB:") || strings.Contains(out, "TTFT:") {
		t.Errorf("non-streamed latency benchmark must omit TTFB/TTFT\noutput:\n%s", out)
	}
}

func TestThroughputRun(t *testing.T) {
	server := httptest.NewServer(apiMockHandler(t))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)

	code, out := runRoot(t, "--config", cfg, "throughput", "--iterations", "2", "--concurrency", "2")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "4 requests") {
		t.Errorf("output missing request count\noutput:\n%s", out)
	}
	if !strings.Contains(out, "TPOT:") || !strings.Contains(out, "TPS:") {
		t.Errorf("throughput benchmark should report TPOT/TPS\noutput:\n%s", out)
	}
	if !strings.Contains(out, "Tokens:") {
		t.Errorf("output missing token stats\noutput:\n%s", out)
	}
	if !strings.Contains(out, "RPS:") {
		t.Errorf("output missing RPS\noutput:\n%s", out)
	}
}

func TestCompatOutJSON(t *testing.T) {
	server := httptest.NewServer(apiMockHandler(t))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)
	outPath := filepath.Join(t.TempDir(), "report.json")

	code, _ := runRoot(t, "--config", cfg, "-o", outPath, "compatibility")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var reports []runner.CompatJSONReport
	if err := json.Unmarshal(data, &reports); err != nil {
		t.Fatalf("parse report: %v\n%s", err, data)
	}
	if len(reports) != 3 {
		t.Fatalf("got %d reports, want 3 (one per format)", len(reports))
	}
	for _, r := range reports {
		if !r.Support || !r.Stream {
			t.Errorf("report %+v: want support=true stream=true", r)
		}
		if r.Model != "m1" || r.BaseURL == "" {
			t.Errorf("report metadata missing: %+v", r)
		}
		for _, c := range r.Cases {
			if !c.Support {
				t.Errorf("case %s not passing: %+v", c.ID, c)
			}
		}
	}
	// Failure detail is omitted on pass.
	if reports[0].Cases[0].Detail != "" {
		t.Errorf("passing case has detail %q, want omitted", reports[0].Cases[0].Detail)
	}
}

func TestLatencyOutJSON(t *testing.T) {
	server := httptest.NewServer(apiMockHandler(t))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)
	outPath := filepath.Join(t.TempDir(), "latency.json")

	code, _ := runRoot(t, "--config", cfg, "-o", outPath, "latency", "--iterations", "2", "--concurrency", "2")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var reports []runner.BenchmarkJSONReport
	if err := json.Unmarshal(data, &reports); err != nil {
		t.Fatalf("parse report: %v\n%s", err, data)
	}
	if len(reports) != 3 {
		t.Fatalf("got %d reports, want 3", len(reports))
	}
	r := reports[0]
	if r.TotalRequests != 4 || r.Concurrency != 2 || r.Iterations != 2 {
		t.Errorf("run params = %+v, want 2x2=4 requests", r)
	}
	if !r.Stream || r.TTFB == nil || r.TTFT == nil {
		t.Errorf("streamed report must include ttfb/ttft: %+v", r)
	}
	// ElapsedMS is not asserted: against the in-process mock the whole run
	// can complete in under a millisecond, so .Milliseconds() truncates to 0.
	if r.Mode != "latency" || r.Failed != 0 {
		t.Errorf("report fields wrong: %+v", r)
	}
	// Latency mode: no throughput-only indicators.
	if r.TPOT != nil || r.TPS != nil || r.Tokens != nil {
		t.Errorf("latency report must omit tpot/tps/tokens: %+v", r)
	}
}

func TestThroughputDefaults(t *testing.T) {
	server := httptest.NewServer(apiMockHandler(t))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)

	code, out := runRoot(t, "--config", cfg, "throughput")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "9 requests") {
		t.Errorf("throughput default should be 3x3=9 requests\noutput:\n%s", out)
	}
}

func TestThroughputOutJSON(t *testing.T) {
	server := httptest.NewServer(apiMockHandler(t))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)
	outPath := filepath.Join(t.TempDir(), "throughput.json")

	code, _ := runRoot(t, "--config", cfg, "-o", outPath, "throughput", "--iterations", "2", "--concurrency", "2")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var reports []runner.BenchmarkJSONReport
	if err := json.Unmarshal(data, &reports); err != nil {
		t.Fatalf("parse report: %v\n%s", err, data)
	}
	r := reports[0]
	if r.Mode != "throughput" {
		t.Errorf("mode = %q, want throughput", r.Mode)
	}
	if r.TotalRequests != 4 || r.Failed != 0 {
		t.Errorf("run params = %+v, want 4 requests, 0 failed", r)
	}
	// Throughput mode: tpot/tps/tokens required, ttfb/ttft also present (streamed).
	if r.TPOT == nil || r.TPS == nil || r.Tokens == nil {
		t.Errorf("throughput report must include tpot/tps/tokens: %+v", r)
	}
	if !r.Stream || r.TTFB == nil || r.TTFT == nil {
		t.Errorf("streamed report must include ttfb/ttft: %+v", r)
	}
}

func TestCacheRun(t *testing.T) {
	server := httptest.NewServer(apiMockHandler(t))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)

	code, out := runRoot(t, "--config", cfg, "cache", "--turns", "3")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, id := range []string{"chat:cache", "messages:cache"} {
		if !strings.Contains(out, id) {
			t.Errorf("output missing %q\noutput:\n%s", id, out)
		}
	}
	if got := strings.Count(out, "Verdict: cache observed"); got != 2 {
		t.Errorf("got %d 'cache observed' verdicts, want 2\noutput:\n%s", got, out)
	}
	if !strings.Contains(out, "Failed: 0/3") {
		t.Errorf("output missing success line\noutput:\n%s", out)
	}
}

func TestCacheOutJSON(t *testing.T) {
	server := httptest.NewServer(apiMockHandler(t))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)
	outPath := filepath.Join(t.TempDir(), "cache.json")

	code, _ := runRoot(t, "--config", cfg, "-o", outPath, "cache", "--turns", "3")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var reports []runner.CacheJSONReport
	if err := json.Unmarshal(data, &reports); err != nil {
		t.Fatalf("parse report: %v\n%s", err, data)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2 (chat + messages)", len(reports))
	}
	for _, r := range reports {
		if r.Verdict != "cache observed" || r.SessionHitRate <= 0 {
			t.Errorf("report %+v: want observed verdict with positive hit rate", r)
		}
		if len(r.Turns) != 3 {
			t.Errorf("report %s: %d turns, want 3", r.APIFormat, len(r.Turns))
		}
		if r.Turns[0].Cached != 0 || r.Turns[1].Cached <= 0 {
			t.Errorf("report %s: cold/warm turn misparsed: %+v", r.APIFormat, r.Turns)
		}
		if r.Model != "m1" || r.BaseURL == "" {
			t.Errorf("report metadata missing: %+v", r)
		}
	}
}

func TestCacheFlagValidation(t *testing.T) {
	cfg := writeConfig(t, t.TempDir(), "http://mock.invalid")
	for _, turns := range []string{"0", "1", "99"} {
		code, out := runRoot(t, "--config", cfg, "cache", "--turns", turns)
		if code != 2 {
			t.Errorf("--turns %s: exit code = %d, want 2", turns, code)
		}
		if !strings.Contains(out, "turns must be between") {
			t.Errorf("--turns %s: output missing validation message\noutput:\n%s", turns, out)
		}
	}
	code, out := runRoot(t, "--config", cfg, "--no-stream", "cache")
	if code != 2 || !strings.Contains(out, "--no-stream does not apply") {
		t.Errorf("--no-stream cache: code = %d, want 2 with explanation\noutput:\n%s", code, out)
	}
	code, out = runRoot(t, "--config", cfg, "cache", "--api-format", "responses")
	if code != 2 || !strings.Contains(out, "has no cache test") {
		t.Errorf("--api-format responses cache: code = %d, want 2 with 'has no cache test'\noutput:\n%s", code, out)
	}
}
