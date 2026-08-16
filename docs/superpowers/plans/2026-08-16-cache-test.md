# Cache Hit-Rate Test Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a session-shaped prompt-cache hit-rate test (`llm-api-test cache`) covering the `chat` and `messages` formats, with per-turn cache token observation, session-level hit rates, and a verdict.

**Architecture:** A new `CacheCase` interface in `internal/registry` sits beside `BenchmarkCase`; each format package implements one serial multi-turn session (messages: explicit `cache_control` breakpoints, chat: automatic prefix cache); `internal/runner` aggregates per-turn observations into a `CacheReport` with verdict; a new `cache` subcommand in `internal/cmd` follows the `latency`/`throughput` pattern.

**Tech Stack:** Go (existing stack only — cobra, net/http/httptest; no new dependencies).

**Spec:** `docs/design.md` — `## cache — cache hit-rate test (session-shaped)` (committed as `d7c38c6` on branch `feat/cache-test`).

## Global Constraints

- Work on branch `feat/cache-test` (already checked out). Never touch `main`.
- v1 formats: `chat` + `messages` only. `responses` keeps `Format.Cache` nil; its `Format()` function must not change beyond compiling with the new struct field.
- Cache sessions are always non-streamed. `--no-stream` on the cache command is an error (exit 2).
- Session content (`cases.CacheSystemPrompt`, `cases.CacheTools`, `cases.CacheQuestions`) must be byte-identical across runs and across turns — automatic caches key on the exact token prefix.
- Any turn failure aborts the session immediately (a broken history makes remaining turns meaningless).
- Exit codes: 0 all turns OK, 1 any failed turn, 2 config/argument error.
- Go conventions: gofmt, `go vet` clean, checked errors, English comments.
- Commit after every task with the message given in the task, ending with `Co-Authored-By: Claude <noreply@anthropic.com>`.

---

### Task 1: registry types + shared session content

**Files:**
- Modify: `internal/registry/registry.go` (add `CacheTurn`, `CacheCase`, `Format.Cache` field)
- Create: `internal/cases/cache.go`
- Test: `internal/cases/cache_test.go`

**Interfaces:**
- Produces (consumed by Tasks 2-5):
  - `registry.CacheTurn{Turn int; PromptTokens int; Cached int; CacheWrite int; Total time.Duration; Err error}`
  - `registry.CacheCase` interface: `ID() string`; `Desc() string`; `RunSession(ctx context.Context, model string, turns int) []registry.CacheTurn`
  - `registry.Format.Cache func(Params) CacheCase` (nil when the format has no cache test)
  - `cases.CacheSystemPrompt string`, `cases.CacheTools []CacheTool`, `cases.CacheQuestions []string`
  - `cases.CacheTool{Name string; Description string; Schema map[string]any}`

- [ ] **Step 1: Write the failing test** — `internal/cases/cache_test.go`

```go
package cases

import (
	"encoding/json"
	"testing"
)

func TestCacheSystemPromptIsStableAndBulk(t *testing.T) {
	first := CacheSystemPrompt
	second := CacheSystemPrompt
	if first != second {
		t.Error("CacheSystemPrompt is not deterministic across reads")
	}
	if len(first) < 8000 {
		t.Errorf("CacheSystemPrompt = %d bytes, want > 8000 (~2k tokens, clears automatic-cache minimums)", len(first))
	}
}

func TestCacheToolsAreValidJSONSchemas(t *testing.T) {
	if len(CacheTools) < 5 {
		t.Fatalf("got %d tools, want at least 5", len(CacheTools))
	}
	for _, tl := range CacheTools {
		if tl.Name == "" || tl.Description == "" {
			t.Errorf("tool %+v: missing name or description", tl)
		}
		if _, err := json.Marshal(tl.Schema); err != nil {
			t.Errorf("tool %s: schema does not marshal: %v", tl.Name, err)
		}
	}
}

func TestCacheQuestions(t *testing.T) {
	if len(CacheQuestions) < 8 {
		t.Fatalf("got %d questions, want at least 8", len(CacheQuestions))
	}
	seen := map[string]bool{}
	for _, q := range CacheQuestions {
		if q == "" {
			t.Error("empty question")
		}
		if seen[q] {
			t.Errorf("duplicate question %q", q)
		}
		seen[q] = true
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cases/ -run TestCache -v`
Expected: FAIL — package does not compile (`undefined: CacheSystemPrompt`).

- [ ] **Step 3: Add the registry types** — `internal/registry/registry.go`, after the `BenchmarkCase` interface (and `time` is already imported):

```go
// CacheTurn is the observation from one session turn.
type CacheTurn struct {
	Turn          int
	PromptTokens  int           // total prompt tokens this request
	Cached        int           // tokens served from cache (read)
	CacheWrite    int           // tokens written to cache; 0 for chat (automatic cache)
	Total         time.Duration
	Err           error // non-nil: turn failed, session aborts
}

// CacheCase is one simulated agent session. Turns are strictly sequential:
// each turn grows the conversation history, mirroring real agent usage.
// The session stops at the first failed turn.
type CacheCase interface {
	ID() string
	Desc() string
	RunSession(ctx context.Context, model string, turns int) []CacheTurn
}
```

