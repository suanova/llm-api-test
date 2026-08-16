package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"llm-api-test/internal/cases"
)

func TestCacheSession(t *testing.T) {
	var requests []*Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, &req)
		turn := len(requests)

		// System message first, carrying the stable prompt.
		if len(req.Messages) < 2 || req.Messages[0].Role != "system" {
			t.Errorf("turn %d: messages[0] = %+v, want system message first", turn, req.Messages[0])
		}
		if req.Messages[0].Content != cases.CacheSystemPrompt {
			t.Errorf("turn %d: system content mismatch", turn)
		}
		// Tools present in the prefix.
		if len(req.Tools) == 0 {
			t.Errorf("turn %d: no tools", turn)
		}
		// History grows: turn n has 2n messages (system + 2n-1 exchanges).
		if want := 2 * turn; len(req.Messages) != want {
			t.Errorf("turn %d: %d messages, want %d", turn, len(req.Messages), want)
		}

		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			io.WriteString(w, `{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":5000,"completion_tokens":20}}`)
		} else {
			io.WriteString(w, `{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":5030,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":4970}}}`)
		}
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, false)
	turns := (&CacheCase{client: client}).RunSession(context.Background(), "m", 3)
	if len(turns) != 3 {
		t.Fatalf("got %d turns, want 3", len(turns))
	}
	if turns[0].Cached != 0 || turns[0].CacheWrite != 0 || turns[0].PromptTokens != 5000 {
		t.Errorf("turn 1 = %+v, want cached=0 written=0 prompt=5000", turns[0])
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

// TestCacheSessionDeepSeekUsage covers providers that report
// prompt_cache_hit_tokens instead of prompt_tokens_details (DeepSeek style).
func TestCacheSessionDeepSeekUsage(t *testing.T) {
	turn := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn++
		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			io.WriteString(w, `{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":5000,"completion_tokens":20,"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":5000}}`)
		} else {
			io.WriteString(w, `{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":5030,"completion_tokens":20,"prompt_cache_hit_tokens":4970,"prompt_cache_miss_tokens":60}}`)
		}
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, false)
	turns := (&CacheCase{client: client}).RunSession(context.Background(), "m", 2)
	if turns[0].Cached != 0 {
		t.Errorf("turn 1 cached = %d, want 0", turns[0].Cached)
	}
	if turns[1].Cached != 4970 {
		t.Errorf("turn 2 cached = %d, want 4970 (DeepSeek-style fallback)", turns[1].Cached)
	}
}

func TestCacheSessionAbortsOnError(t *testing.T) {
	n := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 2 {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"nope"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":5000,"completion_tokens":20}}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-key", nil, false)
	turns := (&CacheCase{client: client}).RunSession(context.Background(), "m", 4)
	if len(turns) != 2 {
		t.Fatalf("got %d turns, want 2 (aborted at turn 2)", len(turns))
	}
	if turns[1].Err == nil {
		t.Error("turn 2: want error, got nil")
	}
}
