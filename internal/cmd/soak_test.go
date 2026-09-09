package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"llm-api-test/internal/runner"
)

// soakMockHandler serves streamed completions with their proper end markers
// for chat and messages, or an HTTP error when the drop flag is set.
func soakMockHandler(dropAt int) http.HandlerFunc {
	n := 0
	return func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == dropAt {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":"down"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			sse(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
			sse(w, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`)
			sse(w, `[DONE]`)
		case strings.HasSuffix(r.URL.Path, "/messages"):
			sse(w, `{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}`)
			sse(w, `{"type":"message_stop"}`)
		}
	}
}

func TestSoakRun(t *testing.T) {
	server := httptest.NewServer(soakMockHandler(0))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)

	code, out := runRoot(t, "--config", cfg, "soak",
		"--duration", "300ms", "--interval", "100ms", "--stall", "80ms", "--idle-gaps", "")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, id := range []string{"chat:soak", "messages:soak"} {
		if !strings.Contains(out, id) {
			t.Errorf("output missing %q\noutput:\n%s", id, out)
		}
	}
	if got := strings.Count(out, "failed: 0/3"); got != 2 {
		t.Errorf("got %d success lines, want 2 (chat + messages)\noutput:\n%s", got, out)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("output missing ok class\noutput:\n%s", out)
	}
}

func TestSoakOutJSON(t *testing.T) {
	server := httptest.NewServer(soakMockHandler(0))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)
	outPath := filepath.Join(t.TempDir(), "soak.json")

	code, _ := runRoot(t, "--config", cfg, "-o", outPath, "soak",
		"--duration", "300ms", "--interval", "100ms", "--stall", "80ms", "--idle-gaps", "")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var reports []runner.SoakJSONReport
	if err := json.Unmarshal(data, &reports); err != nil {
		t.Fatalf("parse report: %v\n%s", err, data)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2 (chat + messages)", len(reports))
	}
	for _, r := range reports {
		if r.Turns != 3 || r.Ok != 3 || r.Failed != 0 {
			t.Errorf("report %s = %d/%d/%d turns, want 3/3/0", r.APIFormat, r.Turns, r.Ok, r.Failed)
		}
		if len(r.Buckets) != 6 {
			t.Errorf("report %s: %d latency buckets, want 6", r.APIFormat, len(r.Buckets))
		}
		if r.Model != "m1" || r.BaseURL == "" {
			t.Errorf("report metadata missing: %+v", r)
		}
	}
}

func TestSoakFailureExitCode(t *testing.T) {
	// The second chat request fails with HTTP 503: the run reports the
	// failure and exits 1.
	server := httptest.NewServer(soakMockHandler(2))
	defer server.Close()
	cfg := writeConfig(t, t.TempDir(), server.URL)

	code, out := runRoot(t, "--config", cfg, "soak",
		"--duration", "300ms", "--interval", "100ms", "--stall", "80ms", "--idle-gaps", "")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "http-5xx") {
		t.Errorf("output missing http-5xx class\noutput:\n%s", out)
	}
}

func TestSoakFlagValidation(t *testing.T) {
	cfg := writeConfig(t, t.TempDir(), "http://mock.invalid")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"soak", "--interval", "0"}, "--interval must be positive"},
		{[]string{"soak", "--duration", "0"}, "--duration must be positive"},
		{[]string{"soak", "--stall", "0"}, "--stall must be positive"},
		{[]string{"soak", "--idle-gaps", "bogus"}, "idle gap"},
		{[]string{"soak", "--duration", "5m", "--idle-gaps", "10m"}, "does not fit"},
		{[]string{"--no-stream", "soak"}, "--no-stream does not apply"},
		{[]string{"soak", "--api-format", "responses"}, "has no soak test"},
	} {
		args := append([]string{"--config", cfg}, tc.args...)
		code, out := runRoot(t, args...)
		if code != 2 {
			t.Errorf("%v: exit code = %d, want 2", tc.args, code)
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("%v: output missing %q\noutput:\n%s", tc.args, tc.want, out)
		}
	}
}
