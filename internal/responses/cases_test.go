package responses

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"llm-api-test/internal/config"
	"llm-api-test/internal/registry"
)

// --- mock server helpers ---

func decodeRequest(t *testing.T, r *http.Request) *Request {
	t.Helper()
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return &req
}

func writeSSE(w http.ResponseWriter, payloads ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, p := range payloads {
		io.WriteString(w, "data: "+p+"\n\n")
	}
}

// respStream replies with text deltas, a completed event with usage, and the
// [DONE] sentinel.
func respStream(w http.ResponseWriter, parts ...string) {
	payloads := make([]string, 0, len(parts)+2)
	for _, p := range parts {
		b, err := json.Marshal(map[string]any{"type": "response.output_text.delta", "delta": p})
		if err != nil {
			panic(err)
		}
		payloads = append(payloads, string(b))
	}
	payloads = append(payloads,
		`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5}}}`,
		"[DONE]")
	writeSSE(w, payloads...)
}

// --- compatibility cases ---

func TestBasicStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if !req.Stream {
			t.Error("request.Stream = false, want true")
		}
		respStream(w, "Hello", " world")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&BasicCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestBasicPlain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.Stream {
			t.Error("request.Stream = true, want false")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"pong"}]}],"usage":{"input_tokens":3,"output_tokens":1}}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, false)
	res := (&BasicCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestInstructions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.Instructions == nil || *req.Instructions != "Reply with exactly: hello" {
			t.Errorf("request instructions = %v, want 'Reply with exactly: hello'", req.Instructions)
		}
		if req.Input != "Say: hello" {
			t.Errorf("request input = %q, want 'Say: hello'", req.Input)
		}
		if req.Temperature == nil || *req.Temperature != 0 {
			t.Errorf("request temperature = %v, want 0 (exact-match assertion needs deterministic output)", req.Temperature)
		}
		respStream(w, "hello")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&InstructionsCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.Reasoning == nil || req.Reasoning.Effort != "high" || req.Reasoning.Summary != "concise" {
			t.Errorf("request reasoning = %+v, want effort=high summary=concise", req.Reasoning)
		}
		respStream(w, "pong")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&ReasoningCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestTextFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.Text == nil || req.Text.Format == nil {
			t.Error("request has no text.format")
		}
		respStream(w, `{"name":"Alice"}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&TextFormatCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestTextFormatBadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respStream(w, "not json")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&TextFormatCase{client: client}).Run(context.Background(), "m")
	if res.Pass {
		t.Fatal("expected fail for non-JSON response")
	}
	if !strings.Contains(res.Detail, "not schema-conformant") {
		t.Errorf("detail = %q, want 'not schema-conformant'", res.Detail)
	}
}

func TestVerbosity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.Text == nil || req.Text.Verbosity != "low" {
			t.Errorf("request text = %+v, want verbosity=low", req.Text)
		}
		respStream(w, "pong")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&VerbosityCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestPromptCacheKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.PromptCacheKey != "test-cache-key" {
			t.Errorf("request prompt_cache_key = %q, want test-cache-key", req.PromptCacheKey)
		}
		respStream(w, "pong")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&PromptCacheKeyCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if len(req.Tools) != 1 || req.Tools[0].Name != "get_weather" {
			t.Errorf("request tools = %+v, want get_weather", req.Tools)
		}
		// Real wire shape: the function name is on output_item.added/done,
		// never on function_call_arguments.done.
		writeSSE(w,
			`{"type":"response.output_item.added","item":{"type":"function_call","id":"call_1","name":"get_weather","arguments":"","status":"in_progress"}}`,
			`{"type":"response.function_call_arguments.delta","delta":"{\"location\":\"Shanghai\"}"}`,
			`{"type":"response.function_call_arguments.done","arguments":"{\"location\": \"Shanghai\"}","item_id":"call_1"}`,
			`{"type":"response.output_item.done","item":{"type":"function_call","id":"call_1","name":"get_weather","arguments":"{\"location\": \"Shanghai\"}","status":"completed"}}`,
			"[DONE]",
		)
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&ToolCallCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

// --- format wiring ---

func TestFormat(t *testing.T) {
	f := Format()
	if f.Name != "responses" {
		t.Errorf("Name = %q, want responses", f.Name)
	}
	cases := f.Cases(registry.Params{
		Config: &config.Config{BaseURL: "http://mock", APIKey: "k"},
		Stream: true,
	})
	want := []string{
		"responses:basic", "responses:instructions", "responses:reasoning",
		"responses:text.format", "responses:text.verbosity", "responses:prompt_cache_key",
		"responses:tool-call",
	}
	if len(cases) != len(want) {
		t.Fatalf("got %d cases, want %d", len(cases), len(want))
	}
	for i, id := range want {
		if cases[i].ID() != id {
			t.Errorf("cases[%d].ID() = %q, want %q", i, cases[i].ID(), id)
		}
	}
	if bc := f.Benchmark(registry.Params{Config: &config.Config{BaseURL: "http://mock", APIKey: "k"}}); bc.ID() != "responses:benchmark" {
		t.Errorf("benchmark ID = %q, want responses:benchmark", bc.ID())
	}
}

// --- benchmark metrics ---

func TestBenchmarkCapsMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.MaxOutputTokens == nil || *req.MaxOutputTokens != 4096 {
			t.Errorf("request max_output_tokens = %v, want 4096", req.MaxOutputTokens)
		}
		respStream(w, "a", "b")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	bc := &BenchmarkCase{client: client}
	if m := bc.Run(context.Background(), "m", "long prompt"); m.Err != nil {
		t.Fatalf("unexpected error: %v", m.Err)
	}
}

func TestBenchmarkMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respStream(w, "a", "b", "c")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	bc := &BenchmarkCase{client: client}
	m := bc.Run(context.Background(), "m", "pong")
	if m.Err != nil {
		t.Fatalf("unexpected error: %v", m.Err)
	}
	if m.TTFB <= 0 || m.TTFT <= 0 || m.Total <= 0 {
		t.Errorf("timings not recorded: TTFB=%v TTFT=%v Total=%v", m.TTFB, m.TTFT, m.Total)
	}
	if len(m.TPOTs) != 2 {
		t.Errorf("TPOTs = %d, want 2 inter-chunk gaps", len(m.TPOTs))
	}
	if m.Chunks != 3 || m.ContentBytes != 3 {
		t.Errorf("Chunks/ContentBytes = %d/%d, want 3/3", m.Chunks, m.ContentBytes)
	}
	if m.CompletionTokens != 5 || m.PromptTokens != 10 {
		t.Errorf("tokens = %d/%d, want 5/10", m.CompletionTokens, m.PromptTokens)
	}
}

func TestHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"unsupported field"}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&BasicCase{client: client}).Run(context.Background(), "m")
	if res.Pass {
		t.Fatal("expected fail")
	}
	if !strings.Contains(res.Detail, "HTTP 400") {
		t.Errorf("detail = %q, want mention of HTTP 400", res.Detail)
	}
}
