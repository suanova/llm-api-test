// Package httpx holds small HTTP helpers shared by the API-specific clients
// (openai, chat): a redacted request/response dumper, pretty-printing, and the
// non-2xx APIError type.
package httpx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// APIError is returned for non-2xx responses.
type APIError struct {
	Status int
	Body   []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error: status %d: %s", e.Status, Truncate(string(e.Body), 500))
}

// Truncate clips s to n bytes, appending a marker if it was shortened.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}

// PrettyJSON returns indented JSON, or the raw string if it isn't valid JSON.
func PrettyJSON(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(out)
}

// redactHeaders are header values never dumped in the clear.
var redactHeaders = map[string]bool{
	"authorization": true,
	"x-api-key":     true,
}

// DumpRequest writes a human-readable copy of the outgoing request, redacting
// sensitive header values.
func DumpRequest(w io.Writer, req *http.Request, body []byte) {
	fmt.Fprintf(w, ">>> REQUEST %s %s\n", req.Method, req.URL.String())
	fmt.Fprintln(w, ">>> Headers:")
	for k, vs := range req.Header {
		for _, v := range vs {
			if redactHeaders[strings.ToLower(k)] {
				v = strings.Repeat("*", 8) + " (redacted)"
			}
			fmt.Fprintf(w, ">>>   %s: %s\n", k, v)
		}
	}
	fmt.Fprintf(w, ">>> Body: %s\n", PrettyJSON(body))
}

// DumpResponse writes a human-readable copy of the response.
func DumpResponse(w io.Writer, resp *http.Response, body []byte) {
	fmt.Fprintf(w, "<<< RESPONSE %s\n", resp.Status)
	fmt.Fprintln(w, "<<< Headers:")
	for k, vs := range resp.Header {
		for _, v := range vs {
			fmt.Fprintf(w, "<<<   %s: %s\n", k, v)
		}
	}
	fmt.Fprintf(w, "<<< Body: %s\n", PrettyJSON(body))
}
