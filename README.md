# llm-api-test

A small Go CLI that tests LLM API compatibility and benchmarks latency/throughput
against OpenAI-compatible and Anthropic-compatible endpoints. It speaks the raw
HTTP wire format — no SDK — so it exercises what a compatible server actually
accepts and returns.

## What it tests

Select an API surface with `--api` (default: `all`). Available values: `all`, `responses`, `chat`, `messages`.

### Responses API (`--api responses`, `POST /responses`)

| Case | Feature under test | Assertion |
|---|---|---|
| `responses-basic` | Responses API supported | returns output text |
| `responses-stream` | `stream=true` supported | SSE content deltas received |
| `responses-instructions` | `instructions` param | accepted and followed |
| `responses-verbosity` | `text.verbosity` param | accepted (2xx + output) |
| `responses-text-format` | `text.format` (`json_schema`) | accepted and output_text is schema-conformant JSON |
| `responses-prompt-cache-key` | `prompt_cache_key` param | accepted across 2 calls |
| `responses-reasoning` | `reasoning.effort` / `reasoning.summary` | accepted (2xx + output) |
| `responses-tool-call` | custom function tools | emits `function_call` output item with parseable args |

### Chat Completions API (`--api chat`, `POST /v1/chat/completions`)

| Case | Feature under test | Assertion |
|---|---|---|
| `chat-basic` | Chat Completions supported | returns an assistant message |
| `chat-stream` | `stream=true` supported | SSE content deltas received |
| `chat-system-message` | system message | accepted and followed |
| `chat-tool-call` | custom function tools | emits `tool_calls` with parseable arguments |
| `chat-response-format` | `response_format` (`json_schema`) | accepted and content is schema-conformant JSON |
| `chat-seed` | `seed` param | accepted (2xx + output) |

### Anthropic Messages API (`--api messages`, `POST /v1/messages`)

| Case | Feature under test | Assertion |
|---|---|---|
| `messages-basic` | Messages API supported | returns a text content block |
| `messages-stream` | `stream=true` supported | SSE content deltas received |
| `messages-system` | top-level `system` param | accepted and followed |
| `messages-tool-use` | custom function tools | emits `tool_use` content block with parseable input |
| `messages-cache-control` | `cache_control` on system blocks | accepted (2xx + output) |
| `messages-thinking` | `thinking` (extended thinking) | accepted (2xx + output) |

All tool cases use a **custom `function` tool** (`get_weather`) rather than
built-in tools, so they work against any compatible endpoint.

## Install / build

```bash
go build -o llm-api-test ./cmd/llm-api-test
```

## Configure

Copy the sample config and edit it:

```bash
cp config.example.yaml config.yaml
```

```yaml
base_url: https://api.openai.com/v1    # /v1 is added automatically if omitted
models:
  - gpt-4o-mini
api_key: sk-REPLACE_ME
```

To test several models against the same endpoint, list them:

```yaml
base_url: https://api.openai.com/v1    # /v1 is added automatically if omitted
models:
  - gpt-4o-mini
  - gpt-4.1-mini
api_key: sk-REPLACE_ME
```

Each model runs the full case set in turn, with a `=== model: <name> ===`
header and its own summary line.

Any field can also be set via environment variables, which override the file:

- `OPENAI_BASE_URL`
- `OPENAI_MODEL` — comma-separated for multiple models, e.g. `gpt-4o-mini,gpt-4.1-mini`
- `OPENAI_API_KEY`

So you can keep the key out of the file entirely:

```bash
export OPENAI_API_KEY=sk-...
```

## Run

### Compatibility mode (default)

```bash
# all cases across all API surfaces (default --api all)
./llm-api-test run

# only Responses API cases
./llm-api-test run -a responses

# only Chat Completions cases
./llm-api-test run -a chat

# only Anthropic Messages cases
./llm-api-test run -a messages

# a subset
./llm-api-test run responses-basic responses-tool-call
./llm-api-test run -a chat chat-basic chat-tool-call

# override the model(s) from config (repeatable; runs each model in turn)
./llm-api-test run -m gpt-4o-mini -m gpt-4.1-mini

# a single case (one subcommand per case; auto-selects the API surface)
./llm-api-test responses-tool-call
./llm-api-test chat-tool-call

# list all available cases (default: all surfaces)
./llm-api-test list
./llm-api-test list -a responses
./llm-api-test list -a chat
./llm-api-test list -a messages

# print the raw response body for each case on success/failure
./llm-api-test run -v

# dump full HTTP request/response to stderr (sensitive headers redacted)
./llm-api-test run --http-debug

# tee a clean report copy to a file (stdout still shows live progress)
./llm-api-test run -o report.txt
```

### Benchmark mode

Benchmark mode measures streaming latency and throughput using repeated
concurrent SSE requests. It reports percentiles (p50/p95/p99) for TTFB, TTFT,
TPOT, and TPS.

The `--prompt` flag selects the benchmark prompt style:

- **`pong`** (default): sends "Reply with exactly the word: pong" — measures pure latency (TTFB, TTFT, Total). TPOT/TPS are hidden since they are not meaningful for single-token responses.
- **`long`**: sends "Write a short paragraph about the weather." with `max_tokens=256` — measures both latency and throughput (TPOT, TPS, token counts).

```bash
# default: latency-only benchmark (pong prompt)
./llm-api-test run --mode benchmark

# throughput benchmark (long prompt)
./llm-api-test run --mode benchmark --prompt long

# only Chat Completions benchmarks
./llm-api-test run --mode benchmark -a chat

# custom iterations and concurrency
./llm-api-test run --mode benchmark --iterations 20 --concurrency 10

# a single benchmark case
./llm-api-test benchmark-chat-basic

# combine with model and output flags
./llm-api-test run --mode benchmark -m gpt-4o-mini -o bench-report.txt
```

