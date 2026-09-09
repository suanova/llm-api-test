package cases

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"llm-api-test/internal/registry"
)

// SoakShortPrompt is the fixed prompt of a short soak turn: a small reply so
// the turn exercises the connection without dominating the wall clock.
const SoakShortPrompt = "Reply with exactly the word: ok"

// SoakLongPrompt is the prompt of a long soak turn. It asks for a
// multi-paragraph answer so the stream stays open long enough to expose
// mid-stream cuts or gateway timeouts on longer generations. ~500 tokens ≈ a
// few paragraphs at typical speeds.
const SoakLongPrompt = "Write a few detailed paragraphs (about 400-500 words) " +
	"explaining how HTTP keep-alive connections work, with concrete examples."

// SoakHTTPClass maps a response status to its soak failure class.
func SoakHTTPClass(status int) registry.SoakClass {
	switch {
	case status == 429:
		return registry.SoakHTTP429
	case status >= 500:
		return registry.SoakHTTP5xx
	default:
		return registry.SoakHTTP4xx // includes other non-2xx statuses
	}
}

// SoakNetClass classifies a transport-level request error (everything below
// the HTTP status layer): connection failures vs budget expiry vs other.
func SoakNetClass(err error) registry.SoakClass {
	if errors.Is(err, context.DeadlineExceeded) {
		return registry.SoakTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return registry.SoakTimeout
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return registry.SoakConn // connection closed under the request
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return registry.SoakConn // reset, refused, unreachable, ...
	}
	return registry.SoakOther
}

// StreamStallGuard watches a streamed response and interrupts it when no
// event arrives for the stall window: reset must be called after every
// received event, and stop when the stream read finishes. When the guard
// fires it calls onStall, which should cancel the request context to unblock
// the pending read (and set the caller's stall flag).
func StreamStallGuard(stall time.Duration, onStall func()) (reset, stop func()) {
	resetCh := make(chan struct{}, 1)
	doneCh := make(chan struct{})
	var once sync.Once
	stop = func() { once.Do(func() { close(doneCh) }) }
	go func() {
		for {
			t := time.NewTimer(stall)
			select {
			case <-doneCh:
				t.Stop()
				return
			case <-resetCh:
				t.Stop() // activity: restart the window
			case <-t.C:
				t.Stop()
				onStall()
				return
			}
		}
	}()
	reset = func() {
		select {
		case resetCh <- struct{}{}:
		default: // an earlier reset is already queued
		}
	}
	return reset, stop
}