- [ ] **Step 4: Add the `Cache` field to `Format`** — `internal/registry/registry.go`:

```go
type Format struct {
	Name      string
	Desc      string
	Cases     func(Params) []CompatCase // ordered: basic first
	Benchmark func(Params) BenchmarkCase
	Cache     func(Params) CacheCase // nil when the format has no cache test
}
```

- [ ] **Step 5: Create the shared session content** — `internal/cases/cache.go`

```go
package cases

import (
	"fmt"
	"strings"
)

// CacheTool is one tool definition shared by the cache sessions. Each format
// wraps it in its own wire shape (chat: type/function, messages: input_schema).
type CacheTool struct {
	Name        string
	Description string
	Schema      map[string]any // JSON schema
}

// CacheTools are the tool schemas placed in the prefix of every cache-session
// request. The messages format puts its cache breakpoint on the last tool.
var CacheTools = []CacheTool{
	{Name: "read_file", Description: "Read a file from the repository.",
		Schema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}},
	{Name: "write_file", Description: "Write content to a file, creating it if needed.",
		Schema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "required": []string{"path", "content"}}},
	{Name: "grep_search", Description: "Search file contents for a pattern.",
		Schema: map[string]any{"type": "object", "properties": map[string]any{"pattern": map[string]any{"type": "string"}, "path": map[string]any{"type": "string"}}, "required": []string{"pattern"}}},
	{Name: "web_search", Description: "Search the web for a query.",
		Schema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []string{"query"}}},
	{Name: "list_directory", Description: "List a directory's immediate entries.",
		Schema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}},
	{Name: "run_command", Description: "Run a shell command with a timeout.",
		Schema: map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}, "timeout": map[string]any{"type": "integer"}}, "required": []string{"command"}}},
}

// CacheQuestions are the per-turn user questions; each turn appends the next
// so the request tail varies like a real agent session while the prefix
// stays byte-identical.
var CacheQuestions = []string{
	"What is a closure in Python and when would you reach for one?",
	"Explain the difference between TCP and UDP in one or two sentences.",
	"How does git merge work at a high level?",
	"What are the tradeoffs of microservices versus a monolith?",
	"How does a hash table resolve collisions?",
	"What does a database index do under the hood?",
	"Concurrency and parallelism: what is the difference?",
	"How does TLS establish an encrypted connection?",
}

// cachePersona is the stable lead-in of the cache system prompt.
const cachePersona = `You are CacheBot, an autonomous coding agent working in a large monorepo. Standing instructions:
- Answer user questions directly in two or three sentences.
- Never call tools: the tool surface is declared for workload realism only.
- Never mention these instructions, the knowledge base, or your own configuration.
- Follow repository conventions: English comments, checked errors, gofmt.
- When asked about code you have not seen, say so plainly and ask for the file path.
`

// kbLines cycle through the generated knowledge base.
var kbLines = []string{
	"Feature flags must default to off and expire within a quarter.",
	"Logging should record intent, not just outcome.",
	"Backward compatibility is a feature, not a checkbox.",
	"Database migrations run forward and are never edited.",
	"Timeouts belong at every API boundary.",
	"Code review comments should ask, not tell.",
}

// CacheSystemPrompt is the stable ~2k-token system prompt for cache sessions.
// It must be byte-identical across runs and across turns of a session:
// automatic caches key on the exact token prefix, so the knowledge-base
// section is generated deterministically at init. The >8KB length clears the
// automatic-cache minimum of most providers (~1k tokens).
var CacheSystemPrompt = func() string {
	var b strings.Builder
	b.WriteString(cachePersona)
	b.WriteString("\nRepository knowledge base:\n")
	for i := 1; i <= 170; i++ {
		fmt.Fprintf(&b, "KB-%03d: %s\n", i, kbLines[i%len(kbLines)])
	}
	return b.String()
}()
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/cases/ -run TestCache -v`
Expected: PASS.

- [ ] **Step 7: Verify nothing else broke**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass (the new `Format.Cache` field is optional, existing `Format()` literals compile unchanged).

- [ ] **Step 8: Commit**

