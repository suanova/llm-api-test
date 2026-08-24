---
name: llm-api-test
description: Run LLM provider API compatibility tests and latency/throughput benchmarks with this repo's llm-api-test CLI, and generate comparison reports (markdown + static HTML page with charts). Use whenever the user asks to test LLM API compatibility, benchmark LLM latency/throughput (延迟/吞吐), compare multiple providers or endpoints (对比/哪个快/差距), or generate a test/benchmark report — even if they don't name the tool. Covers both OpenAI-style endpoints (chat/responses) and Anthropic-style endpoints (messages), including proxies (Astraflow, Cuberouter) and official APIs (DeepSeek).
---

# llm-api-test: provider compatibility & benchmark reports

Run the repo's CLI against one or more provider configs, then produce a
report the user can share. Two typical jobs:

1. **Single provider**: test compatibility and/or benchmark, output one report.
2. **Multi-provider comparison**: run the SAME case set / benchmark on several
   configs, compare side by side, output a comparison report + chart page.

Work from the conversation, but if anything is unspecified, ask (AskUserQuestion
is fine) rather than guessing — especially mode, providers, and benchmark load.

## Decision flow (clarify what's unstated)

1. **Mode**: compatibility, latency/throughput benchmark, or both?
   - compatibility: default `--api-format all`. A provider's compatibility
     result is the whole point — run all formats unless told otherwise.
   - benchmark: default `--api-format chat`. **Throughput is opt-in and
     expensive** — never run the `throughput` command unless the user asked
     for it. `latency` is cheap; offer it freely.
2. **Providers**: discover `config.*.yaml` files in the repo root, show the
   user the list, and ask which to test. The user may also name configs
   directly. Config files hold real API keys — never print their contents.
3. **api-format ↔ config mapping**: OpenAI-style configs (base_url ending in
   `/v1`, e.g. `config.deepseek.yaml`) serve `chat` and `responses`;
   Anthropic-style configs (e.g. `config.deepseek.anthropic.yaml`, base_url
   `.../anthropic`) serve only `messages` (x-api-key auth). If the user asks
   to test a provider's `messages` surface, find the anthropic config — the
   openai-style one will not work for it, and vice versa.
4. **Benchmark load**: latency default is the CLI default (10 iterations × 5
   concurrency). Throughput default is **3 × 3** (`--iterations 3 --concurrency 3`)
   unless the user specifies; honor any explicit `--iterations/--concurrency`
   the user gives. Before running throughput, say what it costs: one
   deepseek-v4-flash request ≈ 1 min and ~6k tokens (3000-word article
   prompt), so 3×3 ≈ 3-5 min and ~50k+ tokens. Get a nod first if this
   wasn't already clear.

## Running the CLI

Build once, run many times: `make build` (or `go build -o llm-api-test
./cmd/llm-api-test`).

### Compatibility

```bash
./llm-api-test compatibility -c <config>                 # all formats
./llm-api-test compatibility -c <config> --api-format chat
./llm-api-test compatibility -c <config> responses:instructions   # one case
./llm-api-test compatibility -c <config> seed            # every case named seed
./llm-api-test list                                       # available cases (17)
```

Requests stream by default; `--no-stream` disables. `-o report.json` writes
the JSON report (text still goes to stdout).

### Benchmark

```bash
./llm-api-test latency -c <config> --api-format chat --iterations 10 --concurrency 5
./llm-api-test throughput -c <config> --api-format chat --iterations 3 --concurrency 3
```

- The prompt is fixed per command (`latency`: pong; `throughput`: a fixed
  "write a ~3000-word article" prompt, ~4-6k output tokens) — there is no
  `--prompt` flag.
- Always pass `-o <path>.json` so you can build comparison tables and the
  chart page from structured data instead of parsing the text report.
- A live progress line goes to **stderr** (`[benchmark] elapsed 5s, 3/10
  requests completed`); the report prints to stdout. Capture both: redirect
  stdout to the raw text report, let stderr show progress (or 2>/dev/null
  when running in the background).
