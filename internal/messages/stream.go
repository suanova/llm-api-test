package messages

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

// streamEvent is one SSE data payload from a streamed messages request.
type streamEvent struct {
	Type         string `json:"type"`
	ContentBlock *struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
	Message *struct {
		Usage *Usage `json:"usage"`
	} `json:"message"`
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
	resp, err := c.do(ctx, body)
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
	var pending *ToolCall
	var pendingInput strings.Builder
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

		var e streamEvent
		if err := json.Unmarshal(ev.Data, &e); err != nil {
			return nil, fmt.Errorf("decode chunk: %w", err)
		}
		switch e.Type {
		case "content_block_start":
			if e.ContentBlock != nil && e.ContentBlock.Type == "tool_use" {
				pending = &ToolCall{Name: e.ContentBlock.Name}
				if len(e.ContentBlock.Input) > 0 {
					pending.Input = e.ContentBlock.Input
				}
			}
		case "content_block_delta":
			if e.Delta == nil {
				continue
			}
			switch e.Delta.Type {
			case "text_delta":
				if e.Delta.Text == "" {
					continue
				}
				now := time.Now()
				if res.Metrics.TTFT == 0 {
					res.Metrics.TTFT = now.Sub(start)
				}
				if !prevChunk.IsZero() {
					res.Metrics.TPOTs = append(res.Metrics.TPOTs, now.Sub(prevChunk))
				}
				prevChunk = now
				content.WriteString(e.Delta.Text)
				res.Metrics.Chunks++
				res.Metrics.ContentBytes += len(e.Delta.Text)
			case "input_json_delta":
				if pending != nil {
					pendingInput.WriteString(e.Delta.PartialJSON)
				}
			}
		case "content_block_stop":
			if pending != nil {
				if pending.Input == nil && pendingInput.Len() > 0 {
					pending.Input = json.RawMessage(pendingInput.String())
				}
				res.ToolCalls = append(res.ToolCalls, *pending)
				pending = nil
				pendingInput.Reset()
			}
		case "message_start":
			if e.Message != nil && e.Message.Usage != nil {
				res.Usage = e.Message.Usage
				res.Metrics.PromptTokens = e.Message.Usage.InputTokens
				res.Metrics.CompletionTokens = e.Message.Usage.OutputTokens
			}
		case "message_delta":
			if e.Usage != nil {
				res.Usage = e.Usage
				res.Metrics.PromptTokens = e.Usage.InputTokens
				res.Metrics.CompletionTokens = e.Usage.OutputTokens
			}
		}
	}

	m := &res.Metrics
	m.Total = time.Since(start)
	m.TTFB = tr.first
	res.Content = content.String()
	res.Raw = raw.String()
	if c.debug != nil {
		httpx.DumpResponse(c.debug, resp, []byte(res.Raw))
	}
	return res, nil
}
