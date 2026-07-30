package chat

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
// Chat Completions request.
type StreamResult struct {
	Metrics    runner.StreamMetrics
	Content    string     // accumulated content from all content deltas
	ToolCalls  []ToolCall // tool calls from the stream (if any)
	Raw        []byte     // final raw event (for debugging)
	ChunkCount int        // number of SSE content chunks received
}

// StreamChatCompletion sends a streaming Chat Completions request and returns
// timing metrics parsed from the SSE stream. The request must have
// stream=true set (this method adds it automatically).
func (c *Client) StreamChatCompletion(ctx context.Context, req *Request) (*StreamResult, error) {
	// Force stream=true.
	req.SetExtra("stream", true)

	body, err := encodeRequest(req)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + "/chat/completions"
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

	// Parse SSE events.
	parser := sse.NewParser(bytes.NewReader(bodyBytes))
	var (
		contentBuf  strings.Builder
		ttft        time.Duration
		hasTTFT     bool
		usage       chatUsage
		toolCalls   []ToolCall
		finalRaw    []byte
		chunkCount  int // count of content delta chunks (rough token proxy)
		sseEvents   int
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

		// Debug: dump the first few SSE events so we can see the server's
		// actual format.
		if c.DebugWriter != nil && sseEvents <= 10 {
			fmt.Fprintf(c.DebugWriter, "<<< SSE event #%d: %s\n", sseEvents, httpx.Truncate(string(event.Data), 500))
		}

		var chunk chatStreamChunk
		if err := json.Unmarshal(event.Data, &chunk); err != nil {
			// Skip unparseable events (some servers send keepalive comments).
			continue
		}

		// Extract content delta for TTFT.
		content := ""
		reasoning := ""
		if len(chunk.Choices) > 0 {
			content = chunk.Choices[0].Delta.Content
			reasoning = chunk.Choices[0].Delta.ReasoningContent
		}
		// Fallback: some compatible servers put content in a different location.
		// Try extracting via raw map if the structured parse found nothing.
		if content == "" && reasoning == "" {
			content = extractChatContent(event.Data)
		}

		// Track reasoning tokens for TTFT — reasoning tokens arrive before
		// content tokens for thinking models, so TTFT should be measured from
		// the first token of any kind (reasoning or content).
		if reasoning != "" {
			if !hasTTFT {
				ttft = time.Since(requestStart)
				hasTTFT = true
			}
			chunkCount++
		}

		if content != "" {
			if !hasTTFT {
				ttft = time.Since(requestStart)
				hasTTFT = true
			}
			contentBuf.WriteString(content)
			chunkCount++
		}

		if len(chunk.Choices) > 0 {
			// Extract tool call deltas.
			for _, tc := range chunk.Choices[0].Delta.ToolCalls {
				idx := tc.Index
				for len(toolCalls) <= idx {
					toolCalls = append(toolCalls, ToolCall{
						ID:       tc.ID,
						Type:     tc.Type,
						Function: ToolFunction{Name: tc.Function.Name},
					})
				}
				if tc.ID != "" {
					toolCalls[idx].ID = tc.ID
				}
				if tc.Type != "" {
					toolCalls[idx].Type = tc.Type
				}
				if tc.Function.Name != "" {
					toolCalls[idx].Function.Name = tc.Function.Name
				}
				toolCalls[idx].Function.Arguments += tc.Function.Arguments
			}
		}

		// Extract usage if present (usually in the last chunk when
		// stream_options.include_usage=true is set).
		if chunk.Usage.CompletionTokens > 0 || chunk.Usage.PromptTokens > 0 {
			usage = chunk.Usage
		}
	}

	totalTime := time.Since(requestStart)

	// Fallback: if SSE parsing found zero events, the server likely
	// returned a non-streaming JSON body (ignoring stream=true).
	// Parse it as a regular Chat Completions response.
	if chunkCount == 0 {
		var resp Response
		if json.Unmarshal(bodyBytes, &resp) == nil {
			if len(resp.Choices) > 0 {
				text := resp.Choices[0].Message.Content
				if text != "" {
					if !hasTTFT {
						// For a non-streaming response, TTFT = totalTime.
						ttft = totalTime
						hasTTFT = true
					}
					contentBuf.WriteString(text)
					chunkCount = 1
					finalRaw = bodyBytes
				}
			}
			// Extract usage from non-streaming response.
			if resp.Usage != nil {
				var u chatUsage
				if json.Unmarshal(resp.Usage, &u) == nil {
					usage = u
				}
			}
		}
	}

	// Token counting priority:
	// 1. usage.CompletionTokens (most accurate, requires stream_options)
	// 2. content chunk count (each SSE content delta ≈ 1 token)
	// 3. character-based heuristic (len/4)
	tokens := usage.CompletionTokens
	if tokens <= 0 {
		tokens = chunkCount
	}
	if tokens <= 0 {
		tokens = countApproxTokens(contentBuf.String())
	}

	if !hasTTFT {
		// If we never got a content delta, TTFT equals total time
		// (the model produced no content, only the final event).
		ttft = totalTime
	}

	metrics := runner.StreamMetrics{
		TTFB:             ttfb,
		TTFT:             ttft,
		TotalTime:        totalTime,
		CompletionTokens: tokens,
		PromptTokens:     usage.PromptTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		ContentLen:       contentBuf.Len(),
		ChunkCount:       chunkCount,
	}

	if c.DebugWriter != nil {
		httpx.DumpResponse(c.DebugWriter, resp, finalRaw)
	}

	return &StreamResult{
		Metrics:    metrics,
		Content:    contentBuf.String(),
		ToolCalls:  toolCalls,
		Raw:        finalRaw,
		ChunkCount: chunkCount,
	}, nil
}

// chatStreamChunk is a minimal model of a streaming Chat Completions chunk.
type chatStreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Choices []streamChoice  `json:"choices"`
	Usage   chatUsage       `json:"usage,omitempty"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type streamDelta struct {
	Role             string           `json:"role,omitempty"`
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []streamToolCall `json:"tool_calls,omitempty"`
}

type streamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens"`
}

// countApproxTokens provides a rough token estimate when usage is unavailable.
// Uses the heuristic that 1 token ≈ 4 characters for English text.
func countApproxTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return len(s) / 4
}

// extractChatContent is a fallback that tries to extract content text from a
// Chat Completions SSE event when the structured parse finds nothing. It
// handles servers that use non-standard field names or nesting.
func extractChatContent(data json.RawMessage) string {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return ""
	}

	// Try choices[0].delta.content via raw map (handles minor schema differences).
	if choicesRaw, ok := raw["choices"]; ok {
		var choices []map[string]json.RawMessage
		if json.Unmarshal(choicesRaw, &choices) == nil && len(choices) > 0 {
			if deltaRaw, ok := choices[0]["delta"]; ok {
				var delta map[string]json.RawMessage
				if json.Unmarshal(deltaRaw, &delta) == nil {
					// Try "content" field.
					if cRaw, ok := delta["content"]; ok {
						var s string
						if json.Unmarshal(cRaw, &s) == nil && s != "" {
							return s
						}
					}
					// Try "text" field (some servers use this instead of "content").
					if tRaw, ok := delta["text"]; ok {
						var s string
						if json.Unmarshal(tRaw, &s) == nil && s != "" {
							return s
						}
					}
				}
			}
		}
	}

	return ""
}
