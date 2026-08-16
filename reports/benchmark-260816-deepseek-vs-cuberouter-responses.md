# DeepSeek Official vs Cuberouter — Responses API Latency

Date: 2026-08-16

- API surface: Responses API only (`POST /responses`)
- Prompt: pong (latency-focused, single-token response)
- Load: 10 iterations × 5 concurrency = 50 requests per endpoint
- Model: deepseek-v4-flash (pinned on both endpoints with `-m` for apples-to-apples)
- Note: DeepSeek official was benchmarked twice. Run 1 hit a transient server-side stall (see Stability); the main comparison uses run 2 (clean). Cuberouter ran once.

## Results

| Endpoint | p50 Total | p95 Total | p99 Total | min | max | TTFB p50 | Streaming | Failures |
|---|---|---|---|---|---|---|---|---|
| DeepSeek official | 1.216s | 1.469s | 1.587s | 912ms | 1.587s | 141ms | Yes (immediate first byte) | 0/50 |
| Cuberouter | 1.407s | 1.732s | 2.203s | 1.029s | 2.203s | 1.107s | No (first byte after ~1.1s) | 0/50 |

Elapsed: DeepSeek 13.4s · Cuberouter 16.6s · RPS: 3.7 vs 3.0 req/s

## Speed gap

### vs baseline (deepseek official, clean run)

| Endpoint | p50 Total | p95 Total | p99 Total | TTFB p50 |
|---|---|---|---|---|
| DeepSeek (baseline) | 1.216s | 1.469s | 1.587s | 141ms |
| Cuberouter | 1.407s (**1.16×**) | 1.732s (**1.18×**) | 2.203s (**1.39×**) | 1.107s (**7.9×**) |

DeepSeek wins at every percentile: 191ms (13.6%) faster at the median, 263ms (15.2%) at p95, 616ms (28%) at p99. The gap widens toward the tail.

### Streaming quality (TTFB vs TTFT)

- **DeepSeek** — genuine incremental streaming: first byte at 141ms, first token at 1.215s, total 1.216s. The client sees output almost immediately.
- **Cuberouter** — effectively buffered: TTFB 1.107s ≈ TTFT 1.381s (only 274ms between first byte and completion). The first byte is ~8× slower, so interactive rendering of the response feels much worse than the Total numbers suggest.

## Stability

- **DeepSeek:** run 2 clean 50/50 (max 1.587s). Run 1 had a severe transient stall: progress froze at 34/50 for 3m20s, then again at 38/50 for ~31s — 2 of 50 requests took 31.9s and 200.6s (p95 31.97s, p99 3m20.6s, run elapsed 4m3s). Zero failures in both runs — the stalls were slow generations, not errors.
- **Cuberouter:** 50/50 OK in its single run, max 2.203s, no stalls observed.

The DeepSeek tail is real but rare: 4% of requests in one sample, 0% in the re-run minutes later. Cuberouter showed no tail at all.

## Summary

- **DeepSeek official is faster across the board when healthy**: 13–28% faster Total at every percentile, 7.9× faster TTFB (true streaming vs buffered ~1.1s first byte), 24% higher RPS.
- **Cuberouter is the predictable one**: no tail in its sample (max 2.2s), but slower at the median and it buffers instead of streaming.
- **The official endpoint has tail risk**: one run had a multi-minute stall on 2/50 requests; a re-run was clean. Not systematic, but real — and invisible in median-only benchmarks.

### Recommendation

- For latency-sensitive interactive use (streaming UX, request-per-user): **deepseek official**, accepting rare multi-minute stalls.
- For batch/automation where predictability beats peak speed, or when you need the proxy: **cuberouter** — the p50 penalty is only ~16%, and you trade true streaming for a stable tail.
- If tail latency on the official endpoint matters, sample with a larger N (e.g. 200+ requests) before trusting either side.
