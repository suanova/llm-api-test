---
name: llm-api-test
description: Run LLM provider API tests with this repo's llm-api-test CLI and generate comparison reports (markdown + static HTML page with charts). The CLI has five runnable test types — compatibility (兼容性), latency (延迟), throughput (吞吐), cache hit-rate (缓存), soak stability (稳定性/长稳) — combinable in any subset in one job. Use whenever the user asks to test LLM API compatibility, benchmark latency/throughput (延迟/吞吐), measure prompt-cache hits (缓存), run soak/stability checks, compare multiple providers or endpoints (对比/哪个快/差距), or generate a test/benchmark report — even if they don't name the tool. Covers both OpenAI-style endpoints (chat/responses) and Anthropic-style endpoints (messages), including proxies (Astraflow, Cuberouter) and official APIs (DeepSeek).
---

# llm-api-test: provider compatibility & benchmark reports

Run the repo's CLI against one or more provider configs, then produce a
report the user can share. Two typical jobs:

1. **Single provider**: run one or more of the five test types (below),
   output one report.
2. **Multi-provider comparison**: run the SAME test types on several configs,
   compare side by side, output a comparison report + chart page.

Work from the conversation, but if anything is unspecified, ask (AskUserQuestion
is fine) rather than guessing — especially test types, providers, and load.

## What you can run — present the full menu

The CLI runs exactly five test types, one subcommand each. When the user
hasn't named the type(s), your reply must present this menu and ask which to
run — never silently narrow to one type, and never invent tests the CLI
can't run (it has no auth-failure, malformed-request, or error-mapping
probes; only the five types below):

| Type | What it answers | API formats | Cost |
|---|---|---|---|
| `compatibility` | does the API implement the feature surface? (17 cases) | chat, responses, messages | cheap |
| `latency` | how fast is a short request (TTFB/TTFT/Total)? | chat, responses, messages | cheap |
| `throughput` | how fast does it generate (tokens/s, long prompt)? | chat, responses, messages | **expensive — opt-in** |
| `cache` | does prompt caching actually hit in a session? | chat, messages | ~1-2 min |
| `soak` | is it stable over a long window (drops, stalls, idle resets)? | chat, messages | **1h+ — never unprompted** |

Types **combine freely** in one job: run the chosen subset as separate
subcommands against the same config(s), each writing its own
`-o <type>-<desc>.json` (a shared `-o` path would be overwritten), then
merge all JSONs into the single report. Exact commands per type below;
token/wall-clock figures in "Cost of a default run".

## Decision flow (clarify what's unstated)

1. **Test types** (pick from the menu above; any subset, combinable): when
   the user hasn't specified, present the menu and ask — don't default
   silently.
   - compatibility: default `--api-format all`. A provider's compatibility
     result is the whole point — run all formats unless told otherwise.
   - benchmark: default `--api-format chat`. **Throughput is opt-in and
     expensive** — never run the `throughput` command unless the user asked
     for it. `latency` is cheap; offer it freely.
   - cache/soak: `chat`/`messages` only (no `responses` in v1).
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

### Cost of a default run

Quote these before running anything on a metered proxy. Rough figures
(English ≈ 4 chars/token); README "What each test measures & costs" has the
full table and caveats.

| Command | Default load | Est. tokens | Wall clock |
|---|---|---|---|
| `latency` | 10×5 = 50 short req | ~1k total | seconds |
| `throughput` | 3×3 = 9 long req | ~40-60k **output** | ~3-5 min |
| `cache` | 8-turn session | ~50k input, mostly cache-hit on warm turns | ~1-2 min |
| `soak` | 1h: ~120 short + 12 long turns | ~10-15k total | 1h (the run time is the cost) |

So the expensive things are `throughput` (≈ same output tokens as an hour of
`soak`) and the hour `soak` ties up — never run either unprompted. Note some
providers (DeepSeek v4) ignore generation caps: `throughput` output can
exceed 4096 tokens/request and take minutes.

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

### Soak (long-duration stability)

```bash
./llm-api-test soak -c <config> --duration 1h --interval 30s   # chat + messages
./llm-api-test soak -c <config> --duration 10m --interval 15s  # quick probe run
./llm-api-test soak --idle-gaps ""                              # no idle probes
```

- Long-window gateway/proxy stability: one short streamed request per turn
  (with idle-probe windows and a longer generation every `--long-every`
  turns) and per-turn failure classification. Wall-clock heavy — a 1h soak is
  ~120 turns of mostly waiting; token cost is trivial.
- Failure classes: `conn`, `timeout`, `stall` (no data for `--stall`),
  `dropped` (stream ended before its completion marker — chat: `[DONE]`/
  `finish_reason`, messages: `message_stop`), `http-429`/`http-5xx`/
  `http-4xx`, `other`. Failed turns do not abort the session.
- Default `--idle-gaps 1m,5m,10m` pause traffic mid-run to expose proxies
  that kill idle keep-alive connections; the first turn after each gap is
  the probe result. Note net/http retries/heals most stale-connection reuse
  silently, so a *visible* probe failure means the proxy truly broke.
- Text report: class tallies, failure timeline, probe results, per-bucket
  latency. Any failed turn → exit 1. `-o report.json` for charting.

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

Save to `reports/` alongside the markdown report (e.g.
`reports/benchmark-YYMMDD-<desc>.html`), showing:
- bar charts comparing p50 (and p95/p99) Total per endpoint/model — inline
  SVG, no external libraries (self-contained; the page is opened directly via
  file:// or a static server)
- the results table, stability notes, and the recommendation

Before writing the page, load the `artifact-design` skill to calibrate the
design effort.

Start the file with `<meta charset="utf-8">` (right before `<title>`). The
page is opened directly (file://, a static server), and without the
declaration non-ASCII text (e.g. Chinese reports) renders as mojibake.

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
  directly otherwise get no charset, and non-ASCII (e.g. Chinese) text
  silently renders as mojibake. Only caught at browser-verification time, so
  declare it from the start.
