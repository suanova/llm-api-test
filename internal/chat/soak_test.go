package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// streamChat writes one SSE chat stream: a content delta, a finish_reason
// chunk, and the [DONE] sentinel.
func streamChat(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
	io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
	io.WriteString(w, "data: [DONE]\n\n")
}

func newSoakClient(t *testing.T, h http.HandlerFunc) (*SoakCase, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	client := New(server.URL, "test-key", nil, true)
	return &SoakCase{client: client}, server
}

func TestSoakTurnOK(t *testing.T) {
	sc, _ := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !req.Stream {
			t.Error("request.Stream = false, want true")
		}
		if req.MaxCompletionTokens == nil || *req.MaxCompletionTokens != 64 {
			t.Errorf("max tokens = %v, want 64 (short turn)", req.MaxCompletionTokens)
		}
		if len(req.Messages) != 1 || req.Messages[0].Content != cases.SoakShortPrompt {
			t.Errorf("messages = %+v, want the short soak prompt", req.Messages)
		}
		streamChat(w)
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
	sc, _ := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req Request
		json.NewDecoder(r.Body).Decode(&req)
		if req.MaxCompletionTokens == nil || *req.MaxCompletionTokens != 512 {
			t.Errorf("max tokens = %v, want 512 (long turn)", req.MaxCompletionTokens)
		}
		if len(req.Messages) != 1 || req.Messages[0].Content != cases.SoakLongPrompt {
			t.Errorf("messages = %+v, want the long soak prompt", req.Messages)
		}
		streamChat(w)
	})
	tr := sc.RunTurn(context.Background(), "m", true, 5*time.Second)
	if tr.Class != registry.SoakOK || !tr.Long {
		t.Errorf("turn = %+v, want ok and long", tr)
	}
}

func TestSoakTurnOKNoDone(t *testing.T) {
	// A provider that closes after the finish_reason chunk without [DONE]
	// still completes the turn.
	sc, _ := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
	})
	tr := sc.RunTurn(context.Background(), "m", false, 5*time.Second)
	if tr.Class != registry.SoakOK {
		t.Fatalf("class = %q (%v), want ok", tr.Class, tr.Err)
	}
}

func TestSoakTurnDropped(t *testing.T) {
	// The server cuts the stream after content, before any completion marker.
	sc, _ := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
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
	if tr.Total <= 0 {
		t.Error("total not recorded on a dropped turn")
	}
}

func TestSoakTurnHTTPErrors(t *testing.T) {
	cases := []struct {
		status int
		class  registry.SoakClass
	}{
		{500, registry.SoakHTTP5xx},
		{429, registry.SoakHTTP429},
		{400, registry.SoakHTTP4xx},
	}
	for _, tc := range cases {
		sc, _ := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
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
	// The server accepts the request and sends nothing: the watchdog fires.
	sc, _ := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestSoakTurnTimeout(t *testing.T) {
	// The per-turn budget expires (data trickles, so the stall watchdog does
	// not fire first).
	sc, _ := newSoakClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 100; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	tr := sc.RunTurn(ctx, "m", false, 2*time.Second) // stall far longer than the budget
	if tr.Class != registry.SoakTimeout {
		t.Fatalf("class = %q (%v), want timeout", tr.Class, tr.Err)
	}
}
