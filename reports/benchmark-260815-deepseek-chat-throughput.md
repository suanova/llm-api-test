# DeepSeek Official — Chat API Throughput Benchmark

Date: 2026-08-15

- API surface: Chat Completions (`POST /v1/chat/completions`, streamed)
- Prompt: long thorough prompt (TCP handshake / HTTP/2-3 / network diagnostics), fixed for throughput mode
- Load: 3 iterations × 3 concurrency = 9 requests
- Model: deepseek-v4-flash (official API, `https://api.deepseek.com/v1`)
- Generation cap: 4096 tokens requested; **ignored by DeepSeek** (actual completions 8k–21k tokens)

## Results

| Metric | p50 | p95 | p99 | min | max |
|---|---|---|---|---|---|
| TTFB (first byte) | 143ms | 243ms | 243ms | 123ms | 243ms |
| TTFT (first content token) | 1m44.8s | 2m53.1s | 2m53.1s | 29.4s | 2m53.1s |
| Total | 2m20.7s | 3m34.2s | 3m34.2s | 1m15.2s | 3m34.2s |
| TPOT | 0ms* | 26ms | 42ms | 0ms | 222ms |
| TPS | 101.3 tok/s | 106.5 | 106.5 | 98.4 | 106.5 |
| Completion tokens | 14,130 | 21,267 | 21,267 | 7,983 | 21,267 |
| Prompt tokens | 195 | 195 | 195 | 195 | 195 |

\* TPOT p50 = 0ms: per-chunk pacing measurement — the first content chunk after the reasoning phase delivers a burst of tokens, so inter-chunk gaps on the first chunk measure as 0. p95 (26ms) / p99 (42ms) reflect steady-state token pacing.

Elapsed: 8m46.6s wall clock · aggregate throughput 247.5 tok/s (9 requests in parallel) · RPS 0.017 req/s · Failed 0/9

## Analysis

- **True streaming, long thinking phase.** TTFB is 143ms p50 — bytes start flowing immediately. But the first *content* token lands at 1m44.8s p50: deepseek-v4-flash emits its reasoning phase first, so TTFT is dominated by thinking time, not transport.
- **Sustained generation ≈ 101 tok/s** (p50 TPS 101.3, very tight range 98.4–106.5). Steady-state token pacing is fast (TPOT p95 26ms/chunk), but the reasoning preamble is the real latency cost.
- **The 4096-token cap is ignored.** Completions ran 7,983–21,267 tokens (p50 14,130); the request-level context timeout is the actual bound, which is why a 3×3 run takes ~9 minutes.
- **Stability:** 9/9 requests OK, 0 failures, no retries observed. Clean run.

## Summary

- DeepSeek official chat throughput: **~101 tok/s sustained per request**, extremely consistent (98–107 tok/s across all 9 requests).
- Per-request latency is dominated by reasoning, not generation: ~75% of the 2m21s p50 total is time-to-first-content-token.
- The tool's generation cap is best-effort against this provider — throughput runs on deepseek-v4-flash should budget ~2–3.5 min per request at this prompt length.

### Recommendation

- deepseek-v4-flash chat is a solid throughput workhorse: 0 failures, ~100 tok/s steady, true streaming. For throughput measurement, treat **TPS (~101 tok/s)** as the stable headline number and **TTFT (p50 ~1m45s)** as the latency that scales with reasoning depth.
- If lower TTFT matters, prefer a model without an extended reasoning phase or shorten the prompt — generation cost is not the lever; thinking time is.
