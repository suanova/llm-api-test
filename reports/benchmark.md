# Deepseek Offical vs Astraflow vs Cuberouter

Date: 2026-07-31

- API surface: Responses API only (`POST /responses`)
- Prompt: pong (latency-focused, single-token response)
- Load: 10 iterations × 5 concurrency = 50 requests per endpoint
- Model: deepseek-v4-flash (same model on all three endpoints for apples-to-apples comparison)

## Results

| Endpoint | p50 Total | p95 Total | p99 Total | min | max | TTFB p50 | Streaming | Failures |
|---|---|---|---|---|---|---|---|---|
| Deepseek (baseline) | 908ms | 1.131s | 1.187s | 563ms | 1.187s | 113ms | Yes (true streaming) | 0/50 |
| Astraflow | 2.003s | 2.793s | 3.073s | 1.678s | 3.073s | 1.313s | Yes (late streaming) | 5/50 ⚠️ |
| Cuberouter | 2.035s | 2.983s | 3.329s | 1.535s | 3.329s | 1.926s | Marginal (buffered) | 0/50 |

Elapsed: Deepseek 9.8s · Astraflow 26.1s · Cuberouter 24.9s


## Speed gap

### vs baseline (deepseek official)

Both proxies add ~2.2× overhead at the median:

| Endpoint | p50 Total | Overhead vs baseline |
|---|---|---|
| Deepseek (baseline) | 908ms | — |
| Astraflow | 2.003s | **2.2× slower** |
| Cuberouter | 2.035s | **2.2× slower** |

### Astraflow vs Cuberouter

Statistically a tie. p50 differs by ~32ms (1.6%). Astraflow has a marginally tighter tail (p99 3.07s vs 3.33s).

### Streaming quality (TTFB vs TTFT)

- **Deepseek** — genuine incremental streaming: first byte at 113ms, first token at 907ms.
- **Astraflow** — streams but late: first byte 1.31s, first token 2.00s.
- **Cuberouter** — effectively non-streaming: TTFB 1.93s ≈ TTFT 2.03s (buffers the whole response before emitting).

## Stability

- **Deepseek (baseline):** 50/50 OK, clean.
- **Cuberouter:** 50/50 OK, clean. Stable but no real streaming.
- **Astraflow:** 5/50 flagged "FAILED" — **but these are HTTP 200 responses, not errors.** deepseek-v4-flash returns a `reasoning` output item (`"type":"reasoning"`), which the benchmark's parser does not recognize as valid output text. This is a **response-format/parsing incompatibility**, not a service outage — the upstream served all 50 requests.

## Summary

- **Both proxies roughly double latency** vs the deepseek official API (~2.2× at p50). Cuberouter ≈ Astraflow on speed.
- **Deepseek official is the clear winner** — fastest and streams properly (TTFB 113ms).
- **Cuberouter is the most stable proxy** (0 failures) but does not truly stream (buffers full response).
- **Astraflow streams** but has a parsing incompatibility with deepseek-v4-flash's `reasoning` output items (5/50 marked failed despite HTTP 200) — a client/interop gap to fix, not an upstream outage.

### Recommendation

- For lowest latency and real streaming: use the **deepseek official API** directly.
- If a proxy is required: **Astraflow and Cuberouter are equivalent on speed**; choose Cuberouter for cleanest stability, or Astraflow if you need streaming (pending the `reasoning`-output parsing fix).