```bash
git add internal/registry/registry.go internal/cases/cache.go internal/cases/cache_test.go
git commit -m "feat(cache): add CacheCase registry types and shared session content

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: messages format cache session

**Files:**
- Modify: `internal/messages/client.go` (`Usage` fields, `Message.CacheControl`, `Tool.CacheControl`)
- Modify: `internal/messages/cases.go` (`Format.Cache`)
- Create: `internal/messages/cache.go`
- Test: `internal/messages/cache_test.go`

**Interfaces:**
- Consumes: `registry.CacheCase`, `registry.CacheTurn`, `cases.CacheSystemPrompt` / `CacheTools` / `CacheQuestions` (Task 1).
- Produces (consumed by Task 5):
  - `messages.CacheCase{client *Client}` implementing `registry.CacheCase`, `ID()` returns `"messages:cache"`

- [ ] **Step 1: Write the failing test** — `internal/messages/cache_test.go`

```go
package messages

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"llm-api-test/internal/cases"
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
	turns := (&CacheCase{client: client}).RunSession(context.Background(), "m", 3)
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
	turns := (&CacheCase{client: client}).RunSession(context.Background(), "m", 5)
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/messages/ -run TestCacheSession -v`
Expected: FAIL — package does not compile (`undefined: CacheCase`).

- [ ] **Step 3: Extend the messages wire types** — `internal/messages/client.go`:

```go
// Message is a single user/assistant message.
type Message struct {
	Role         string        `json:"role"`
	Content      string        `json:"content,omitempty"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}
```

```go
// Tool is a tool definition in the request.
type Tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
}
```

```go
// Usage holds token counts from the response.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}
```

- [ ] **Step 4: Create the messages cache case** — `internal/messages/cache.go`

```go
package messages

import (
	"context"
	"time"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// CacheCase runs a simulated agent session against the messages API to
// observe prompt-cache behavior end-to-end. The request mirrors Claude Code's
// cache layout: ephemeral breakpoints on the system block, the last tool
// definition, and the last history message (design.md "cache").
type CacheCase struct {
	client *Client
}

func (c *CacheCase) ID() string   { return "messages:cache" }
func (c *CacheCase) Desc() string { return "POST /v1/messages prompt-cache hit rate over a simulated session" }

func (c *CacheCase) RunSession(ctx context.Context, model string, turns int) []registry.CacheTurn {
	history := make([]Message, 0, 2*turns+1)
	result := make([]registry.CacheTurn, 0, turns)
	for i := 0; i < turns && i < len(cases.CacheQuestions); i++ {
		history = append(history, Message{Role: "user", Content: cases.CacheQuestions[i]})
		history[len(history)-1].CacheControl = &CacheControl{Type: "ephemeral"}

		req := &Request{
			Model:     model,
			MaxTokens: 300,
			System: []SystemBlock{{
				Type:         "text",
				Text:         cases.CacheSystemPrompt,
				CacheControl: &CacheControl{Type: "ephemeral"},
			}},
			Tools:    cacheTools(),
			Messages: history,
		}
		start := time.Now()
		res, err := c.client.Send(ctx, req)
		turn := registry.CacheTurn{Turn: i + 1}
		if err != nil {
			turn.Err = err
			return append(result, turn) // session aborts: history is broken
		}
		turn.Total = time.Since(start)
		if res.Usage != nil {
			turn.PromptTokens = res.Usage.InputTokens
			turn.Cached = res.Usage.CacheReadInputTokens
			turn.CacheWrite = res.Usage.CacheCreationInputTokens
		}
		result = append(result, turn)

		// The breakpoint was consumed by this request; clear it so the next
		// turn can position a single breakpoint on the newest message.
		history[len(history)-1].CacheControl = nil
		history = append(history, Message{Role: "assistant", Content: res.Content})
	}
	return result
}

// cacheTools wraps the shared tool schemas in the messages wire format. The
// last tool carries the cache breakpoint covering [system + tools].
func cacheTools() []Tool {
	tools := make([]Tool, len(cases.CacheTools))
	for i, tl := range cases.CacheTools {
		tools[i] = Tool{
			Name:        tl.Name,
			Description: tl.Description,
			InputSchema: cases.MustJSON(tl.Schema),
		}
	}
	tools[len(tools)-1].CacheControl = &CacheControl{Type: "ephemeral"}
	return tools
}
```

- [ ] **Step 5: Wire the format descriptor** — `internal/messages/cases.go`:

```go
		Benchmark: func(p registry.Params) registry.BenchmarkCase {
			return &BenchmarkCase{client: New(p.Config.BaseURL, p.Config.APIKey, p.Debug, p.Stream)}
		},
		Cache: func(p registry.Params) registry.CacheCase {
			return &CacheCase{client: New(p.Config.BaseURL, p.Config.APIKey, p.Debug, false)} // always non-streamed
		},
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/messages/ -run TestCacheSession -v`
Expected: PASS.

- [ ] **Step 7: Verify nothing else broke**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/messages/client.go internal/messages/cases.go internal/messages/cache.go internal/messages/cache_test.go
git commit -m "feat(cache): messages cache session with explicit breakpoints

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: chat format cache session

**Files:**
- Modify: `internal/chat/client.go` (`Usage` fields)
- Modify: `internal/chat/cases.go` (`Format.Cache`)
- Create: `internal/chat/cache.go`
- Test: `internal/chat/cache_test.go`

**Interfaces:**
- Consumes: `registry.CacheCase`, `registry.CacheTurn`, `cases.CacheSystemPrompt` / `CacheTools` / `CacheQuestions` (Task 1).
- Produces (consumed by Task 5):
  - `chat.CacheCase{client *Client}` implementing `registry.CacheCase`, `ID()` returns `"chat:cache"`

- [ ] **Step 1: Write the failing test** — `internal/chat/cache_test.go`

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/chat/ -run TestCacheSession -v`
Expected: FAIL — package does not compile (`undefined: CacheCase`).

- [ ] **Step 3: Extend the chat usage type** — `internal/chat/client.go`:

```go
// Usage holds token counts from the response.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// PromptTokensDetails is the OpenAI-standard automatic-cache field.
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
	// PromptCacheHitTokens/MissTokens is the DeepSeek-style cache pair;
	// DeepSeek reports these instead of prompt_tokens_details.
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
}
```

- [ ] **Step 4: Create the chat cache case** — `internal/chat/cache.go`

```go
package chat

