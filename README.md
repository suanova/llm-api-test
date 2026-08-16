# llm-api-test

A small Go CLI that tests LLM API compatibility and benchmarks latency/throughput
against OpenAI-compatible and Anthropic-compatible endpoints. It speaks the raw
HTTP wire format — no SDK — so it exercises what a compatible server actually
accepts and returns.

## What it tests

Each API format has a set of compatibility cases with stable IDs
(`<format>:<name>`). All requests are streamed by default (`--no-stream`
disables streaming); "accepted" means the endpoint returns 2xx and a usable
response.

### Chat Completions (`--api-format chat`, `POST /v1/chat/completions`)

| Case | Feature under test | Assertion |
|---|---|---|
| `chat:basic` | Chat Completions supported | returns an assistant message |
| `chat:system-message` | system message | accepted and followed |
| `chat:response_format` | `response_format` (`json_object`) | accepted and content is JSON |
| `chat:seed` | `seed` param | accepted (2xx + output) |
| `chat:tool-call` | custom function tools | emits `tool_calls` with parseable arguments |

### Responses API (`--api-format responses`, `POST /responses`)

| Case | Feature under test | Assertion |
|---|---|---|
| `responses:basic` | Responses API supported | returns output text |
| `responses:instructions` | `instructions` param | accepted and followed |
| `responses:reasoning` | `reasoning.effort` / `reasoning.summary` | accepted (2xx + output) |
| `responses:text.format` | `text.format` (`json_schema`) | accepted and output is schema-conformant JSON |
| `responses:text.verbosity` | `text.verbosity` param | accepted (2xx + output) |
| `responses:prompt_cache_key` | `prompt_cache_key` param | accepted |
| `responses:tool-call` | custom function tools | emits `function_call` output item with parseable args |

### Anthropic Messages API (`--api-format messages`, `POST /v1/messages`)

| Case | Feature under test | Assertion |
|---|---|---|
| `messages:basic` | Messages API supported | returns a text content block |
| `messages:system` | top-level `system` param | accepted and followed |
| `messages:thinking` | `thinking` (extended thinking) | accepted (2xx + output) |
| `messages:cache_control` | `cache_control` on system blocks | accepted |
| `messages:tool-use` | custom function tools | emits `tool_use` content block with parseable input |

Tool cases use a custom `function` tool (`get_weather`) rather than built-in
tools, so they work against any compatible endpoint.

## Install / build

```bash
go build -o llm-api-test ./cmd/llm-api-test
# or: make build
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

### Benchmarks

The `latency` and `throughput` commands run `iterations` waves of
`concurrency` parallel streamed requests and report percentiles
(p50/p95/p99/min/max).

```bash
# latency benchmark (prompt: "Reply with exactly the word: pong")
./llm-api-test latency

# throughput benchmark (long, thorough prompt)
./llm-api-test throughput

# only Chat Completions; custom iterations/concurrency
./llm-api-test throughput --api-format chat --iterations 10 --concurrency 5
```

`latency` reports TTFB/TTFT/Total; `throughput` additionally reports
TPOT, TPS, and token counts. With `--no-stream`, TTFB/TTFT/TPOT are omitted
(they require streaming) and `throughput` falls back to tokens/s from usage.
Benchmark requests cap generation at 4096 tokens (`max_completion_tokens` for
chat, `max_output_tokens` for responses, `max_tokens` for messages) so a
thorough prompt cannot run unbounded; the benchmark context timeout (120s per
request, minimum 10 minutes) is the backstop. While a benchmark runs, a live
status line is printed to stderr (`[benchmark] elapsed 5s, 3/10 requests
completed`) and cleared when the report prints.

### Exit codes

- `0` — all cases passed
- `1` — one or more cases failed
- `2` — config or argument error

## Output

Compatibility output is grouped by API format, with a header showing the
endpoint and model:

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

### Benchmark output

Latency mode:

```
base_url: https://api.openai.com/v1  model: gpt-4o-mini
iterations=10  concurrency=5  prompt=Reply with exactly the word: pong

  chat:benchmark  (10 iters x 5 concurrency = 50 requests)
    TTFB:  p50=180ms p95=410ms p99=590ms min=120ms max=620ms
    TTFT:  p50=210ms p95=450ms p99=620ms min=150ms max=680ms
    Total: p50=380ms p95=620ms p99=890ms min=250ms max=950ms
    RPS:    11.9 req/s
    Failed: 0/50
    Elapsed: 4.2s
```

Throughput mode adds TPOT/TPS/Tokens/Output:

```
  chat:benchmark  (10 iters x 5 concurrency = 50 requests)
    TTFB:  p50=180ms p95=410ms p99=590ms min=120ms max=620ms
    TTFT:  p50=210ms p95=450ms p99=620ms min=150ms max=680ms
    Total: p50=1.2s p95=2.1s p99=2.8s min=900ms max=3.1s
    TPOT:  p50=18.5ms p95=24.2ms p99=32.1ms min=12.0ms max=38.5ms
    TPS:   p50=54.0  p95=41.0  p99=31.0  min=26.0  max=83.0 tok/s
    Tokens: completion p50=52 p95=58 p99=64  prompt p50=15 p95=15 p99=15
    Output: avg_content=234 bytes  avg_chunks=52
    RPS:    11.9 req/s
    Failed: 0/50
    Elapsed: 15.2s
```

`-o report.json` writes machine-readable JSON reports (one object per
model/format run); see `docs/design.md` for the schema.

## Project layout

```
cmd/llm-api-test/main.go     # entrypoint
internal/cmd/                # cobra CLI: compatibility / latency / throughput / list
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
