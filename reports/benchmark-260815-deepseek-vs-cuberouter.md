# Deepseek Official vs Cuberouter — Responses API Latency

Date: 2026-08-15

- API surface: Responses API only (`POST /responses`)
- Prompt: pong (latency-focused, single-token response)
- Load: 10 iterations × 5 concurrency = 50 requests per endpoint
- Model: deepseek-v4-flash (same model on both endpoints for apples-to-apples)

## Results

| Endpoint | p50 Total | p95 Total | p99 Total | min | max | TTFB p50 | Streaming | Failures |
|---|---|---|---|---|---|---|---|---|
| DeepSeek official (baseline) | 676ms | 999ms | 1.095s | 479ms | 1.095s | 86ms | Yes (true streaming, 2 chunks) | 0/50 |
| Cuberouter | 1.469s | 2.246s | 2.573s | 1.114s | 2.573s | 1.136s | No (buffered, 1 chunk) | 0/50 |

Elapsed: DeepSeek 8.7s · Cuberouter 18.8s · RPS: 5.7 vs 2.7 req/s

## Speed gap

### vs baseline (deepseek official)

| Endpoint | p50 Total | p95 Total | p99 Total | TTFB p50 |
|---|---|---|---|---|
| DeepSeek (baseline) | 676ms | 999ms | 1.095s | 86ms |
| Cuberouter | 1.469s (**2.2×**) | 2.246s (**2.2×**) | 2.573s (**2.3×**) | 1.136s (**13×**) |

Cuberouter adds ~793ms at the median (~2.2× slower Total). The gap widens at the tail (p99 2.35×).

### Streaming quality (TTFB vs TTFT)

- **DeepSeek** — genuine incremental streaming: first byte at 86ms, first token at 672ms, total 676ms. 2 chunks per response.
- **Cuberouter** — effectively non-streaming: TTFB 1.136s ≈ TTFT 1.423s ≈ Total 1.469s (buffers the whole response, emits a single chunk). The TTFB gap alone is 13×.

## Stability

- **DeepSeek (baseline):** 50/50 OK, clean.
- **Cuberouter:** 50/50 OK, clean.

Both endpoints served all 50 requests with zero failures — the comparison is purely about speed and streaming behavior.

## Summary

- **DeepSeek official wins on every metric**: ~2.2× faster Total at p50/p95/p99, 13× faster TTFB, 2.1× higher RPS, half the wall-clock elapsed.
- **Cuberouter buffers instead of streaming**: single-chunk responses mean no incremental output — a real UX difference for interactive use, not just a number.
- **Both are stable** (0/50 failures); the cuberouter cost is latency + buffering, not reliability.

### Recommendation

- For lowest latency and true streaming: use the **deepseek official API** directly.
- Use **cuberouter** only when you need the proxy (routing/aggregation) and can accept ~2.2× total latency with fully buffered responses.
