// Package mcp implements the slice of the Model Context Protocol depdog needs
// to let an MCP-capable agent (Claude, Cursor, …) consult the architecture in
// the loop: a hand-rolled JSON-RPC 2.0 codec over the MCP base protocol
// (Content-Length framing, mirroring internal/lsp) and a stdio server around
// an injected read-only handler.
//
// The package depends on the standard library and internal/core only — the
// same constraint core itself lives under, machine-enforced by depdog.yaml.
// The CLI (internal/cli/mcp.go) injects closures that do config discovery,
// adapter selection, graph loading, evaluation, and JSON rendering, so this
// package never imports config/lang/report. docs/mcp.md records why no
// third-party MCP library is used.
package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// JSON-RPC 2.0 error codes the server emits.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// errBadJSON marks a frame whose header was fine but whose payload was not
// valid JSON. The framing is intact, so the caller can answer with
// codeParseError and keep reading; every other read error is unrecoverable.
var errBadJSON = errors.New("frame payload is not valid JSON")

// message is a JSON-RPC 2.0 message. One struct covers all three shapes:
// a request has ID and Method, a notification only Method, a response ID and
// exactly one of Result or Error. ID stays raw because clients may use
// numbers or strings and responses must echo it verbatim.
type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *responseError   `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// readMessage reads one base-protocol frame: header lines terminated by \r\n,
// a blank line, then exactly Content-Length bytes of JSON. Header names are
// case-insensitive; unknown headers (Content-Type in particular) are ignored.
// A frame without a valid Content-Length is unrecoverable — the stream has no
// other sync point — so it returns a hard error.
func readMessage(r *bufio.Reader) (*message, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF && line == "" && length == -1 {
				return nil, io.EOF // clean end of stream between frames
			}
			return nil, fmt.Errorf("reading frame header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("malformed header line %q: want \"Name: value\"", line)
		}
		if !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid Content-Length %q: want a non-negative integer", strings.TrimSpace(value))
		}
		length = n
	}
	if length == -1 {
		return nil, errors.New("frame has no Content-Length header")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("reading %d-byte frame body: %w", length, err)
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", errBadJSON, err)
	}
	return &m, nil
}

// writeMessage frames m as Content-Length: N\r\n\r\n<json>.
func writeMessage(w io.Writer, m *message) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// response builds a success response echoing id; result is marshaled.
func response(id *json.RawMessage, result any) (*message, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &message{JSONRPC: "2.0", ID: id, Result: raw}, nil
}

// errorResponse builds an error response; a nil id becomes JSON null, per the
// JSON-RPC spec for errors detected before the id is known.
func errorResponse(id *json.RawMessage, code int, msg string) *message {
	if id == nil {
		null := json.RawMessage("null")
		id = &null
	}
	return &message{JSONRPC: "2.0", ID: id, Error: &responseError{Code: code, Message: msg}}
}