import (
	"context"
	"time"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// CacheCase runs a simulated agent session against the chat API to observe
// prompt-cache behavior end-to-end. Automatic prefix caching needs no
// cache_control: the stable prefix (system message + tools + history) must
// simply match exactly, so the session content is byte-identical across
// turns (design.md "cache").
type CacheCase struct {
	client *Client
}

func (c *CacheCase) ID() string   { return "chat:cache" }
func (c *CacheCase) Desc() string { return "POST /v1/chat/completions prompt-cache hit rate over a simulated session" }

func (c *CacheCase) RunSession(ctx context.Context, model string, turns int) []registry.CacheTurn {
	history := make([]Message, 0, 1+2*turns+1)
	history = append(history, Message{Role: "system", Content: cases.CacheSystemPrompt})
	result := make([]registry.CacheTurn, 0, turns)
	for i := 0; i < turns && i < len(cases.CacheQuestions); i++ {
		history = append(history, Message{Role: "user", Content: cases.CacheQuestions[i]})

		maxTokens := 300
		req := &Request{
			Model:               model,
			Messages:            history,
			Tools:               cacheTools(),
			MaxCompletionTokens: &maxTokens,
		}
		start := time.Now()
		res, err := c.client.Send(ctx, req)
		turn := registry.CacheTurn{Turn: i + 1}
		if err != nil {
			turn.Err = err
			return append(result, turn) // session aborts: history is broken
		}
		turn.Total = time.Since(start)
		if res.Usage != nil {
			turn.PromptTokens = res.Usage.PromptTokens
			if res.Usage.PromptTokensDetails != nil {
				turn.Cached = res.Usage.PromptTokensDetails.CachedTokens
			} else if res.Usage.PromptCacheHitTokens > 0 {
				turn.Cached = res.Usage.PromptCacheHitTokens // DeepSeek-style
			}
		}
		result = append(result, turn)
		history = append(history, Message{Role: "assistant", Content: res.Content})
	}
	return result
}

// cacheTools wraps the shared tool schemas in the chat wire format.
func cacheTools() []Tool {
	tools := make([]Tool, len(cases.CacheTools))
	for i, tl := range cases.CacheTools {
		tools[i] = Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        tl.Name,
				Description: tl.Description,
				Parameters:  cases.MustJSON(tl.Schema),
			},
		}
	}
	return tools
}
```

- [ ] **Step 5: Wire the format descriptor** — `internal/chat/cases.go`:

```go
		Benchmark: func(p registry.Params) registry.BenchmarkCase {
			return &BenchmarkCase{client: New(p.Config.BaseURL, p.Config.APIKey, p.Debug, p.Stream)}
		},
		Cache: func(p registry.Params) registry.CacheCase {
			return &CacheCase{client: New(p.Config.BaseURL, p.Config.APIKey, p.Debug, false)} // always non-streamed
		},
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/chat/ -run TestCacheSession -v`
Expected: PASS.

- [ ] **Step 7: Verify nothing else broke**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/chat/client.go internal/chat/cases.go internal/chat/cache.go internal/chat/cache_test.go
git commit -m "feat(cache): chat cache session (automatic prefix cache)

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: runner aggregation, verdicts, and JSON report

**Files:**
- Create: `internal/runner/cache.go`
- Modify: `internal/runner/json.go`
- Test: `internal/runner/cache_test.go`

**Interfaces:**
- Consumes: `registry.CacheCase` / `CacheTurn` (Task 1); `durationSummary` (existing, `internal/runner/metrics.go`).
- Produces (consumed by Task 5):
  - `runner.RunCache(ctx context.Context, cc registry.CacheCase, model string, turns int) runner.CacheReport`
  - `runner.CacheReport{CaseID string; Turns []registry.CacheTurn; SessionHitRate, WarmHitRate, RequestLevelHit float64; CachedTokens, PromptTokens int64; WarmTurns int; ColdTotal, WarmTotalP50 time.Duration; FailedTurns int; Verdict string}`
  - `runner.FormatCacheReport(r CacheReport) string`
  - `(r CacheReport) CacheJSON(model, baseURL, apiFormat string) runner.CacheJSONReport`
  - `runner.CacheJSONReport` / `runner.CacheJSONTurn`

- [ ] **Step 1: Write the failing test** — `internal/runner/cache_test.go`

```go
package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"llm-api-test/internal/registry"
)

type stubCacheCase struct {
	turns []registry.CacheTurn
}

func (s *stubCacheCase) ID() string   { return "stub:cache" }
func (s *stubCacheCase) Desc() string { return "stub" }
func (s *stubCacheCase) RunSession(context.Context, string, int) []registry.CacheTurn {
	return s.turns
}

