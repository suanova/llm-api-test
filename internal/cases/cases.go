// Package cases provides shared helpers used by API-specific case packages
// (internal/responses, internal/chat). Each case package defines its own All()
// that returns cases wired to a client for that API surface.
package cases

import (
	"encoding/json"
	"fmt"
	"strings"

	"llm-api-test/internal/runner"
)

// Fail returns a failing Result with a formatted detail message and the raw
// response body (for debugging / --verbose).
func Fail(raw json.RawMessage, format string, args ...any) *runner.Result {
	return &runner.Result{
		Pass:   false,
		Detail: fmt.Sprintf(format, args...),
		Raw:    string(raw),
	}
}

// Pass returns a passing Result with a detail message and the raw body.
func Pass(detail string, raw json.RawMessage) *runner.Result {
	return &runner.Result{Pass: true, Detail: detail, Raw: string(raw)}
}

// MustJSON marshals v to JSON, panicking only on impossible inputs. Handy for
// building request bodies from literals.
func MustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ContainsFold reports whether s contains sub, ignoring ASCII case.
func ContainsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
