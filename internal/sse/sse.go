// Package sse provides a minimal Server-Sent Events parser for streaming LLM
// API responses. It reads from an io.Reader and emits parsed events.
package sse

import (
	"bufio"
	"bytes"
	"io"
)

// Event is a single SSE event: one or more "data:" lines, optionally followed
// by a blank line.
type Event struct {
	// Data is the concatenation of all "data:" fields (joined by newlines if
	// multiple data lines appear).
	Data []byte
}

// Parser reads SSE events from a stream.
type Parser struct {
	scanner *bufio.Scanner
}

// NewParser creates an SSE parser reading from r.
func NewParser(r io.Reader) *Parser {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line
	return &Parser{scanner: s}
}

// Next returns the next event. It returns io.EOF when the stream ends.
// Comments (lines starting with ":") and empty lines are skipped.
// The "[DONE]" sentinel is returned as an event with Data = []byte("[DONE]").
func (p *Parser) Next() (Event, error) {
	var data [][]byte
	for p.scanner.Scan() {
		line := p.scanner.Bytes()
		line = bytes.TrimSpace(line)

		// Empty line: end of current event (if any data collected).
		if len(line) == 0 {
			if len(data) > 0 {
				break
			}
			continue
		}

		// Comment line: skip.
		if line[0] == ':' {
			continue
		}

		// "data:" field.
		if bytes.HasPrefix(line, []byte("data:")) {
			// Trim "data:" prefix and optional single space.
			payload := bytes.TrimPrefix(line, []byte("data:"))
			payload = bytes.TrimPrefix(payload, []byte(" "))
			data = append(data, payload)
		}
		// Other SSE fields (event:, id:, retry:) are ignored for now.
	}

	if len(data) == 0 {
		if err := p.scanner.Err(); err != nil {
			return Event{}, err
		}
		return Event{}, io.EOF
	}

	return Event{Data: bytes.Join(data, []byte("\n"))}, nil
}