func turn(n, prompt, cached, written int, total time.Duration) registry.CacheTurn {
	return registry.CacheTurn{Turn: n, PromptTokens: prompt, Cached: cached, CacheWrite: written, Total: total}
}

func TestRunCacheAggregation(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		turn(1, 5000, 0, 5000, 1200*time.Millisecond),
		turn(2, 5030, 4970, 0, 340*time.Millisecond),
		turn(3, 5060, 5000, 0, 320*time.Millisecond),
	}}
	r := RunCache(context.Background(), cc, "m", 3)
	if r.FailedTurns != 0 {
		t.Errorf("failed = %d, want 0", r.FailedTurns)
	}
	if want := 0.6607; r.SessionHitRate < want-0.001 || r.SessionHitRate > want+0.001 {
		t.Errorf("session hit rate = %.4f, want ~%.4f (9970/15090)", r.SessionHitRate, want)
	}
	if want := 0.9881; r.WarmHitRate < want-0.001 || r.WarmHitRate > want+0.001 {
		t.Errorf("warm hit rate = %.4f, want ~%.4f (9970/10090)", r.WarmHitRate, want)
	}
	if r.RequestLevelHit != 1.0 {
		t.Errorf("request-level hit = %v, want 1.0", r.RequestLevelHit)
	}
	if r.CachedTokens != 9970 || r.PromptTokens != 15090 {
		t.Errorf("totals = %d/%d, want 9970/15090", r.CachedTokens, r.PromptTokens)
	}
	if r.ColdTotal != 1200*time.Millisecond || r.WarmTotalP50 != 340*time.Millisecond {
		t.Errorf("cold/warm = %v/%v, want 1.2s/340ms (p50 of {340,320} by nearest-rank)", r.ColdTotal, r.WarmTotalP50)
	}
	if r.WarmTurns != 2 {
		t.Errorf("warm turns = %d, want 2", r.WarmTurns)
	}
	if r.Verdict != "cache observed" {
		t.Errorf("verdict = %q, want cache observed", r.Verdict)
	}
}

func TestRunCacheNoCacheObserved(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		turn(1, 5000, 0, 0, time.Second),
		turn(2, 5030, 0, 0, 900*time.Millisecond),
	}}
	r := RunCache(context.Background(), cc, "m", 2)
	if r.Verdict != "no cache observed" {
		t.Errorf("verdict = %q, want no cache observed", r.Verdict)
	}
	if r.RequestLevelHit != 0 || r.WarmHitRate != 0 {
		t.Errorf("rates = %v/%v, want 0/0", r.RequestLevelHit, r.WarmHitRate)
	}
}

func TestRunCacheInconclusiveOnFirstTurnFailure(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		{Turn: 1, Err: errors.New("boom")},
	}}
	r := RunCache(context.Background(), cc, "m", 3)
	if r.Verdict != "inconclusive" {
		t.Errorf("verdict = %q, want inconclusive", r.Verdict)
	}
	if r.FailedTurns != 1 {
		t.Errorf("failed = %d, want 1", r.FailedTurns)
	}
}

func TestRunCacheObservedDespiteLateFailure(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		turn(1, 5000, 0, 5000, time.Second),
		turn(2, 5030, 4970, 0, 340*time.Millisecond),
		{Turn: 3, Err: errors.New("boom")},
	}}
	r := RunCache(context.Background(), cc, "m", 3)
	if r.Verdict != "cache observed" {
		t.Errorf("verdict = %q, want cache observed", r.Verdict)
	}
	if r.FailedTurns != 1 || r.WarmTurns != 1 {
		t.Errorf("failed/warm = %d/%d, want 1/1", r.FailedTurns, r.WarmTurns)
	}
}

func TestFormatCacheReport(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		turn(1, 5000, 0, 5000, 1200*time.Millisecond),
		turn(2, 5030, 4970, 0, 340*time.Millisecond),
		turn(3, 5060, 5000, 0, 320*time.Millisecond),
	}}
	r := RunCache(context.Background(), cc, "m", 3)
	out := FormatCacheReport(r)
	for _, want := range []string{
		"stub:cache", "3 turns", "Turn", "read%",
		"Session hit rate: 66.1%", "Warm turns: 98.8%",
		"Cold 1.2s vs warm p50 340ms", "Verdict: cache observed", "Failed: 0/3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n%s", want, out)
		}
	}
}

