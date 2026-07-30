package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"llm-api-test/internal/httpx"
	"llm-api-test/internal/runner"
	"llm-api-test/internal/sse"
)

// StreamResult holds the timing and token metrics from a single streaming
// Responses API request.
type StreamResult struct {
	Metrics    runner.StreamMetrics
	Text       string // accumulated output text
	Raw        []byte // final raw event (for debugging)
	ChunkCount int    // number of SSE content delta chunks received
}

// StreamResponse sends a streaming Responses API request and returns timing
// metrics parsed from the SSE stream. The request has stream=true added
// automatically.
func (c *Client) StreamResponse(ctx context.Context, req *Request) (*StreamResult, error) {
	// Force stream=true.
	req.SetExtra("stream", true)

	body, err := encodeRequest(req)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	if c.DebugWriter != nil {
		httpx.DumpRequest(c.DebugWriter, httpReq, body)
	}

	requestStart := time.Now()
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	ttfb := time.Since(requestStart)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		if c.DebugWriter != nil {
			httpx.DumpResponse(c.DebugWriter, resp, raw)
		}
		return nil, &httpx.APIError{Status: resp.StatusCode, Body: raw}
	}

	// Read the full response body so we can handle both SSE streaming and
	// non-streaming (some servers ignore stream=true and return plain JSON).
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// Some servers return 200 OK with a non-SSE error body (e.g. unsupported
	// model). Detect this before trying to parse SSE events.
	// SSE-formatted error bodies are handled inside the event loop.
	if !bytes.HasPrefix(bytes.TrimSpace(bodyBytes), []byte("data:")) && isErrorBody(bodyBytes) {
		return nil, &httpx.APIError{Status: resp.StatusCode, Body: bodyBytes}
	}

	// Try SSE parsing first.
	parser := sse.NewParser(bytes.NewReader(bodyBytes))
	var (
		contentBuf strings.Builder
		ttft       time.Duration
		hasTTFT    bool
		usage      responsesStreamUsage
		finalRaw   []byte
		chunkCount int
		sseEvents  int
	)

	for {
		event, err := parser.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse sse: %w", err)
		}

		if string(event.Data) == "[DONE]" {
			break
		}

		finalRaw = event.Data
		sseEvents++

		// Detect SSE error events (some servers return 200 OK with an error
		// in the SSE stream, e.g. {"code":"InvalidParameter","message":"..."}).
		if isErrorBody(event.Data) {
			return nil, &httpx.APIError{Status: resp.StatusCode, Body: event.Data}
		}

		// Debug: dump the first few SSE events so we can see the server's
		// actual format.
		if c.DebugWriter != nil && sseEvents <= 10 {
			fmt.Fprintf(c.DebugWriter, "<<< SSE event #%d: %s\n", sseEvents, httpx.Truncate(string(event.Data), 500))
		}

		// Try to extract delta text using multiple possible event shapes.
		deltaText := extractDeltaText(event.Data)
		if deltaText != "" {
			if !hasTTFT {
				ttft = time.Since(requestStart)
				hasTTFT = true
			}
			contentBuf.WriteString(deltaText)
			chunkCount++
		}

		// Extract usage from events.
		var evt map[string]json.RawMessage
		if json.Unmarshal(event.Data, &evt) == nil {
			if typeVal, ok := evt["type"]; ok {
				var typeStr string
				if json.Unmarshal(typeVal, &typeStr) == nil {
					if typeStr == "response.completed" {
						if respRaw, ok := evt["response"]; ok {
							extractUsageFromResponse(respRaw, &usage)
						}
					}
					maybeExtractUsage(evt, &usage)
				}
			}
		}
	}

	totalTime := time.Since(requestStart)

	// Fallback: if SSE parsing found no content deltas, the server may have
	// returned SSE events in an unrecognized format, or it may have returned a
	// non-streaming JSON body (ignoring stream=true). Try parsing as a regular
	// Responses API response.
	if chunkCount == 0 {
		var resp Response
		if json.Unmarshal(bodyBytes, &resp) == nil {
			text := outputTextFromResponse(resp.Output)
			if text != "" {
				if !hasTTFT {
					ttft = totalTime
					hasTTFT = true
				}
				contentBuf.WriteString(text)
				// For a non-streaming response, we can't count individual tokens
				// via chunks, so set chunkCount to 1 to indicate content was found.
				chunkCount = 1
				finalRaw = bodyBytes
			}
			// Extract usage from non-streaming response.
			if resp.Usage != nil {
				var u responsesStreamUsage
				if json.Unmarshal(resp.Usage, &u) == nil {
					usage = u
				}
			}
		}
	}

	// Token counting priority:
	// 1. usage.OutputTokens (most accurate, from response.completed event)
	// 2. content chunk count (each SSE delta ≈ 1 token)
	// 3. character-based heuristic (len/4)
	tokens := usage.OutputTokens
	if tokens <= 0 {
		tokens = chunkCount
	}
	if tokens <= 0 {
		tokens = countApproxTokens(contentBuf.String())
	}

	if !hasTTFT {
		ttft = totalTime
	}

	metrics := runner.StreamMetrics{
		TTFB:             ttfb,
		TTFT:             ttft,
		TotalTime:        totalTime,
		CompletionTokens: tokens,
		PromptTokens:     usage.InputTokens,
		ContentLen:       contentBuf.Len(),
		ChunkCount:       chunkCount,
	}

	if c.DebugWriter != nil {
		httpx.DumpResponse(c.DebugWriter, resp, finalRaw)
	}

	return &StreamResult{
		Metrics:    metrics,
		Text:       contentBuf.String(),
		Raw:        finalRaw,
		ChunkCount: chunkCount,
	}, nil
}

