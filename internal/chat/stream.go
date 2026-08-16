package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"llm-api-test/internal/httpx"
	"llm-api-test/internal/sse"
)

// streamChunk is one SSE data payload from a streamed chat completion.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

// timedReader records the elapsed time of the first read, i.e. the time to
// the first byte of the HTTP response body.
type timedReader struct {
	r     io.Reader
	start time.Time
	once  sync.Once
	first time.Duration
}

func (t *timedReader) Read(p []byte) (int, error) {
	t.once.Do(func() { t.first = time.Since(t.start) })
	return t.r.Read(p)
}

// sendStream sends a streaming request, accumulates content and tool calls,
// and records timing metrics (TTFB, TTFT, per-chunk TPOT).
func (c *Client) sendStream(ctx context.Context, req *Request) (*Result, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	start := time.Now()
	resp, err := c.oc.Do(ctx, "/chat/completions", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, httpx.Truncate(string(data), 500))
	}

	res := &Result{}
	tr := &timedReader{r: resp.Body, start: start}
	parser := sse.NewParser(tr)
	var content, raw strings.Builder
	var prevChunk time.Time
	for {
		ev, err := parser.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse stream: %w", err)
		}
		if bytes.Equal(ev.Data, []byte("[DONE]")) {
			break
		}
		raw.Write(ev.Data)
		raw.WriteByte('\n')

		var ch streamChunk
		if err := json.Unmarshal(ev.Data, &ch); err != nil {
			return nil, fmt.Errorf("decode chunk: %w", err)
		}
		if ch.Usage != nil {
			res.Usage = ch.Usage
			res.Metrics.PromptTokens = ch.Usage.PromptTokens
			res.Metrics.CompletionTokens = ch.Usage.CompletionTokens
		}
		if len(ch.Choices) == 0 {
			continue
		}
		delta := ch.Choices[0].Delta
		if delta.Content != "" {
			now := time.Now()
			if res.Metrics.TTFT == 0 {
				res.Metrics.TTFT = now.Sub(start)
			}
			if !prevChunk.IsZero() {
				res.Metrics.TPOTs = append(res.Metrics.TPOTs, now.Sub(prevChunk))
			}
			prevChunk = now
			content.WriteString(delta.Content)
			res.Metrics.Chunks++
			res.Metrics.ContentBytes += len(delta.Content)
		}
		res.ToolCalls = append(res.ToolCalls, delta.ToolCalls...)
	}

	m := &res.Metrics
	m.Total = time.Since(start)
	m.TTFB = tr.first
	res.Content = content.String()
	res.Raw = raw.String()
	if c.oc.Debug != nil {
		httpx.DumpResponse(c.oc.Debug, resp, []byte(res.Raw))
	}
	return res, nil
}
