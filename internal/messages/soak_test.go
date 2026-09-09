package messages

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// streamMessages writes one SSE messages stream: a text delta, then
// message_stop.
func streamMessages(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
	io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
}

func newSoakClient(t *testing.T, h http.HandlerFunc) *SoakCase {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	client := New(server.URL, "test-key", nil, true)
	return &SoakCase{client: client}
}

func TestSoakTurnOK(t *testing.T) {
	sc := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !req.Stream {
			t.Error("request.Stream = false, want true")
		}
		if req.MaxTokens != 64 {
			t.Errorf("max tokens = %d, want 64 (short turn)", req.MaxTokens)
		}
		if len(req.Messages) != 1 || req.Messages[0].Content != cases.SoakShortPrompt {
			t.Errorf("messages = %+v, want the short soak prompt", req.Messages)
		}
		streamMessages(w)
	})
	tr := sc.RunTurn(context.Background(), "m", false, 5*time.Second)
	if tr.Class != registry.SoakOK {
		t.Fatalf("class = %q (%v), want ok", tr.Class, tr.Err)
	}
	if tr.Total <= 0 || tr.Long {
		t.Errorf("turn = %+v, want total > 0 and short", tr)
	}
}

func TestSoakTurnLongBudget(t *testing.T) {
	sc := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req Request
		json.NewDecoder(r.Body).Decode(&req)
		if req.MaxTokens != 512 {
			t.Errorf("max tokens = %d, want 512 (long turn)", req.MaxTokens)
		}
		if len(req.Messages) != 1 || req.Messages[0].Content != cases.SoakLongPrompt {
			t.Errorf("messages = %+v, want the long soak prompt", req.Messages)
		}
		streamMessages(w)
	})
	tr := sc.RunTurn(context.Background(), "m", true, 5*time.Second)
	if tr.Class != registry.SoakOK || !tr.Long {
		t.Errorf("turn = %+v, want ok and long", tr)
	}
}

func TestSoakTurnDropped(t *testing.T) {
	// The server cuts the stream after a delta, before message_stop.
	sc := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		hj := w.(http.Hijacker)
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		conn.Close()
	})
	tr := sc.RunTurn(context.Background(), "m", false, 5*time.Second)
	if tr.Class != registry.SoakDropped {
		t.Fatalf("class = %q (%v), want dropped", tr.Class, tr.Err)
	}
}

func TestSoakTurnStreamErrorEvent(t *testing.T) {
	sc := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"overloaded\"}}\n\n")
	})
	tr := sc.RunTurn(context.Background(), "m", false, 5*time.Second)
	if tr.Class != registry.SoakOther {
		t.Fatalf("class = %q (%v), want other", tr.Class, tr.Err)
	}
	if tr.Err == nil || tr.Err.Error() != "overloaded" {
		t.Errorf("err = %v, want the stream error message", tr.Err)
	}
}

func TestSoakTurnHTTPErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		class  registry.SoakClass
	}{
		{500, registry.SoakHTTP5xx},
		{429, registry.SoakHTTP429},
		{400, registry.SoakHTTP4xx},
	} {
		sc := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			io.WriteString(w, `{"error":"nope"}`)
		})
		tr := sc.RunTurn(context.Background(), "m", false, 5*time.Second)
		if tr.Class != tc.class {
			t.Errorf("status %d: class = %q (%v), want %q", tc.status, tr.Class, tr.Err, tc.class)
		}
	}
}

func TestSoakTurnStall(t *testing.T) {
	sc := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select { // hold the connection open without data until the client gives up
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	tr := sc.RunTurn(context.Background(), "m", false, 80*time.Millisecond)
	if tr.Class != registry.SoakStall {
		t.Fatalf("class = %q (%v), want stall", tr.Class, tr.Err)
	}
	if tr.Total < 80*time.Millisecond {
		t.Errorf("total = %v, want >= the stall window", tr.Total)
	}
}
