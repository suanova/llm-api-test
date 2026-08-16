# DeepSeek Official — Chat API Throughput Benchmark

Date: 2026-08-16

- API surface: Chat Completions (`POST /v1/chat/completions`, streamed)
- Prompt: long thorough prompt (history of the internet, ~3000-word article), fixed for throughput mode
- Load: 3 iterations × 3 concurrency = 9 requests
- Model: deepseek-v4-flash (official API, `https://api.deepseek.com/v1`)
- Generation cap: 4096 tokens requested; **ignored by DeepSeek** (actual completions 4,976–7,581 tokens)

## Results

| Metric | p50 | p95 | p99 | min | max |
|---|---|---|---|---|---|
| TTFB (first byte) | 159ms | 246ms | 246ms | 124ms | 246ms |
| TTFT (first content token) | 5.8s | 14.1s | 14.1s | 3.1s | 14.1s |
| Total | 50.0s | 1m2.2s | 1m2.2s | 41.1s | 1m2.2s |
| TPOT | 0ms* | 22ms | 45ms | 0ms | 175ms |
| TPS | 121.9 tok/s | 126.2 | 126.2 | 119.8 | 126.2 |
| Completion tokens | 5,966 | 7,581 | 7,581 | 4,976 | 7,581 |
| Prompt tokens | 119 | 119 | 119 | 119 | 119 |

\* TPOT p50 = 0ms: per-chunk pacing measurement — the first content chunk after the reasoning phase delivers a burst of tokens, so inter-chunk gaps on the first chunk measure as 0. p95 (22ms) / p99 (45ms) reflect steady-state token pacing.

Elapsed: 2m43.9s wall clock · aggregate throughput 331.9 tok/s (9 requests in parallel) · RPS 0.055 req/s · Failed 0/9

## Analysis

- **True streaming, short reasoning phase this run.** TTFB is 159ms p50 — bytes start flowing immediately; the first *content* token lands at 5.8s p50, i.e. thinking time was brief this time, unlike the 1m45s TTFT seen in the 2026-08-15 run. Reasoning depth varies run to run on deepseek-v4-flash.
- **Sustained generation ≈ 122 tok/s** (p50 TPS 121.9, very tight range 119.8–126.2). Steady-state token pacing is fast (TPOT p95 22ms/chunk).
- **The 4096-token cap is ignored.** Completions ran 4,976–7,581 tokens (p50 5,966); the request-level context timeout is the actual bound.
- **Generation dominates total time** — Total p50 50.0s vs TTFT p50 5.8s: ~44s of the median request is token generation. Aggregate throughput across the 9 concurrent streams: ~332 tok/s.
- **Stability:** 9/9 requests OK, 0 failures, no retries observed. Clean run.

## Summary

- DeepSeek official chat throughput: **~122 tok/s sustained per request**, extremely consistent (119.8–126.2 across all 9 requests); ~332 tok/s aggregate at 3×3 concurrency.
- 3×3 run completed in 2m44s wall clock — faster than the 8m47s of the 08-15 run because completions were shorter (5k–7.6k vs 8k–21k tokens); per-request budget varies with reasoning depth and generation length.
- The tool's generation cap is best-effort against this provider — budget roughly 1–1.5 min per request at this prompt length.

### Recommendation

- deepseek-v4-flash chat is a solid throughput workhorse: 0 failures, ~122 tok/s steady, true streaming. Treat **TPS (~122 tok/s)** as the stable headline number.
- TTFT (p50 ~5.8s here, but 1m45s on the previous run) is dominated by reasoning depth, not transport — if time-to-first-token matters, watch reasoning length rather than generation speed.