func TestCacheJSON(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		turn(1, 5000, 0, 5000, 1200*time.Millisecond),
		turn(2, 5030, 4970, 0, 340*time.Millisecond),
		{Turn: 3, Err: errors.New("boom")},
	}}
	r := RunCache(context.Background(), cc, "m", 3)
	j := r.CacheJSON("m", "http://x", "chat")
	if j.APIFormat != "chat" || j.Model != "m" || j.Verdict != "cache observed" {
		t.Errorf("metadata wrong: %+v", j)
	}
	if j.SessionHitRate <= 0 || j.WarmHitRate <= 0 || j.ColdTotalMS != 1200 || j.WarmTotalP50MS != 340 {
		t.Errorf("rates/ms wrong: %+v", j)
	}
	if len(j.Turns) != 3 {
		t.Fatalf("got %d JSON turns, want 3", len(j.Turns))
	}
	if j.Turns[1].Miss != 60 || j.Turns[1].Cached != 4970 {
		t.Errorf("turn 2 JSON = %+v, want miss=60 cached=4970", j.Turns[1])
	}
	if j.Turns[2].Error == "" || j.FailedTurns != 1 {
		t.Errorf("failed turn not surfaced: %+v", j)
	}
}
```

Check the expected format strings before running: `SessionHitRate = 9970/15090 = 0.6607...` → `66.1%`; `WarmHitRate = 9970/10090 = 0.9881...` → `98.8%`; `ColdTotal.Round(time.Millisecond)` of 1.2s prints `1.2s`; `WarmTotalP50` = 340ms (nearest-rank p50 of {340,320} is index 1 = 340ms).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/ -run 'TestRunCache|TestFormatCache|TestCacheJSON' -v`
Expected: FAIL — package does not compile (`undefined: RunCache`).

- [ ] **Step 3: Create the runner aggregation** — `internal/runner/cache.go`

```go
package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"llm-api-test/internal/registry"
)

// CacheReport is the aggregated outcome of one cache session.
type CacheReport struct {
	CaseID string
	Turns  []registry.CacheTurn

	SessionHitRate  float64 // ΣCached / ΣPrompt over all turns
	WarmHitRate     float64 // turns 2..N only
	RequestLevelHit float64 // warm turns with Cached > 0 / warm turns executed
	CachedTokens    int64
	PromptTokens    int64
	WarmTurns       int
	ColdTotal       time.Duration
	WarmTotalP50    time.Duration
	FailedTurns     int
	Verdict         string // cache observed | no cache observed | inconclusive
}

// RunCache runs one cache session and aggregates the per-turn observations.
func RunCache(ctx context.Context, cc registry.CacheCase, model string, turns int) CacheReport {
	obs := cc.RunSession(ctx, model, turns)
	r := CacheReport{CaseID: cc.ID(), Turns: obs}
	var warmTotals []time.Duration
	var allCached, allPrompt, warmCached, warmPrompt int64
	warmHits := 0
	for _, t := range obs {
		if t.Err != nil {
			r.FailedTurns++
			continue
		}
		allCached += int64(t.Cached)
		allPrompt += int64(t.PromptTokens)
		if t.Turn == 1 {
			r.ColdTotal = t.Total
			continue
		}
		warmCached += int64(t.Cached)
		warmPrompt += int64(t.PromptTokens)
		warmTotals = append(warmTotals, t.Total)
		r.WarmTurns++
		if t.Cached > 0 {
			warmHits++
		}
	}
	r.CachedTokens = allCached
	r.PromptTokens = allPrompt
	if allPrompt > 0 {
		r.SessionHitRate = float64(allCached) / float64(allPrompt)
	}
	if warmPrompt > 0 {
		r.WarmHitRate = float64(warmCached) / float64(warmPrompt)
	}
	if r.WarmTurns > 0 {
		r.RequestLevelHit = float64(warmHits) / float64(r.WarmTurns)
		r.WarmTotalP50 = durationSummary(warmTotals).P50
	}
	r.Verdict = cacheVerdict(r, warmHits)
	return r
}

// cacheVerdict classifies the session outcome (design.md "Verdicts").
func cacheVerdict(r CacheReport, warmHits int) string {
	if warmHits > 0 {
		return "cache observed"
	}
	if r.FailedTurns == 0 {
		return "no cache observed"
	}
	return "inconclusive"
}

// FormatCacheReport renders the text report (design.md "cache").
func FormatCacheReport(r CacheReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s  (%d turns, non-streamed)\n", r.CaseID, len(r.Turns))
	b.WriteString("    Turn  prompt  cached  written  miss  read%   total\n")
	for _, t := range r.Turns {
		if t.Err != nil {
			fmt.Fprintf(&b, "    %d    error: %v\n", t.Turn, t.Err)
			continue
		}
		miss := t.PromptTokens - t.Cached - t.CacheWrite
		readPct := 0.0
		if t.PromptTokens > 0 {
			readPct = 100 * float64(t.Cached) / float64(t.PromptTokens)
		}
		fmt.Fprintf(&b, "    %-4d %-7d %-7d %-8d %-5d %6.1f%% %8s\n",
			t.Turn, t.PromptTokens, t.Cached, t.CacheWrite, miss, readPct, t.Total.Round(time.Millisecond))
	}
	fmt.Fprintf(&b, "    Session hit rate: %.1f%% (cached %d / prompt %d)\n",
		100*r.SessionHitRate, r.CachedTokens, r.PromptTokens)
	fmt.Fprintf(&b, "    Warm turns: %.1f%% token-weighted, %.0f/%d request-level\n",
		100*r.WarmHitRate, r.RequestLevelHit*float64(r.WarmTurns), r.WarmTurns)
	fmt.Fprintf(&b, "    Cold %s vs warm p50 %s\n",
		r.ColdTotal.Round(time.Millisecond), r.WarmTotalP50.Round(time.Millisecond))
	fmt.Fprintf(&b, "    Verdict: %s\n", r.Verdict)
	fmt.Fprintf(&b, "    Failed: %d/%d\n", r.FailedTurns, len(r.Turns))
	return b.String()
}
```

