// Package openai is a thin raw HTTP client for the Responses API.
// It intentionally avoids the official SDK so we exercise the real wire format.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"llm-api-test/internal/httpx"
)

// Client posts to the /responses endpoint of a Responses-API-compatible server.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	// DebugWriter, if non-nil, receives a human-readable dump of each request
	// (URL + headers + body) and response (status + headers + body). Sensitive
	// headers are redacted.
	DebugWriter io.Writer
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		// No Timeout on the client — streaming responses can take a long time.
		// Per-request timeouts are set via context in each method call.
		HTTP: &http.Client{},
	}
}

// Request is a minimal subset of the Responses API request body. We keep it
// loose (map for anything we don't model explicitly) so cases can pass through
// experimental params without a struct change.
type Request struct {
	Model        string          `json:"model"`
	Input        json.RawMessage `json:"input"` // string or array
	Instructions *string         `json:"instructions,omitempty"`
	Tools        []Tool          `json:"tools,omitempty"`
	ToolChoice   json.RawMessage `json:"tool_choice,omitempty"` // string or object
	Reasoning    *Reasoning      `json:"reasoning,omitempty"`
	Text         *Text           `json:"text,omitempty"`
	// Extra passthrough params (e.g. prompt_cache_key, temperature, ...).
	Extra map[string]json.RawMessage `json:"-"`
}

// Tool is a single tool definition.
type Tool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Reasoning configures reasoning behavior.
type Reasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// Text configures the text response format.
type Text struct {
	Verbosity string  `json:"verbosity,omitempty"`
	Format    *Format `json:"format,omitempty"`
}

// Format configures structured output. Type is "json_schema" or "json_object".
// For json_schema, Name+Schema+Strict are set.
type Format struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
	Description string          `json:"description,omitempty"`
}

// Response is a minimal model of the Responses API response body. We keep the
// raw bytes so cases can inspect anything not modeled here.
type Response struct {
	ID     string          `json:"id"`
	Object string          `json:"object"`
	Model  string          `json:"model"`
	Output []OutputItem    `json:"output"`
	Usage  json.RawMessage `json:"usage"`
	Raw    json.RawMessage `json:"-"`
}

// OutputItem is a single entry in the response output array. Arguments/Content
// are kept raw so callers can decode per item type.
type OutputItem struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Role      string          `json:"role"`
	Status    string          `json:"status"`
	Name      string          `json:"name"`
	CallID    string          `json:"call_id"`
	Arguments string          `json:"arguments"`
	Content   json.RawMessage `json:"content"`
}

// CreateResponse posts the request to /responses and returns the parsed body.
// On non-2xx it returns a rich error including the raw response body.
func (c *Client) CreateResponse(ctx context.Context, req *Request) (*Response, error) {
	body, err := encodeRequest(req)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	if c.DebugWriter != nil {
		httpx.DumpRequest(c.DebugWriter, httpReq, body)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if c.DebugWriter != nil {
		httpx.DumpResponse(c.DebugWriter, resp, raw)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &httpx.APIError{Status: resp.StatusCode, Body: raw}
	}

	// Some servers return 200 OK with an error body even for non-streaming
	// requests (e.g. unsupported model). The error may be plain JSON or
	// SSE-formatted (data: {...}). Detect before trying to parse.
	if isErrorBodyOrSSEError(raw) {
		return nil, &httpx.APIError{Status: resp.StatusCode, Body: raw}
	}

	var out Response
	out.Raw = raw
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, httpx.Truncate(string(raw), 500))
	}
	return &out, nil
}

// encodeRequest marshals the request, merging Extra fields into the top-level
// JSON object so callers can add arbitrary params.
func encodeRequest(req *Request) ([]byte, error) {
	type alias Request
	b, err := json.Marshal((*alias)(req))
	if err != nil {
		return nil, err
	}
	if len(req.Extra) == 0 {
		return b, nil
	}
	merged := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &merged); err != nil {
		return nil, err
	}
	for k, v := range req.Extra {
		merged[k] = v
	}
	return json.Marshal(merged)
}

// SetExtra is a small helper to attach a raw-JSON extra param.
func (r *Request) SetExtra(key string, val any) {
	if r.Extra == nil {
		r.Extra = map[string]json.RawMessage{}
	}
	b, err := json.Marshal(val)
	if err != nil {
		return
	}
	r.Extra[key] = b
}
