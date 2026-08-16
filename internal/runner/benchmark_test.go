package runner

import (
	"testing"
	"time"
)

func TestProgressLine(t *testing.T) {
	got := progressLine(3*time.Second, 1, 4)
	want := "[benchmark] elapsed 3s, 1/4 requests completed"
	if got != want {
		t.Errorf("progressLine = %q, want %q", got, want)
	}
}