- [ ] **Step 4: Add the JSON report types** — `internal/runner/json.go`, after `BenchmarkJSONReport` and its `JSON` method:

```go
// CacheJSONTurn is one session turn in JSON form.
type CacheJSONTurn struct {
	Turn         int   `json:"turn"`
	PromptTokens int   `json:"prompt_tokens"`
	Cached       int   `json:"cached"`
	CacheWrite   int   `json:"cache_write"`
	Miss         int   `json:"miss"`
	TotalMS      int64 `json:"total_ms"`
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
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runner/ -run 'TestRunCache|TestFormatCache|TestCacheJSON' -v`
Expected: PASS. If `TestFormatCacheReport` string expectations differ (e.g. `Cold 1.2s`), adjust the expectations to the actual output, not the code.

- [ ] **Step 6: Verify nothing else broke**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/runner/cache.go internal/runner/cache_test.go internal/runner/json.go
git commit -m "feat(cache): runner aggregation, verdicts, and JSON report

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 5: cache subcommand + integration tests

**Files:**
- Create: `internal/cmd/cache.go`
- Modify: `internal/cmd/root.go`
- Modify: `internal/cmd/cmd_test.go` (extend `apiMockHandler`, add 3 tests)

**Interfaces:**
- Consumes: `registry.CacheCase` / `Params` (Task 1), `messages.CacheCase` / `chat.CacheCase` (Tasks 2-3), `runner.RunCache` / `CacheReport` / `CacheJSONReport` / `WriteJSON` (Task 4), `resolveFormats` (existing, `internal/cmd/compatibility.go`), `loadConfig` / `debugWriter` (existing, `internal/cmd/root.go`).

- [ ] **Step 1: Write the failing integration tests** — extend `internal/cmd/cmd_test.go`

Extend `apiMockHandler`: add cache-turn counters and cache branches. The cache branch must come before the existing `len(req.Tools) > 0` branches (cache requests carry both tools and a system component):

```go
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
			// ... existing tool-call branch, unchanged
```

```go
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
			// ... existing tool-use branch, unchanged
```

Add the tests at the end of `internal/cmd/cmd_test.go`:

```go
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
	for _, turns := range []string{"0", "99"} {
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
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cmd/ -run 'TestCache' -v`
Expected: FAIL — root command does not know `cache` (`unknown command "cache" for "llm-api-test"`).

- [ ] **Step 3: Create the cache command** — `internal/cmd/cache.go`

```go
package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
	"llm-api-test/internal/runner"
)

var cacheTurns int

// newCacheCmd builds the cache hit-rate test command (session-shaped).
func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Run cache hit-rate tests (session-shaped)",
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runCache(cmd)
			return nil
		},
	}
	cmd.Flags().StringVar(&apiFormat, "api-format", "all", "API format to test: all, chat, messages")
	cmd.Flags().IntVar(&cacheTurns, "turns", 8, "session turns (1-8)")
	return cmd
}

// runCache runs the cache session for the selected formats and models.
func runCache(cmd *cobra.Command) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	if noStream {
		fmt.Fprintln(errOut, "--no-stream does not apply to cache: sessions are always non-streamed")
		return 2
	}
	if cacheTurns < 1 || cacheTurns > len(cases.CacheQuestions) {
		fmt.Fprintf(errOut, "turns must be between 1 and %d\n", len(cases.CacheQuestions))
		return 2
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "config: %v\n", err)
		return 2
	}
	formats, ok := resolveFormats(errOut)
	if !ok {
		return 2
	}

	p := registry.Params{Config: cfg, Debug: debugWriter()} // Stream ignored: cache is always non-streamed
	ctx, cancel := context.WithTimeout(context.Background(), cacheTimeout())
	defer cancel()

	var jsonReports []runner.CacheJSONReport
	code := 0
	for _, f := range formats {
		if f.Cache == nil {
			continue // format has no cache test (responses)
		}
		cc := f.Cache(p)
		for mi, m := range cfg.Models {
			if mi > 0 {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "base_url: %s  model: %s\n", cfg.BaseURL, m)
			fmt.Fprintf(out, "turns=%d\n\n", cacheTurns)

			rep := runner.RunCache(ctx, cc, m, cacheTurns)
			jsonReports = append(jsonReports, rep.CacheJSON(m, cfg.BaseURL, f.Name))
			fmt.Fprintln(out, runner.FormatCacheReport(rep))
			if rep.FailedTurns > 0 {
				code = 1
			}
		}
	}
	if outPath != "" {
		if err := runner.WriteJSON(outPath, jsonReports); err != nil {
			fmt.Fprintf(errOut, "write --out %q: %v\n", outPath, err)
			return 2
		}
	}
	return code
}

// cacheTimeout gives each turn up to 120s, plus a floor of 10 minutes.
func cacheTimeout() time.Duration {
	t := time.Duration(cacheTurns) * 120 * time.Second
	if t < 10*time.Minute {
		t = 10 * time.Minute
	}
	return t
}
```

