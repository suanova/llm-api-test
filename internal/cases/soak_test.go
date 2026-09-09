package cases

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"llm-api-test/internal/registry"
)

func TestSoakHTTPClass(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   registry.SoakClass
	}{
		{429, registry.SoakHTTP429},
		{500, registry.SoakHTTP5xx},
		{503, registry.SoakHTTP5xx},
		{400, registry.SoakHTTP4xx},
		{403, registry.SoakHTTP4xx},
		{302, registry.SoakHTTP4xx},
	} {
		if got := SoakHTTPClass(tc.status); got != tc.want {
			t.Errorf("SoakHTTPClass(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestSoakNetClass(t *testing.T) {
	connErr := &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}
	for _, tc := range []struct {
		name string
		err  error
		want registry.SoakClass
	}{
		{"deadline", context.DeadlineExceeded, registry.SoakTimeout},
		{"wrapped deadline", &url.Error{Op: "Post", URL: "http://x", Err: context.DeadlineExceeded}, registry.SoakTimeout},
		{"i/o timeout", &net.OpError{Op: "dial", Err: &timeoutError{}}, registry.SoakTimeout},
		{"conn reset", &url.Error{Op: "Post", URL: "http://x", Err: connErr}, registry.SoakConn},
		{"unexpected eof", io.ErrUnexpectedEOF, registry.SoakConn},
		{"eof", io.EOF, registry.SoakConn},
		{"other", errors.New("boom"), registry.SoakOther},
	} {
		if got := SoakNetClass(tc.err); got != tc.want {
			t.Errorf("%s: SoakNetClass = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// timeoutError implements net.Error with Timeout() true.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestStreamStallGuardFires(t *testing.T) {
	var fired atomic.Bool
	_, stop := StreamStallGuard(30*time.Millisecond, func() { fired.Store(true) })
	defer stop()
	time.Sleep(80 * time.Millisecond)
	if !fired.Load() {
		t.Error("guard did not fire after the stall window")
	}
}

func TestStreamStallGuardResetKeepsAlive(t *testing.T) {
	var fired atomic.Bool
	reset, stop := StreamStallGuard(40*time.Millisecond, func() { fired.Store(true) })
	defer stop()
	for i := 0; i < 5; i++ {
		time.Sleep(20 * time.Millisecond)
		reset() // activity before the window expires
	}
	if fired.Load() {
		t.Error("guard fired despite regular resets")
	}
}

func TestStreamStallGuardStop(t *testing.T) {
	var fired atomic.Bool
	reset, stop := StreamStallGuard(20*time.Millisecond, func() { fired.Store(true) })
	stop()
	time.Sleep(60 * time.Millisecond)
	if fired.Load() {
		t.Error("guard fired after stop")
	}
	_ = reset // no-op after stop; must not panic
}
