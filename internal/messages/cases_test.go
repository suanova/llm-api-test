package messages

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

// msgStream replies with a text stream plus usage.
func msgStream(w http.ResponseWriter, parts ...string) {
	events := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":0}}}`,
		`{"type":"content_block_start","content_block":{"type":"text","text":""}}`,
	}
	for _, p := range parts {
		b, err := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"delta": map[string]any{"type": "text_delta", "text": p},
		})
		if err != nil {
			panic(err)
		}
		events = append(events, string(b))
	}
	events = append(events,
		`{"type":"content_block_stop"}`,
		`{"type":"message_delta","usage":{"input_tokens":10,"output_tokens":5}}`,
		`{"type":"message_stop"}`)
	writeSSE(w, events...)
}

// --- compatibility cases ---

func TestBasicStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if !req.Stream {
			t.Error("request.Stream = false, want true")
		}
		msgStream(w, "Hello", " world")
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
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"content":[{"type":"text","text":"pong"}],"usage":{"input_tokens":3,"output_tokens":1}}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, false)
	res := (&BasicCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestSystem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if len(req.System) != 1 || req.System[0].Text != "Reply with exactly: hello" {
			t.Errorf("request system = %+v, want 'Reply with exactly: hello'", req.System)
		}
		if len(req.Messages) != 1 || req.Messages[0].Content != "Say: hello" {
			t.Errorf("request user message = %+v, want 'Say: hello'", req.Messages)
		}
		if req.Temperature == nil || *req.Temperature != 0 {
			t.Errorf("request temperature = %v, want 0 (exact-match assertion needs deterministic output)", req.Temperature)
		}
		msgStream(w, "hello")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&SystemCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.Thinking == nil || req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens != 4096 {
			t.Errorf("request thinking = %+v, want enabled/4096", req.Thinking)
		}
		msgStream(w, "pong")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&ThinkingCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestCacheControl(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if len(req.System) != 1 || req.System[0].CacheControl == nil ||
			req.System[0].CacheControl.Type != "ephemeral" {
			t.Errorf("request system = %+v, want cache_control ephemeral", req.System)
		}
		msgStream(w, "pong")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&CacheControlCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if len(req.Tools) != 1 || req.Tools[0].Name != "get_weather" {
			t.Errorf("request tools = %+v, want get_weather", req.Tools)
		}
		// Tool use arrives as a start block plus input_json_delta fragments.
		writeSSE(w,
			`{"type":"content_block_start","content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather"}}`,
			`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"location\":"}}`,
			`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"\"Shanghai\"}"}}`,
			`{"type":"content_block_stop"}`,
			`{"type":"message_stop"}`,
		)
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&ToolUseCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

// --- format wiring ---

func TestFormat(t *testing.T) {
	f := Format()
	if f.Name != "messages" {
		t.Errorf("Name = %q, want messages", f.Name)
	}
	cases := f.Cases(registry.Params{
		Config: &config.Config{BaseURL: "http://mock", APIKey: "k"},
		Stream: true,
	})
	want := []string{"messages:basic", "messages:system", "messages:thinking", "messages:cache_control", "messages:tool-use"}
	if len(cases) != len(want) {
		t.Fatalf("got %d cases, want %d", len(cases), len(want))
	}
	for i, id := range want {
		if cases[i].ID() != id {
			t.Errorf("cases[%d].ID() = %q, want %q", i, cases[i].ID(), id)
		}
	}
	if bc := f.Benchmark(registry.Params{Config: &config.Config{BaseURL: "http://mock", APIKey: "k"}}); bc.ID() != "messages:benchmark" {
		t.Errorf("benchmark ID = %q, want messages:benchmark", bc.ID())
	}
}

// --- benchmark metrics ---

func TestBenchmarkCapsMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.MaxTokens != 4096 {
			t.Errorf("request max_tokens = %d, want 4096", req.MaxTokens)
		}
		msgStream(w, "a", "b")
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
		msgStream(w, "a", "b", "c")
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
		io.WriteString(w, `{"error":{"message":"unsupported field"}}`)
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