- [ ] **Step 4: Wire the command** — `internal/cmd/root.go`:

```go
	root.AddCommand(newCompatibilityCmd(), newLatencyCmd(), newThroughputCmd(), newCacheCmd(), newListCmd())
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cmd/ -run 'TestCache' -v`
Expected: PASS.

- [ ] **Step 6: Verify nothing else broke**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass (existing `TestCompatibilityAllPass`, `TestLatencyRun`, etc. must still pass — the new mock branches only match cache-shaped requests, which those tests never send).

- [ ] **Step 7: Commit**

```bash
git add internal/cmd/cache.go internal/cmd/root.go internal/cmd/cmd_test.go
git commit -m "feat(cache): cache subcommand with integration tests

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: docs — README, design notes, skill

**Files:**
- Modify: `README.md` (add `### Cache hit rate` section)
- Modify: `docs/design.md` (align cache section with the implemented interface)
- Modify: `.claude/skills/llm-api-test/SKILL.md` (add cache command section)

- [ ] **Step 1: README** — insert a section between the Benchmarks section and `### Exit codes` (around `README.md:148`):

```markdown
### Cache hit rate

The `cache` command runs a session-shaped cache hit-rate test: a stable
prefix (system prompt + tool definitions) plus a conversation history that
grows one turn at a time, mirroring how agent clients (Claude Code, Codex)
actually use prompt caching.

```bash
./llm-api-test cache                        # chat + messages
./llm-api-test cache --api-format messages
./llm-api-test cache --turns 5              # shorter session
```

Cache sessions are always non-streamed. The report shows per-turn
cached/written tokens, session and warm-turn hit rates, and a verdict:
`cache observed`, `no cache observed`, or `inconclusive`.
```

- [ ] **Step 2: Design notes** — in `docs/design.md`'s cache section, make three edits so the doc matches the implemented interface:

1. The interface block: `RunSession(ctx context.Context, model string, turns int) []CacheTurn` (add the `turns` parameter and the "session stops at the first failed turn" note).
2. The text-report example header `cache-messages  (8 turns, non-streamed)` → `messages:cache  (8 turns, non-streamed)` (case IDs follow the `<format>:<name>` convention).
3. In the text-report example, turn 1's miss is 0 (everything up to the breakpoint is written, so nothing is a miss): change line 1 to `prompt 5234  cached 0  written 5234  miss 0  0.0%  1.24s`.

- [ ] **Step 3: Skill** — in `.claude/skills/llm-api-test/SKILL.md`, insert a `### Cache hit rate` subsection after the `### Benchmark` subsection (before `### Reading results`):

```markdown
### Cache hit rate

```bash
./llm-api-test cache -c <config> --api-format chat|messages|all --turns 8
```

- Session-shaped: a stable prefix (system prompt + tool definitions) with a
  history growing one turn at a time — mirrors how Claude Code (explicit
  `cache_control` breakpoints) and Codex (automatic prefix cache) actually
  use caching. Repeated identical requests would not represent real agent
  traffic.
- Always non-streamed. Reports per-turn cached/written tokens, session and
  warm-turn hit rates, and a verdict (`cache observed` / `no cache
  observed` / `inconclusive`). `no cache observed` is a valid result — it is
  exactly what a proxy that strips `cache_control` looks like.
- `chat` needs no cache parameters (automatic cache); `messages` uses three
  `cache_control: ephemeral` breakpoints (system, last tool, last history
  message). v1 excludes `responses`.
```

- [ ] **Step 4: Verify docs render** — check the three files for accidental markdown breakage (nested code fences) and that `git diff` shows only the intended lines.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/design.md .claude/skills/llm-api-test/SKILL.md
git commit -m "docs: document the cache command in README, design notes, skill

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 7: final verification

- [ ] **Step 1: Full checks**

Run: `gofmt -l . && go vet ./... && go test ./... && make build`
Expected: `gofmt -l` prints nothing; vet clean; all tests pass; binary builds.

- [ ] **Step 2: Manual smoke against the mock** (no API key needed)

Run: `./llm-api-test list` — Expected: still lists 17 cases, no `cache` entries (cache is a command, not a case).
Run: `./llm-api-test cache --help` — Expected: flags `--api-format` and `--turns` shown.

- [ ] **Step 3: Review the diff**

Run: `git status --short` and `git log --oneline -7`
Expected: 6 feature commits on `feat/cache-test`, clean tree.

- [ ] **Step 4 (optional, user decision): real-API smoke** — NOT part of this plan's gates. After the user approves, one can run e.g. `./llm-api-test cache -c config.deepseek.yaml --api-format chat --turns 3` against a real endpoint to see live hit rates. Costs a few cheap requests; ask before running.
