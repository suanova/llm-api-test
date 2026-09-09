package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/httpx"
	"llm-api-test/internal/registry"
	"llm-api-test/internal/sse"
)

// SoakCase runs one short streamed messages request per soak turn and
// classifies every failure mode (design.md "soak"). Turns carry no history:
// each is an independent request that must stream to its completion marker.
type SoakCase struct {
	client *Client
}

func (c *SoakCase) ID() string   { return "messages:soak" }
func (c *SoakCase) Desc() string { return "POST /v1/messages stream stability over a long run" }

// RunTurn runs one streamed request and returns its classification. The
// stream must end with the message_stop event: EOF before it is a mid-stream
// cut. When no stream event arrives for stall, the request is torn down and
// the turn is classified as stalled.
func (c *SoakCase) RunTurn(ctx context.Context, model string, long bool, stall time.Duration) *registry.SoakTurn {
	tr := &registry.SoakTurn{Long: long}
	maxTokens := 64
	prompt := cases.SoakShortPrompt
	if long {
		maxTokens = 512
		prompt = cases.SoakLongPrompt
	}
	body, err := json.Marshal(&Request{
		Model:     model,
		MaxTokens: maxTokens,
		Stream:    true,
		Messages:  []Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		tr.Class = registry.SoakOther
		tr.Err = err
		return tr
	}
	start := time.Now()
	reqCtx, cancelReq := context.WithCancel(ctx)
	defer cancelReq()
	resp, err := c.client.do(reqCtx, body)
	if err != nil {
		tr.Class = cases.SoakNetClass(err)
		tr.Err = err
		return tr
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		tr.Class = cases.SoakHTTPClass(resp.StatusCode)
		tr.Err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, httpx.Truncate(string(data), 500))
		tr.Total = time.Since(start)
		return tr
	}

	// Read the stream to the message_stop event under a no-data watchdog.
	var stalled atomic.Bool
	reset, stop := cases.StreamStallGuard(stall, func() {
		stalled.Store(true)
		cancelReq() // interrupt the blocked read
	})
	defer stop()

	complete := false
	parser := sse.NewParser(resp.Body)
	for {
		ev, err := parser.Next()
		if err != nil {
			if err == io.EOF || complete {
				break // clean end, or teardown noise after the completion marker
			}
			if stalled.Load() {
				tr.Class = registry.SoakStall
				tr.Err = fmt.Errorf("no data for %s", stall)
			} else if ctx.Err() != nil {
				tr.Class = registry.SoakTimeout
				tr.Err = ctx.Err()
			} else {
				// The response started but the wire broke before the
				// completion marker: a mid-stream cut, not a connect failure.
				tr.Class = registry.SoakDropped
				tr.Err = fmt.Errorf("stream cut before message_stop: %w", err)
			}
			tr.Total = time.Since(start)
			return tr
		}
		reset()
		var e struct {
			Type  string `json:"type"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(ev.Data, &e); err != nil {
			continue // non-JSON events (e.g. ping) are activity, not signals
		}
		if e.Type == "error" {
			msg := "stream error event"
			if e.Error != nil && e.Error.Message != "" {
				msg = e.Error.Message
			}
			tr.Class = registry.SoakOther
			tr.Err = fmt.Errorf("%s", msg)
			tr.Total = time.Since(start)
			return tr
		}
		if e.Type == "message_stop" {
			complete = true
		}
	}
	if !complete {
		tr.Class = registry.SoakDropped
		tr.Err = fmt.Errorf("stream ended without message_stop")
	} else {
		tr.Class = registry.SoakOK
	}
	tr.Total = time.Since(start)
	return tr
}

// soakHTTPClient returns an HTTP client whose transport holds idle
// connections open for hours, so a proxy-side idle timeout surfaces on reuse
// instead of being masked by the default 90s client-side reap.
func soakHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.IdleConnTimeout = 2 * time.Hour
	return &http.Client{Transport: tr}
}
