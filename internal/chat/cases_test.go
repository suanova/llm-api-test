package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"llm-api-test/internal/config"
	"llm-api-test/internal/registry"
)

// --- mock server helpers ---

// decodeRequest reads and decodes the chat request body.
func decodeRequest(t *testing.T, r *http.Request) *Request {
	t.Helper()
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return &req
}

// writeSSE writes each payload as an SSE "data:" event.
func writeSSE(w http.ResponseWriter, payloads ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, p := range payloads {
		io.WriteString(w, "data: "+p+"\n\n")
	}
}

// chatStream replies with the given content chunks plus a usage chunk and the
// [DONE] sentinel. Content is JSON-marshaled so it survives embedded quotes.
func chatStream(w http.ResponseWriter, parts ...string) {
	payloads := make([]string, 0, len(parts)+2)
	for _, p := range parts {
		b, err := json.Marshal(map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{"content": p}}},
		})
		if err != nil {
			panic(err)
		}
		payloads = append(payloads, string(b))
	}
	payloads = append(payloads, `{"usage":{"prompt_tokens":10,"completion_tokens":5}}`, "[DONE]")
	writeSSE(w, payloads...)
}

// --- compatibility cases ---

func TestBasicStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if !req.Stream {
			t.Error("request.Stream = false, want true")
		}
		chatStream(w, "Hello", " world")
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
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"pong"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, false)
	res := (&BasicCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestBasicHTTPError(t *testing.T) {
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

func TestBasicEmptyContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":""}}]}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, false)
	res := (&BasicCase{client: client}).Run(context.Background(), "m")
	if res.Pass {
		t.Fatal("expected fail for empty content")
	}
	if !strings.Contains(res.Detail, "no assistant output") {
		t.Errorf("detail = %q, want 'no assistant output'", res.Detail)
	}
}

func TestSystemMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if len(req.Messages) < 2 || req.Messages[0].Role != "system" {
			t.Errorf("request has no system message: %+v", req.Messages)
		}
		if req.Messages[1].Content != "Say: hello" {
			t.Errorf("request user message = %q, want 'Say: hello'", req.Messages[1].Content)
		}
		if req.Temperature == nil || *req.Temperature != 0 {
			t.Errorf("request temperature = %v, want 0 (exact-match assertion needs deterministic output)", req.Temperature)
		}
		chatStream(w, "hello")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&SystemMessageCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestSystemMessageNotFollowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatStream(w, "wrong answer")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&SystemMessageCase{client: client}).Run(context.Background(), "m")
	if res.Pass {
		t.Fatal("expected fail when the system message is not followed")
	}
	if !strings.Contains(res.Detail, "system message not followed") {
		t.Errorf("detail = %q, want 'system message not followed'", res.Detail)
	}
}

func TestResponseFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.ResponseFormat == nil {
			t.Error("request has no response_format")
		}
		var rf struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(req.ResponseFormat, &rf); err != nil {
			t.Fatalf("decode response_format: %v", err)
		}
		if rf.Type != "json_object" {
			t.Errorf("response_format.type = %q, want json_object", rf.Type)
		}
		var prompt string
		for _, m := range req.Messages {
			prompt += m.Content + " "
		}
		if !strings.Contains(strings.ToLower(prompt), "json") {
			t.Error("prompt does not instruct the model to produce JSON")
		}
		chatStream(w, `{"name":"Alice"}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&ResponseFormatCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestResponseFormatBadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatStream(w, "not json")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&ResponseFormatCase{client: client}).Run(context.Background(), "m")
	if res.Pass {
		t.Fatal("expected fail for non-JSON response")
	}
	if !strings.Contains(res.Detail, "not valid JSON") {
		t.Errorf("detail = %q, want 'not valid JSON'", res.Detail)
	}
}

func TestSeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.Seed == nil || *req.Seed != 42 {
			t.Errorf("request seed = %v, want 42", req.Seed)
		}
		chatStream(w, "pong")
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	res := (&SeedCase{client: client}).Run(context.Background(), "m")
	if !res.Pass {
		t.Fatalf("expected pass, got: %s", res.Detail)
	}
}

func TestToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "get_weather" {
			t.Errorf("request tools = %+v, want get_weather", req.Tools)
		}
		writeSSE(w,
			`{"choices":[{"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"location\":\"Shanghai\"}"}}]}}]}`,
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
	if f.Name != "chat" {
		t.Errorf("Name = %q, want chat", f.Name)
	}
	cases := f.Cases(registry.Params{
		Config: &config.Config{BaseURL: "http://mock", APIKey: "k"},
		Stream: true,
	})
	want := []string{"chat:basic", "chat:system-message", "chat:response_format", "chat:seed", "chat:tool-call"}
	if len(cases) != len(want) {
		t.Fatalf("got %d cases, want %d", len(cases), len(want))
	}
	for i, id := range want {
		if cases[i].ID() != id {
			t.Errorf("cases[%d].ID() = %q, want %q", i, cases[i].ID(), id)
		}
	}
	if bc := f.Benchmark(registry.Params{Config: &config.Config{BaseURL: "http://mock", APIKey: "k"}}); bc.ID() != "chat:benchmark" {
		t.Errorf("benchmark ID = %q, want chat:benchmark", bc.ID())
	}
}

// --- benchmark metrics ---

func TestBenchmarkCapsMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.MaxCompletionTokens == nil || *req.MaxCompletionTokens != 4096 {
			t.Errorf("request max_completion_tokens = %v, want 4096", req.MaxCompletionTokens)
		}
		chatStream(w, "a", "b")
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
		chatStream(w, "a", "b", "c")
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
	if m.TTFT < m.TTFB || m.Total < m.TTFT {
		t.Errorf("timings out of order: TTFB=%v TTFT=%v Total=%v", m.TTFB, m.TTFT, m.Total)
	}
	if len(m.TPOTs) != 2 {
		t.Errorf("TPOTs = %v, want 2 inter-chunk gaps", len(m.TPOTs))
	}
	if m.Chunks != 3 || m.ContentBytes != 3 {
		t.Errorf("Chunks/ContentBytes = %d/%d, want 3/3", m.Chunks, m.ContentBytes)
	}
	if m.CompletionTokens != 5 || m.PromptTokens != 10 {
		t.Errorf("tokens = %d/%d, want 5/10", m.CompletionTokens, m.PromptTokens)
	}
	if time.Since(time.Now().Add(-m.Total)) > time.Minute {
		t.Error("Total looks wrong")
	}
}

func TestBenchmarkHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":"overloaded"}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, true)
	bc := &BenchmarkCase{client: client}
	m := bc.Run(context.Background(), "m", "pong")
	if m.Err == nil {
		t.Fatal("expected error for HTTP 503")
	}
}
