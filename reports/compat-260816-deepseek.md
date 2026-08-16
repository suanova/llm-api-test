# DeepSeek (deepseek-v4-flash) — API Compatibility Report

Date: 2026-08-16

- Provider: DeepSeek official API (`api.deepseek.com`)
- Model: deepseek-v4-flash (same model across all surfaces)
- Surfaces tested: OpenAI Chat Completions, OpenAI Responses, Anthropic Messages
- Streaming: enabled (all requests streamed)
- Tool: llm-api-test `compatibility` (all formats)

## Results

| API surface | Endpoint | Cases | Result |
|---|---|---|---|
| chat (OpenAI) | `POST /v1/chat/completions` | 5 | **5/5 PASS** |
| responses (OpenAI) | `POST /responses` | 7 | **7/7 PASS** |
| messages (Anthropic) | `POST /anthropic/v1/messages` | 5 | **5/5 PASS** |
| messages (wrong config) | `POST /v1/messages` via OpenAI-style config | 5 | 0/5 FAIL — HTTP 404 (endpoint mapping, see below) |

### chat (OpenAI Chat Completions) — 5/5 PASS

| Case | Result |
|---|---|
| chat:basic | PASS — assistant message present |
| chat:system-message | PASS — system message followed |
| chat:response_format | PASS — `response_format` (json_object) accepted, JSON returned |
| chat:seed | PASS — `seed` accepted |
| chat:tool-call | PASS — `tool_calls` returned |

### responses (OpenAI Responses) — 7/7 PASS

| Case | Result |
|---|---|
| responses:basic | PASS — output text present |
| responses:instructions | PASS — instructions followed |
| responses:reasoning | PASS — `reasoning.effort` / `reasoning.summary` accepted |
| responses:text.format | PASS — schema-conformant JSON |
| responses:text.verbosity | PASS — `text.verbosity` accepted |
| responses:prompt_cache_key | PASS — `prompt_cache_key` accepted |
| responses:tool-call | PASS — `function_call` output returned |

### messages (Anthropic Messages) — 5/5 PASS

| Case | Result |
|---|---|
| messages:basic | PASS — text content block present |
| messages:system | PASS — system prompt followed |
| messages:thinking | PASS — extended `thinking` accepted |
| messages:cache_control | PASS — `cache_control` accepted |
| messages:tool-use | PASS — `tool_use` content block returned |

## Analysis

- **All three API surfaces are fully compatible with DeepSeek's `deepseek-v4-flash`**: 17/17 cases passed across chat, responses, and messages.
- **The 5 messages FAILs are a config-mapping artifact, not an incompatibility.** `messages` was run against the OpenAI-style config (`config.deepseek.yaml`, `https://api.deepseek.com/v1`) and failed with `HTTP 404`; the 4 sibling cases were skipped ("basic failed"). DeepSeek only serves the Anthropic Messages API on `https://api.deepseek.com/anthropic` (x-api-key auth) — tested with `config.deepseek.anthropic.yaml`, all 5 cases pass. Same pattern as documented for any provider behind two endpoint styles.
- Notable features that work on the Responses surface: `reasoning` (deepseek-v4-flash is a reasoning model), `text.verbosity`, `prompt_cache_key`, and `text.format` (json_schema) — all accepted.
- Notable features that work on the Messages surface: extended `thinking` and `cache_control` — both accepted by DeepSeek's Anthropic-compatible endpoint.
- All exact-match cases (`chat:system-message`, `responses:instructions`, `messages:system`) passed at first attempt; no flakes, no re-runs needed.

## Summary

- DeepSeek official (`deepseek-v4-flash`) is **fully compatible** with the OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages APIs — 17/17 PASS.
- The only FAILs (messages via the `/v1` base URL) are expected: that endpoint simply does not exist; the Anthropic-style surface lives under `/anthropic`. Use the matching config per surface.

### Recommendation

- Use `config.deepseek.yaml` (`api.deepseek.com/v1`) for OpenAI-style `chat` / `responses` consumers.
- Use `config.deepseek.anthropic.yaml` (`api.deepseek.com/anthropic`) for Anthropic-style `messages` consumers (x-api-key auth).
- No compatibility blockers found for any surface — client code written against standard OpenAI/Anthropic SDK shapes should work as-is.
