package anthropic

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
// Anthropic Messages API request.
type StreamResult struct {
	Metrics    runner.StreamMetrics
	Content    string // accumulated content from text deltas
	Raw        []byte // final raw event (for debugging)
	ChunkCount int    // number of SSE content delta chunks received
}

// StreamMessage sends a streaming Anthropic Messages request and returns
// timing metrics parsed from the SSE stream. The request has stream=true
// added automatically.
func (c *Client) StreamMessage(ctx context.Context, req *Request) (*StreamResult, error) {
	// Force stream=true.
	req.SetExtra("stream", true)

	body, err := encodeRequest(req)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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

	parser := sse.NewParser(bytes.NewReader(bodyBytes))
	var (
		contentBuf strings.Builder
		ttft       time.Duration
		hasTTFT    bool
		usage      anthropicStreamUsage
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

		var evt anthropicStreamEvent
		if err := json.Unmarshal(event.Data, &evt); err != nil {
			continue
		}

		// Content start event marks the first content token.
		if evt.Type == "content_block_delta" && evt.Delta != nil && evt.Delta.Text != "" {
			if !hasTTFT {
				ttft = time.Since(requestStart)
				hasTTFT = true
			}
			contentBuf.WriteString(evt.Delta.Text)
			chunkCount++
		}

		// Extract usage from message events.
		if evt.Type == "message_delta" && evt.Usage != nil {
			usage.OutputTokens += evt.Usage.OutputTokens
		}
		if evt.Type == "message_start" && evt.Message != nil && evt.Message.Usage != nil {
			usage.InputTokens = evt.Message.Usage.InputTokens
		}
	}

	totalTime := time.Since(requestStart)

	// Fallback: if SSE parsing found zero events, the server likely
	// returned a non-streaming JSON body (ignoring stream=true).
	// Parse it as a regular Anthropic Messages response.
	if chunkCount == 0 {
		var resp Response
		if json.Unmarshal(bodyBytes, &resp) == nil {
			for _, block := range resp.Content {
				if block.Type == "text" && block.Text != "" {
					if !hasTTFT {
						ttft = totalTime
						hasTTFT = true
					}
					contentBuf.WriteString(block.Text)
					chunkCount = 1
					finalRaw = bodyBytes
				}
			}
			// Extract usage from non-streaming response.
			if resp.Usage != nil {
				var u struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				}
				if json.Unmarshal(resp.Usage, &u) == nil {
					usage.InputTokens = u.InputTokens
					usage.OutputTokens = u.OutputTokens
				}
			}
		}
	}

	// Token counting priority:
	// 1. usage.OutputTokens (from message_delta event)
	// 2. content chunk count (each SSE content delta ≈ 1 token)
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
		Content:    contentBuf.String(),
		Raw:        finalRaw,
		ChunkCount: chunkCount,
	}, nil
}

type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Delta   *struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"delta,omitempty"`
	Message *struct {
		Usage *struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage,omitempty"`
	} `json:"message,omitempty"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
}

type anthropicStreamUsage struct {
	InputTokens  int
	OutputTokens int
}

func countApproxTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return len(s) / 4
}
