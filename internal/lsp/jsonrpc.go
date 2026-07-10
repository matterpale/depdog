// Package lsp implements the slice of the Language Server Protocol depdog
// needs to surface architecture violations as in-editor diagnostics: a
// hand-rolled JSON-RPC 2.0 codec over the LSP base protocol (Content-Length
// framing) and a stdio server around an injected check function.
//
// The package depends on the standard library and internal/core only — the
// same constraint core itself lives under, machine-enforced by depdog.yaml.
// docs/lsp.md records why no third-party LSP library is used.
package lsp

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

// readMessage reads one LSP base-protocol frame: header lines terminated by
// \r\n, a blank line, then exactly Content-Length bytes of JSON. Header names
// are case-insensitive; unknown headers (Content-Type in particular) are
// ignored. A frame without a valid Content-Length is unrecoverable — the
// stream has no other sync point — so it returns a hard error.
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

// request builds a server-to-client request with a server-issued string id.
// The client answers with a response echoing that id and carrying no method —
// the Serve loop routes on exactly that shape.
func request(id, method string, params any) (*message, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	rawID := json.RawMessage(strconv.Quote(id))
	return &message{JSONRPC: "2.0", ID: &rawID, Method: method, Params: raw}, nil
}

// notification builds a server-to-client notification.
func notification(method string, params any) (*message, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return &message{JSONRPC: "2.0", Method: method, Params: raw}, nil
}
