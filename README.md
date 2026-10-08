# llm-api-test

A small Go CLI that tests LLM API compatibility and benchmarks against OpenAI-compatible and Anthropic-compatible endpoints. It speaks the raw HTTP wire format — no SDK — so it exercises what a compatible server actually accepts and returns.

## What each test measures & costs

Estimated token spend per default run. Rough figures: English ≈ 4 chars per
token, prompts differ slightly per model/tokenizer, and `usage` reporting
varies by provider — the authoritative numbers are the provider's own usage
fields, echoed in the `-o report.json` output.

| Test | Default load | Prompt | Gen cap | Est. tokens per run |
|---|---|---|---|---|
| `compatibility` | 17 cases × 1 request = 17 requests per model (5 chat / 7 responses / 5 messages); a format's remaining cases are skipped once its `basic` case fails | tiny prompts (`"Reply with exactly the word: pong"`, `"Say: hello"`, `"What is the weather in Shanghai?"`) | none for chat/responses (the provider's default); 1024 for messages, 4096 for `messages:thinking` (1024 of it a thinking budget) | ~1-2k total: most cases are a few dozen tokens, `responses:reasoning` (effort `high`, uncapped) and `messages:thinking` dominate. Seconds to ~a minute; the whole run is capped at 10 minutes |
| `latency` | 10 iters × 5 concurrency = 50 requests | `"Reply with exactly the word: pong"` | 4096 (unused: the model just replies "pong") | ~1k total; finishes in seconds |
| `throughput` | 3 iters × 3 concurrency = 9 requests | ~3000-word article task (short instruction, long generation) | 4096 \* | ~40-60k output; ~3-5 min |
| `cache` | 1 session, 8 turns, history grows each turn | ~2k-token system prompt + 170-line knowledge base + 6 tool schemas, then one question per turn | 300 per turn | ~50k input per session (~90% served from cache on warm turns, billed at the provider's cache-read rate), ~2.4k output max |
| `soak` | 1 hour: ~120 short turns + 12 long turns (`--long-every 10`) | short: `"Reply with exactly the word: ok"`; long: ~400-500-word writing task | 64 / 512 | ~10-15k total; the cost is an hour of run time, not tokens |

The rows are per default run: `chat` only, except for `compatibility`, which
defaults to all three formats. `--api-format all` widens a benchmark command
to 3 formats and a `cache`/`soak` run to 2 (each format is its own request
set or session, run in sequence), so multiply accordingly — and every model
in the config multiplies again.

\* Generation caps are best-effort: some providers (e.g. DeepSeek v4) ignore
them, so a thorough prompt can run longer and output more than the cap — the
per-request timeout is the real bound. The same caveat applies to the soak
long-turn budget.

What each test is for:

- **`compatibility`** — whether the endpoint implements each format's feature
  surface (params accepted, response shape as specified), as pass/fail rather
  than numbers. One short request per case, so it is the cheap test; run it
  first to find out which formats are worth benchmarking at all.
- **`latency`** — per-request round-trip and time-to-first-token under light
  load. Cheap enough to run repeatedly; use it to compare endpoints
  (TTFB/TTFT/Total percentiles).
- **`throughput`** — steady-state generation speed (TPOT/TPS) on a long
  output. The expensive one: one 3×3 run ≈ 50k+ output tokens, about the
  same as an hour of `soak`. Run it when you need generation-speed numbers,
  not as a default check.
- **`cache`** — whether the endpoint's prompt caching actually hits on a
  session-shaped workload (system + tools + growing history). Input tokens
  dominate, but warm turns should be mostly cache reads; a run is cheap on
  providers with low cache-read pricing.
- **`soak`** — long-window gateway/proxy stability (drops, stalls, 5xx/429
  bursts, latency drift). Token cost is trivial; what it costs you is the
  run time (an hour by default).

## Install / build

```bash
go build -o llm-api-test ./cmd/llm-api-test # or: make build
```

## Configure

Copy the sample config and edit it:

```bash
cp config.example.yaml config.yaml
```

```yaml
base_url: https://api.openai.com/v1    # /v1 is added automatically if missing
models:
  - gpt-4o-mini
api_key: sk-REPLACE_ME
```

To test several models against the same endpoint, list them; each model runs
the full case set in turn.

Any field can also be set via environment variables, which override the file:

- `OPENAI_BASE_URL`
- `OPENAI_MODEL` — comma-separated for multiple models, e.g. `gpt-4o-mini,gpt-4.1-mini`
- `OPENAI_API_KEY`

So you can keep the key out of the file entirely:

```bash
export OPENAI_API_KEY=sk-...
```

CLI flags override config: `--base-url`, `--api-key`, `-m/--models`.

## Run

Common flags (all subcommands):

```
      --base-url string      base URL, overrides config `base_url`
      --api-key string       API key, overrides config `api_key`
  -m, --models stringArray   models to test, overrides config `models`
  -c, --config string        path to config file (default "config.yaml")
      --no-stream            disable streaming (default: requests are streamed)
      --http-debug           dump HTTP request/response to stderr (sensitive headers redacted)
  -o, --out string           write the JSON report to this file; text report still goes to stdout
  -v, --verbose              print raw response body on each case
```

### Compatibility

Each API format has a set of compatibility cases with stable IDs (`<format>:<name>`). All requests are streamed by default (`--no-stream` disables streaming); "accepted" means the endpoint returns 2xx and a usable response.

#### Chat Completions (`--api-format chat`, `POST /v1/chat/completions`)

| Case | Feature under test | Assertion |
|---|---|---|
| `chat:basic` | Chat Completions supported | returns an assistant message |
| `chat:system-message` | system message | accepted and followed |
| `chat:response_format` | `response_format` (`json_object`) | accepted and content is JSON |
| `chat:seed` | `seed` param | accepted (2xx + output) |
| `chat:tool-call` | custom function tools | emits `tool_calls` with parseable arguments |

#### Responses API (`--api-format responses`, `POST /responses`)

| Case | Feature under test | Assertion |
|---|---|---|
| `responses:basic` | Responses API supported | returns output text |
| `responses:instructions` | `instructions` param | accepted and followed |
| `responses:reasoning` | `reasoning.effort` / `reasoning.summary` | accepted (2xx + output) |
| `responses:text.format` | `text.format` (`json_schema`) | accepted and output is schema-conformant JSON |
| `responses:text.verbosity` | `text.verbosity` param | accepted (2xx + output) |
| `responses:prompt_cache_key` | `prompt_cache_key` param | accepted |
| `responses:tool-call` | custom function tools | emits `function_call` output item with parseable args |

#### Anthropic Messages API (`--api-format messages`, `POST /v1/messages`)

| Case | Feature under test | Assertion |
|---|---|---|
| `messages:basic` | Messages API supported | returns a text content block |
| `messages:system` | top-level `system` param | accepted and followed |
| `messages:thinking` | `thinking` (extended thinking) | accepted (2xx + output) |
| `messages:cache_control` | `cache_control` on system blocks | accepted |
| `messages:tool-use` | custom function tools | emits `tool_use` content block with parseable input |

Tool cases use a custom `function` tool (`get_weather`) rather than built-in tools, so they work against any compatible endpoint.

```bash
# all cases across all API formats (default --api-format all)
./llm-api-test compatibility

# only one API format
./llm-api-test compatibility --api-format chat

# subset selection
./llm-api-test compatibility chat:seed            # one exact test
./llm-api-test compatibility seed                 # every test named seed
./llm-api-test compatibility chat                 # all chat tests

# list available tests, grouped by API format
./llm-api-test list
./llm-api-test list --api-format chat
```

#### Output

Compatibility output is grouped by API format, with a header showing the endpoint and model:

```
base_url: https://api.openai.com/v1  model: gpt-4o-mini
OpenAI Chat Completions (POST /v1/chat/completions) Compatibility

  chat:basic              PASS  assistant message present
  chat:system-message     PASS  system message followed
  chat:response_format    PASS  JSON response
  chat:seed               PASS  seed accepted
  chat:tool-call          PASS  tool_calls returned
```

Failures print the reason instead of the pass detail:

```
  chat:response_format    FAIL  request failed: HTTP 400: {"error":...}
```

### Benchmarks

The `latency` and `throughput` commands run `iterations` waves of
`concurrency` parallel streamed requests and report percentiles
(p50/p90/p95/p99/min/max).

```bash
# latency benchmark (prompt: "Reply with exactly the word: pong")
./llm-api-test latency

# throughput benchmark (long, thorough prompt)
./llm-api-test throughput

# only Chat Completions; custom iterations/concurrency
./llm-api-test throughput --api-format chat --iterations 10 --concurrency 5

# long-context tier run: ~8k-token filler prompt, output capped at ~1/100
./llm-api-test throughput --input-tokens 8000 --max-output-tokens 80
```

Three flags shape the load:

- `--input-tokens N` replaces the built-in prompt with a deterministic filler
  prompt of about N tokens (English ≈ 1.33 tokens per word). The provider's
  `usage` reports the real input size; the report labels the run
  `filler:<N>`. This backs long-context tier runs that mirror a vendor
  benchmark's fixed input sizes.
- `--max-output-tokens M` caps generation per request (default 4096). Combined
  with `--input-tokens` it pins a fixed input:output ratio (e.g. 100:1).
- `--reasoning-effort E` passes a reasoning-effort value to reasoning-capable
  formats (chat `reasoning_effort`, responses `reasoning.effort`); empty
  leaves the provider default. `none` disables thinking on reasoning models —
  needed for tier runs, because reasoning tokens otherwise consume the whole
  output cap and no answer is produced.

A **rate-controlled mode** (`--rps N --duration T`) turns either benchmark
into a sustained-load capacity run: requests are offered at N/s in open loop
(ticks do not wait for completions) for T, with in-flight capped at
`--max-in-flight` (default 1024; ticks above the cap are counted as *shed*
instead of silently throttling the offered load). The report adds
offered/achieved rate, sent/shed/completed, HTTP 429/5xx tallies, and the
in-flight distribution; requests still running when the drain budget expires
are counted as *incomplete*.

```bash
# sustained 20 req/s for 3 minutes (RPM-style capacity check)
./llm-api-test latency --rps 20 --duration 3m --input-tokens 1000 --max-output-tokens 10 --reasoning-effort none
```

Both default to `--api-format chat` and run one format per invocation: the
formats are separate endpoints (often separate translation paths on a proxy),
so each one you add multiplies the run rather than refining a single number.
`--api-format all` adds responses and messages, at 3x the requests.

- `latency` reports TTFB/TTFT/Total;
- `throughput` additionally reports TPOT, TPS, token counts, input tok/s, and
  observed cache reads (all from `usage`; streamed requests ask for it via
  `stream_options.include_usage`).

With `--no-stream`, TTFB/TTFT/TPOT are omitted (they require streaming) and `throughput` falls back to tokens/s from usage.

Benchmark requests cap generation at `--max-output-tokens` (default 4096; `max_completion_tokens` for chat, `max_output_tokens` for responses, `max_tokens` for messages) so a thorough prompt cannot run unbounded; the benchmark context timeout (120s per request, minimum 10 minutes) is the backstop. While a benchmark runs, a live status line is printed to stderr (`[benchmark] elapsed 5s, 3/10 requests completed`) and cleared when the report prints.

#### Output

Latency mode:

```
base_url: https://api.openai.com/v1  model: gpt-4o-mini
iterations=10  concurrency=5  prompt=pong

  chat:benchmark  (10 iters x 5 concurrency = 50 requests)
    TTFB:  p50=180ms p90=310ms p95=410ms p99=590ms min=120ms max=620ms
    TTFT:  p50=210ms p90=340ms p95=450ms p99=620ms min=150ms max=680ms
    Total: p50=380ms p90=520ms p95=620ms p99=890ms min=250ms max=950ms
    RPS:    11.9 req/s
    Failed: 0/50
    Elapsed: 4.2s
```

Throughput mode adds TPOT/TPS/Tokens/Output and the usage-derived input and
cache lines:

```
  chat:benchmark  (10 iters x 5 concurrency = 50 requests)
    TTFB:  p50=180ms p90=310ms p95=410ms p99=590ms min=120ms max=620ms
    TTFT:  p50=210ms p90=340ms p95=450ms p99=620ms min=150ms max=680ms
    Total: p50=1.2s p90=1.8s p95=2.1s p99=2.8s min=900ms max=3.1s
    TPOT:  p50=18.5ms p90=22.0ms p95=24.2ms p99=32.1ms min=12.0ms max=38.5ms
    TPS:   p50=54.0  p90=45.5  p95=41.0  p99=31.0  min=26.0  max=83.0 tok/s
    Tokens: completion p50=52 p90=56 p95=58 p99=64  prompt p50=15 p90=15 p95=15 p99=15
    Output: avg_content=234 bytes  avg_chunks=52
    Input:  1234.5 tok/s (from usage)
    Cache:  4970/5030 prompt tokens (98.8%) read from cache
    RPS:    11.9 req/s
    Failed: 0/50
    Elapsed: 15.2s
```

`-o report.json` writes machine-readable JSON reports (one object per model/format run), including a per-request array (`requests[]`) with the vendor-comparable per-request figures (tpot_ms, otps, token counts, cache reads) so percentile and SLO math can be recomputed from the raw data; see `docs/design.md` for the schema.

### Cache hit rate

The `cache` command runs a session-shaped cache hit-rate test: a stable
prefix (system prompt + tool definitions) plus a conversation history that
grows one turn at a time, mirroring how agent clients (Claude Code, Codex)
actually use prompt caching.

```bash
./llm-api-test cache                        # chat (default)
./llm-api-test cache --api-format messages  # Anthropic-style cache_control
./llm-api-test cache --api-format all       # both sessions
./llm-api-test cache --turns 5              # shorter session
```

Cache sessions are always non-streamed. The report shows per-turn
cached/written tokens, session and warm-turn hit rates, and a verdict:
`cache observed`, `no cache observed`, or `inconclusive`.

### Soak (long-duration stability)

The `soak` command keeps sending streamed requests over a long stretch of
real time (default 1 hour) and classifies every failure, to surface gateway
and proxy stability problems that short runs miss: mid-stream cuts, silent
stalls, sustained-load 5xx/429s, and latency drift.

```bash
# one hour, one turn every 30s (chat by default)
./llm-api-test soak -c config.astraflow.yaml

# the Anthropic-style path (own session, so its own hour)
./llm-api-test soak -c config.astraflow.yaml --api-format messages

# shorter probe run: 10 minutes, faster cadence
./llm-api-test soak -c config.astraflow.yaml --duration 10m --interval 15s

# continuous interaction only (no idle probes)
./llm-api-test soak --idle-gaps ""
```

- Turns are independent short streamed requests (a long-generation turn runs
  every 10th by default via `--long-every`). A failed turn does not abort the
  session — the next turn shows whether the failure was transient.
- **Idle probes** (default `--idle-gaps 1m,5m,10m`, spread evenly across the
  run) pause traffic for the given stretches, then report the first turn
  after the gap. A proxy that kills idle keep-alive connections shows up as a
  failure or latency spike on resume. The soak client holds idle connections
  for hours so the default 90s client-side reap does not mask the proxy's own
  timeout.
- **Stall watchdog**: a stream that produces no event for `--stall` (default
  60s) is torn down and reported as `stall`.
- Every failure is classified: `conn` (connection reset/refused),
  `timeout`, `stall`, `dropped` (stream ended before its completion marker),
  `http-429`, `http-5xx`, `http-4xx`, `other`. The report shows per-class
  tallies, a failure timeline with timestamps, idle-probe results, and
  latency per time bucket (drift detection).

Report and exit-code conventions match the other commands: `-o report.json`
writes JSON (one object per model/format), and any failed turn exits `1`.

## Project layout

```
cmd/llm-api-test/main.go     # entrypoint
internal/cmd/                # cobra CLI: compatibility / latency / throughput / cache / soak / list
internal/chat/               # Chat Completions format: client, cases, benchmark
internal/responses/          # Responses format: client, cases, benchmark
internal/messages/           # Anthropic Messages format: client, cases, benchmark
internal/openai/             # shared OpenAI HTTP plumbing (used by chat + responses)
internal/registry/           # shared types: Format, CompatCase, BenchmarkCase, Metrics
internal/runner/             # format-agnostic runner: RunCompat, RunBenchmark, reports
internal/cases/              # shared helpers: Pass/Fail, MustJSON, benchmark prompts
internal/config/             # config.yaml + env overrides
internal/httpx/              # HTTP debug dump helpers
internal/sse/                # minimal SSE event parser
docs/design.md               # detailed design spec (CLI, JSON report schema)
config.example.yaml          # sample config
```

## Adding a compatibility case

1. Create a file in the format package (e.g. `internal/chat/`) implementing
   `registry.CompatCase` (`ID`, `Name`, `Desc`, `Run(ctx, model)`). The struct
   closes over the format's `*Client`.
2. Append it to the ordered case list returned by the format's `Format()`
   descriptor in the package's `cases.go` (the first entry is the basic test;
   when it fails, the runner skips the rest).
3. No CLI changes needed — `compatibility`, `list`, and the JSON report pick
   the new case up automatically.

## Adding a benchmark case

1. Implement `registry.BenchmarkCase` (`ID`, `Desc`, `Run(ctx, model, prompt)`)
   in the format package.
2. Wire it into `Format().Benchmark` in the package's `cases.go`.
3. `latency`/`throughput` run one case per format; add your format to the
   composition root (`formats` in `internal/cmd/compatibility.go`) if it is new.

## Adding an API surface

1. Create a package (e.g. `internal/messages/`) with a client, per-case files,
   a benchmark case, and a `Format()` descriptor — follow `internal/chat/`.
   `messages` shows the non-OpenAI pattern: `x-api-key` auth and its own HTTP
   client; `chat` and `responses` share `internal/openai`.
2. Register the descriptor in the `formats` slice in
   `internal/cmd/compatibility.go` (the composition root).
3. The CLI picks it up automatically: `--api-format <name>`, `list`, and
   `compatibility <name>:<case>` selection.
