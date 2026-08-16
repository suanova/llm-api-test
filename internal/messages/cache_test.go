package messages

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// cacheUsage is the non-streamed JSON body the fake server replies with.
func cacheUsage(cached, written, input int) string {
	return `{"content":[{"type":"text","text":"answer"}],"usage":{"input_tokens":` + jsonInt(input) + `,"output_tokens":20,"cache_creation_input_tokens":` + jsonInt(written) + `,"cache_read_input_tokens":` + jsonInt(cached) + `}}`
}

func jsonInt(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestCacheSession(t *testing.T) {
	var requests []*Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		requests = append(requests, req)
		turn := len(requests)

		if req.Stream {
			t.Errorf("turn %d: request.Stream = true, want false (cache sessions are non-streamed)", turn)
		}
		// System block carries the breakpoint.
		if len(req.System) != 1 || req.System[0].CacheControl == nil || req.System[0].CacheControl.Type != "ephemeral" {
			t.Errorf("turn %d: system block missing cache_control", turn)
		}
		// Tools present; exactly one breakpoint, on the last tool.
		if len(req.Tools) == 0 {
			t.Errorf("turn %d: no tools", turn)
		}
		toolBPs := 0
		for i := range req.Tools {
			if req.Tools[i].CacheControl != nil {
				toolBPs++
				if i != len(req.Tools)-1 {
					t.Errorf("turn %d: breakpoint on tool %d, want only the last", turn, i)
				}
			}
		}
		if toolBPs != 1 {
			t.Errorf("turn %d: %d tool breakpoints, want 1", turn, toolBPs)
		}
		// Exactly one history breakpoint, on the last message.
		histBPs := 0
		for i := range req.Messages {
			if req.Messages[i].CacheControl != nil {
				histBPs++
				if i != len(req.Messages)-1 {
					t.Errorf("turn %d: breakpoint on message %d, want only the last", turn, i)
				}
			}
		}
		if histBPs != 1 {
			t.Errorf("turn %d: %d history breakpoints, want 1", turn, histBPs)
		}
		// History grows: turn n has 2n-1 messages.
		if want := 2*turn - 1; len(req.Messages) != want {
			t.Errorf("turn %d: %d messages, want %d", turn, len(req.Messages), want)
		}
		// MaxTokens capped so turns are cheap.
		if req.MaxTokens != 300 {
			t.Errorf("turn %d: max_tokens = %d, want 300", turn, req.MaxTokens)
		}

		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			io.WriteString(w, cacheUsage(0, 5000, 5000))
		} else {
			io.WriteString(w, cacheUsage(4970, 0, 5030))
		}
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, false)
	turns := (&CacheCase{client: client}).RunSession(context.Background(), "m", 3, nil)
	if len(turns) != 3 {
		t.Fatalf("got %d turns, want 3", len(turns))
	}
	if turns[0].Cached != 0 || turns[0].CacheWrite != 5000 || turns[0].PromptTokens != 5000 {
		t.Errorf("turn 1 = %+v, want cached=0 written=5000 prompt=5000", turns[0])
	}
	if turns[1].Cached != 4970 || turns[1].CacheWrite != 0 || turns[1].PromptTokens != 5030 {
		t.Errorf("turn 2 = %+v, want cached=4970 written=0 prompt=5030", turns[1])
	}
	for i, tr := range turns {
		if tr.Err != nil {
			t.Errorf("turn %d: unexpected error: %v", i+1, tr.Err)
		}
		if tr.Total <= 0 {
			t.Errorf("turn %d: Total not recorded", i+1)
		}
	}
}

func TestCacheSessionAbortsOnError(t *testing.T) {
	n := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 3 {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":"boom"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, cacheUsage(0, 5000, 5000))
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, false)
	turns := (&CacheCase{client: client}).RunSession(context.Background(), "m", 5, nil)
	if len(turns) != 3 {
		t.Fatalf("got %d turns, want 3 (aborted at turn 3)", len(turns))
	}
	if turns[2].Err == nil {
		t.Error("turn 3: want error, got nil")
	}
	if turns[0].Err != nil || turns[1].Err != nil {
		t.Error("turns before the failure must not carry errors")
	}
}