Benchmark config can also go in `config.yaml`:

```yaml
base_url: https://api.openai.com/v1
models:
  - gpt-4o-mini
api_key: sk-REPLACE_ME
benchmark:
  iterations: 10
  concurrency: 5
```

CLI flags (`--iterations`, `--concurrency`) override config values.

### Exit codes

- `0` — all cases passed
- `1` — one or more cases failed
- `2` — config error (missing `base_url` / `model` / `api_key`) or unknown `--api`

## Output

Output is grouped by API surface, with a header showing the endpoint and model:

```
base_url: https://api.openai.com  model: gpt-4o-mini
OpenAI Responses API (POST /responses) Compatibility
  PASS  responses-basic  (runtime=412ms) output_text present
  PASS  responses-tool-call  (runtime=1.2s) function_call to get_weather with args=...
  PASS  2/2 cases passed  (total=1.612s)

OpenAI Chat Completions API (POST /v1/chat/completions) Compatibility
  PASS  chat-basic  (runtime=301ms) assistant message present
  FAIL  chat-response-format  (runtime=289ms) request with `response_format=json_schema` failed: ...
  PASS  1/2 cases passed  (total=590ms)
```

Each case prints one line showing `runtime=` (wall-clock time for that case,
rounded to milliseconds). Each surface ends with a summary line showing
`total=` (sum of per-case runtimes). When testing multiple models, a new
header is printed before each model's results.

### Benchmark output

With `--prompt pong` (default, latency-only):

```
base_url: https://api.openai.com  model: gpt-4o-mini
OpenAI Chat Completions API (POST /v1/chat/completions) Benchmark
  iterations=10  concurrency=5  prompt=pong

  benchmark-chat-basic  (10 iters x 5 concurrency = 50 requests)
    TTFB:  p50=180ms p95=410ms p99=590ms min=120ms max=620ms
    TTFT:  p50=210ms p95=450ms p99=620ms min=150ms max=680ms
    Total: p50=380ms p95=620ms p99=890ms min=250ms max=950ms
    Elapsed: 4.2s
```

With `--prompt long` (throughput):

```
base_url: https://api.openai.com  model: gpt-4o-mini
OpenAI Chat Completions API (POST /v1/chat/completions) Benchmark
  iterations=10  concurrency=5  prompt=long

  benchmark-chat-basic  (10 iters x 5 concurrency = 50 requests)
    TTFB:  p50=180ms p95=410ms p99=590ms min=120ms max=620ms
    TTFT:  p50=210ms p95=450ms p99=620ms min=150ms max=680ms
    Total: p50=1.2s p95=2.1s p99=2.8s min=900ms max=3.1s
    TPOT:  p50=18.5ms p95=24.2ms p99=32.1ms min=12.0ms max=38.5ms
    TPS:   p50=54.0  p95=41.0  p99=31.0  min=26.0  max=83.0 tok/s
    Tokens: completion p50=52 p95=58 p99=64  prompt p50=15 p95=15 p99=15
    Output: avg_content=234 bytes  avg_chunks=52  (content_len/chunks ≈ 4.5 bytes/chunk)
    Elapsed: 15.2s
```

## Project layout

```
cmd/llm-api-test/main.go     # entrypoint
internal/cmd/cmd.go          # cobra CLI: run / list / --api / --model / --mode / --out
internal/config/config.go    # config.yaml + env overrides
internal/apis/apis.go        # API surface registry (responses, chat, ...)
internal/openai/client.go    # raw HTTP client for POST /responses
internal/openai/stream.go    # SSE streaming client for POST /responses
internal/chat/client.go      # raw HTTP client for POST /v1/chat/completions
internal/chat/stream.go      # SSE streaming client for POST /v1/chat/completions
internal/anthropic/client.go # raw HTTP client for POST /v1/messages
internal/anthropic/stream.go # SSE streaming client for POST /v1/messages
internal/sse/sse.go          # minimal SSE event parser
internal/httpx/httpx.go      # shared HTTP helpers (debug dump, error type)
internal/runner/             # Case interface + BenchmarkCase + result/metrics reporting
internal/cases/              # shared helpers (Fail, Pass, ContainsFold, MustJSON)
internal/responses/          # Responses API cases (one file per feature)
internal/chat/               # Chat Completions cases (one file per feature)
internal/anthropic/          # Anthropic Messages cases (one file per feature)
config.example.yaml          # sample config
```

## Adding a compatibility case

1. Create a file in `internal/responses/` or `internal/chat/` implementing
   `runner.Case` (`Name`, `Desc`, `Run(ctx, model)`).
   The struct must hold a `Client` field (set by `All()`).
2. Register it in `All()` in the package's `all.go`.
3. A subcommand named after the case is registered automatically.

## Adding a benchmark case

1. Create a file in the surface package (e.g. `internal/chat/benchmark_foo.go`)
   implementing `runner.BenchmarkCase` (`Name`, `Desc`, `RunBenchmark(ctx, model)`).
   The struct must hold a `Client` field.
2. Register it in the surface's `benchmarkBuild` closure in
   `internal/apis/apis.go`.
3. A subcommand named after the case is registered automatically.

## Adding an API surface

1. Create a client package (e.g. `internal/anthropic/`) following the pattern
   in `internal/openai/client.go` or `internal/chat/client.go`.
2. Create a cases package (e.g. `internal/anthropic/`) with `All(client)` and
   case files.
3. Register the surface in `internal/apis/apis.go` — add an entry to the
   `All` slice with `Name`, `Desc`, and a `build` closure.
4. The CLI picks it up automatically: `--api <name>`, `list -a <name>`, and
   per-case subcommands.
