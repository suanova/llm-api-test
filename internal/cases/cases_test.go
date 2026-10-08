package cases

import (
	"strings"
	"testing"
)

// FillerPrompt backs --input-tokens: the same size must always produce the
// same prompt text so repeated runs are comparable.
func TestFillerPromptDeterministic(t *testing.T) {
	if a, b := FillerPrompt(800), FillerPrompt(800); a != b {
		t.Error("FillerPrompt(800) returned different text across calls")
	}
}

// FillerPrompt targets roughly 0.75 words per token (English prose tokenizes
// at about 1.33 tokens per word), so an 800-token request carries ~600 filler
// words plus the trailing instruction.
func TestFillerPromptSize(t *testing.T) {
	words := len(strings.Fields(FillerPrompt(800)))
	if words < 600 || words > 660 {
		t.Errorf("FillerPrompt(800) = %d words, want 600..660", words)
	}
}

func TestFillerPromptNonPositive(t *testing.T) {
	if got := FillerPrompt(0); got != "" {
		t.Errorf("FillerPrompt(0) = %q, want empty", got)
	}
}