// extractDeltaText tries multiple strategies to pull the content delta text
// from a Responses API SSE event:
//
//  1. Standard shape:  {"type":"response.output_text.delta","delta":"Hello"}
//  2. Nested shape:    {"type":"response.output_text.delta","delta":{"text":"Hello"}}
//  3. Alt field name:  {"type":"...","text":"Hello"}
//  4. Generic: any event with a non-empty string in common content fields
func extractDeltaText(data json.RawMessage) string {
	// Strategy 1: top-level "delta" as a string.
	var flat struct {
		Delta string `json:"delta"`
	}
	if json.Unmarshal(data, &flat) == nil && flat.Delta != "" {
		return flat.Delta
	}

	// Strategy 2: nested "delta" with a "text" field.
	var nested struct {
		Delta struct {
			Text string `json:"text"`
		} `json:"delta"`
	}
	if json.Unmarshal(data, &nested) == nil && nested.Delta.Text != "" {
		return nested.Delta.Text
	}

	// Strategy 3: top-level "text" field (some servers use this instead of "delta").
	var text struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(data, &text) == nil && text.Text != "" {
		return text.Text
	}

	// Strategy 4: raw map — try common content fields at top level.
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) == nil {
		for _, key := range []string{"content", "output", "message"} {
			if rawVal, ok := raw[key]; ok {
				var s string
				if json.Unmarshal(rawVal, &s) == nil && s != "" {
					return s
				}
			}
		}
	}

	return ""
}

// extractUsageFromResponse parses the "response" object from a
// response.completed event to find usage.
func extractUsageFromResponse(respRaw json.RawMessage, usage *responsesStreamUsage) {
	var resp struct {
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(respRaw, &resp) != nil || resp.Usage == nil {
		return
	}
	var u responsesStreamUsage
	if json.Unmarshal(resp.Usage, &u) == nil {
		if u.InputTokens > 0 {
			usage.InputTokens = u.InputTokens
		}
		if u.OutputTokens > 0 {
			usage.OutputTokens = u.OutputTokens
		}
	}
}

// maybeExtractUsage checks for top-level usage fields in any event.
func maybeExtractUsage(evt map[string]json.RawMessage, usage *responsesStreamUsage) {
	if usageRaw, ok := evt["usage"]; ok {
		var u responsesStreamUsage
		if json.Unmarshal(usageRaw, &u) == nil {
			if u.InputTokens > 0 {
				usage.InputTokens = u.InputTokens
			}
			if u.OutputTokens > 0 {
				usage.OutputTokens = u.OutputTokens
			}
		}
	}
}

type responsesStreamUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func countApproxTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return len(s) / 4
}

// isErrorBody detects common error response shapes even when the HTTP status
// is 200. Some servers return errors like:
//
//	{"code":"InvalidParameter","message":"Unsupported model: ..."}
//	{"error":{"message":"...","type":"..."}}
func isErrorBody(body []byte) bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return false
	}
	// {"code": "...", "message": "..."} — common non-OpenAI error format.
	if _, hasCode := raw["code"]; hasCode {
		if _, hasMsg := raw["message"]; hasMsg {
			return true
		}
	}
	// {"error": {...}} — OpenAI-style error.
	if _, hasError := raw["error"]; hasError {
		return true
	}
	return false
}

// isErrorBodyOrSSEError is like isErrorBody but also handles SSE-formatted
// error responses. Some servers return errors like:
//
//	id:1
//	event:error
//	:HTTP_STATUS/400
//	data: {"code":"InvalidParameter","message":"Unsupported model: ..."}
func isErrorBodyOrSSEError(body []byte) bool {
	// Try plain JSON first.
	if isErrorBody(body) {
		return true
	}

	// Scan for "data:" lines and check each one for error patterns.
	// This handles SSE-wrapped error responses.
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimPrefix(line, []byte("data:"))
		data = bytes.TrimSpace(data)
		if isErrorBody(data) {
			return true
		}
	}

	return false
}

// outputTextFromResponse extracts the text content from a non-streaming
// Responses API response's output array. Returns "" if none found.
func outputTextFromResponse(output []OutputItem) string {
	var b strings.Builder
	for _, item := range output {
		if item.Type != "message" || len(item.Content) == 0 {
			continue
		}
		var contents []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(item.Content, &contents); err != nil {
			continue
		}
		for _, c := range contents {
			if c.Type == "output_text" || c.Type == "text" {
				b.WriteString(c.Text)
			}
		}
	}
	return b.String()
}