- Benchmark requests cap generation at 4096 tokens, but some providers
  (e.g. DeepSeek v4) ignore the cap field — the benchmark context timeout
  (120s/request, min 10 min) is the real backstop. A run that looks hung is
  usually just a long generation: check the stderr progress line before
  concluding anything.

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
- A live progress line goes to **stderr** (`[cache] elapsed 5s, 3/8 turns
  completed`); the report prints to stdout. Same for `compatibility`
  (`[compat] ... cases completed`) — a silent stderr means the run is
  genuinely stuck, not just slow.

### Reading results

- `PASS/FAIL` lines: read the detail text — `FAIL` on a 2xx/3xx response is
  an incompatibility (e.g. unsupported param, unexpected response shape),
  not necessarily an outage.
- Compatibility exact-match cases (`chat:system-message`,
  `responses:instructions`, `messages:system`, the `name` field in
  `response_format`/`text.format`) assert exact model output and can flake
  even at temperature 0 on reasoning models. If one fails while its siblings
  pass, re-run that case once before reporting it as a failure.
- Benchmark JSON: per-request p50/p95/p99/min/max for TTFB/TTFT/Total,
  TPOT/TPS/Tokens in throughput mode, RPS, Failed, ElapsedMS.

## Reports

Two deliverable formats, both produced for every job:

### 1. Markdown report (always)

Save to `reports/` with a dated filename (`reports/benchmark-YYMMDD-<desc>.md`,
or `reports/compat-YYMMDD-<desc>.md`). Follow the house style in
`reports/benchmark.md` and `benchmark-report-260731.md`:

```markdown
# <Provider A> vs <Provider B> — <API surface>

Date: YYYY-MM-DD
- API surface: ... (`POST /responses`)
- Prompt: pong (latency) / long thorough prompt (throughput)
- Load: N iterations × N concurrency = N requests per endpoint
- Model: <model> (same model across endpoints for apples-to-apples)

## Results
| Endpoint | p50 Total | p95 Total | p99 Total | min | max | TTFB p50 | Streaming | Failures |
|---|---|---|---|---|---|---|---|---|
Elapsed: ...

## Speed gap
- vs baseline: ×N.2 slower at p50
- pairwise: p50 differs by Xms (Y%)
- streaming quality: TTFB vs TTFT (true streaming vs buffered)

## Stability
- per-endpoint: X/Y OK; describe any FAILED (error vs parsing incompatibility)

## Summary
- bullets: what won, what's stable, what's a real problem vs a client interop gap

### Recommendation
- concrete: which endpoint to use when, and why
```

For single-provider runs, keep the same sections minus the comparison ones
(Results + a short analysis of what passed/failed and why).

### 2. Static HTML page with charts (always, when benchmark data exists)

Publish an Artifact (via the Artifact tool) titled like the report, showing:
- bar charts comparing p50 (and p95/p99) Total per endpoint/model — inline
  SVG or mermaid, no external libs (Artifacts are strictly static, CSP-blocked)
- the results table, stability notes, and the recommendation

Before writing the page, load the `artifact-design` skill to calibrate the
design effort. Keep the favicon emoji stable across updates (e.g. ⚡ for
benchmark reports).

Start the file with `<meta charset="utf-8">` (right before `<title>`). The
page is opened directly (file://, a static server) as often as it is viewed
as an Artifact, and without the declaration non-ASCII text (e.g. Chinese
reports) renders as mojibake.

## Pitfalls (learned the hard way)

- **DeepSeek ignores `max_completion_tokens`** — benchmark caps are
  best-effort; the timeout and progress line are the real bounds.
- **`json_schema` response_format is not universally supported** —
  DeepSeek only accepts `json_object`; the tool's chat `response_format` and
  responses `text.format` cases differ deliberately.
- **messages needs the anthropic config** — running `messages` against an
  OpenAI-style config fails auth/endpoint.
- **Streamed vs plain responses differ in shape** — a parser bug in the tool
  (not the provider) can surface only against a real API. If a case fails
  with a weird detail (e.g. empty function name), check `-v` raw output and
  the tool's stream parser before blaming the provider.
- **A FAIL with `HTTP 4xx` from a "supported" feature** usually means the
  provider doesn't implement that feature — quote the error in the report.
- **HTML pages need an explicit `<meta charset="utf-8">`** — files opened
  directly (not through the Artifact wrapper) otherwise get no charset, and
  non-ASCII (e.g. Chinese) text silently renders as mojibake. Only caught at
  browser-verification time, so declare it from the start.
