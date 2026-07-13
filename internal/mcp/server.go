package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
)

// Server is a stdio MCP server that answers a fixed, read-only method set —
// initialize / notifications/initialized / tools/list / tools/call /
// resources/list / resources/read / ping — over JSON-RPC 2.0 with
// Content-Length framing. It dispatches the actual work (check, explain,
// can_import, config, components) to an injected Handler; the Server itself
// only speaks protocol. A nil Handler is valid: tools/call and resources/read
// then answer "not wired yet" (the M1 stub state), everything else still
// works, so the handshake and catalogue can be exercised before the work is
// wired.
type Server struct {
	handler Handler
	version string // stamped into serverInfo.version (cli.Version)
}

// NewServer builds a Server around the injected handler. version is stamped
// into the initialize serverInfo. A nil handler makes tools/call and
// resources/read return a clear "not wired yet" error while the rest of the
// protocol keeps working.
func NewServer(handler Handler, version string) *Server {
	return &Server{handler: handler, version: version}
}

// Serve runs the JSON-RPC loop on in/out until the client closes stdin. It is
// deliberately single-goroutine: messages are handled in arrival order, so a
// session transcript is deterministic. logw (stderr in production) receives
// every operational message — out carries protocol frames only.
//
// Return value: nil on stdin EOF (MCP has no shutdown handshake — the client
// closing the pipe is the orderly end of a session); an error only when the
// message stream itself becomes unrecoverable or a write fails.
func (s *Server) Serve(ctx context.Context, in io.Reader, out, logw io.Writer) error {
	logger := log.New(logw, "depdog mcp: ", 0)
	r := bufio.NewReader(in)
	for {
		if err := ctx.Err(); err != nil {
			return nil // cancelled: end the session cleanly
		}
		msg, err := readMessage(r)
		switch {
		case errors.Is(err, io.EOF):
			return nil // client closed the pipe: orderly end of session
		case errors.Is(err, errBadJSON):
			// Framing was intact, so the loop can keep reading.
			logger.Printf("dropping unparseable frame: %v", err)
			if werr := writeMessage(out, errorResponse(nil, codeParseError, err.Error())); werr != nil {
				return werr
			}
			continue
		case err != nil:
			return fmt.Errorf("cannot recover the message stream: %w — restart the server", err)
		}

		if msg.ID == nil {
			// Notification: never answered. notifications/initialized is the
			// only one MCP defines for this server; anything else is ignored.
			if msg.Method != "notifications/initialized" {
				logger.Printf("ignoring notification %q", msg.Method)
			}
			continue
		}

		resp := s.dispatch(ctx, logger, msg)
		if err := writeMessage(out, resp); err != nil {
			return err
		}
	}
}

// dispatch answers one request. It never returns nil: every request gets a
// response (a result or a JSON-RPC error) so the client is never left hanging.
func (s *Server) dispatch(ctx context.Context, logger *log.Logger, msg *message) *message {
	switch msg.Method {
	case "initialize":
		var p initializeParams
		if len(msg.Params) > 0 {
			if err := json.Unmarshal(msg.Params, &p); err != nil {
				return errorResponse(msg.ID, codeInvalidParams, fmt.Sprintf("invalid initialize params: %v", err))
			}
		}
		resp, err := response(msg.ID, initializeResult{
			ProtocolVersion: negotiateVersion(p.ProtocolVersion),
			Capabilities: serverCapabilities{
				Tools:     map[string]any{},
				Resources: map[string]any{},
			},
			ServerInfo: serverInfo{Name: "depdog", Version: s.version},
		})
		if err != nil {
			return errorResponse(msg.ID, codeInternalError, err.Error())
		}
		return resp

	case "ping":
		return &message{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage("{}")}

	case "tools/list":
		resp, err := response(msg.ID, toolsListResult{Tools: serverTools})
		if err != nil {
			return errorResponse(msg.ID, codeInternalError, err.Error())
		}
		return resp

	case "resources/list":
		resp, err := response(msg.ID, resourcesListResult{Resources: serverResources})
		if err != nil {
			return errorResponse(msg.ID, codeInternalError, err.Error())
		}
		return resp

	case "tools/call":
		return s.callTool(ctx, logger, msg)

	case "resources/read":
		return s.readResource(ctx, logger, msg)

	default:
		return errorResponse(msg.ID, codeMethodNotFound,
			fmt.Sprintf("method %q is not supported by this depdog increment", msg.Method))
	}
}

// callTool dispatches tools/call to the handler. Bad params or an unknown tool
// are JSON-RPC errors; a nil handler (M1 stub) or a handler-reported failure
// becomes a tool error RESULT (isError: true), never a transport error, per
// the MCP spec — the model can read and recover from it.
func (s *Server) callTool(ctx context.Context, logger *log.Logger, msg *message) *message {
	var p callToolParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return errorResponse(msg.ID, codeInvalidParams, fmt.Sprintf("invalid tools/call params: %v", err))
	}

	payload, err := s.runTool(ctx, p)
	if errors.Is(err, errUnknownTool) {
		return errorResponse(msg.ID, codeInvalidParams, err.Error())
	}
	if err != nil {
		logger.Printf("tool %q failed: %v", p.Name, err)
		return toolErrorResult(msg.ID, err.Error())
	}
	resp, mErr := response(msg.ID, callToolResult{
		Content: []textContent{{Type: "text", Text: string(payload)}},
	})
	if mErr != nil {
		return errorResponse(msg.ID, codeInternalError, mErr.Error())
	}
	return resp
}

// errUnknownTool marks a tools/call naming a tool this server does not expose
// — a client mistake worth a JSON-RPC error rather than a tool result.
var errUnknownTool = errors.New("unknown tool")

// runTool routes to the handler by tool name, decoding each tool's arguments.
// A nil handler returns ErrNotWired, surfaced as a tool error result.
func (s *Server) runTool(ctx context.Context, p callToolParams) ([]byte, error) {
	switch p.Name {
	case "check":
		var args struct {
			Path string `json:"path"`
			All  bool   `json:"all"`
		}
		if len(p.Arguments) > 0 {
			if err := json.Unmarshal(p.Arguments, &args); err != nil {
				return nil, fmt.Errorf("invalid check arguments: %v", err)
			}
		}
		if s.handler == nil {
			return nil, ErrNotWired
		}
		return s.handler.Check(ctx, args.Path, args.All)
	case "explain", "can_import":
		var args struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid %s arguments: %v", p.Name, err)
		}
		if args.From == "" || args.To == "" {
			return nil, fmt.Errorf("%s requires both \"from\" and \"to\"", p.Name)
		}
		if s.handler == nil {
			return nil, ErrNotWired
		}
		if p.Name == "explain" {
			return s.handler.Explain(ctx, args.From, args.To)
		}
		return s.handler.CanImport(ctx, args.From, args.To)
	default:
		return nil, fmt.Errorf("%w %q", errUnknownTool, p.Name)
	}
}

// readResource dispatches resources/read to the handler. An unknown URI is a
// JSON-RPC error; a nil handler (M1 stub) or a handler failure is a JSON-RPC
// error too — resources have no per-result error channel like tools do.
func (s *Server) readResource(ctx context.Context, logger *log.Logger, msg *message) *message {
	var p readResourceParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return errorResponse(msg.ID, codeInvalidParams, fmt.Sprintf("invalid resources/read params: %v", err))
	}

	var (
		payload []byte
		err     error
	)
	switch p.URI {
	case uriConfig:
		if s.handler == nil {
			err = ErrNotWired
		} else {
			payload, err = s.handler.Config(ctx)
		}
	case uriComponents:
		if s.handler == nil {
			err = ErrNotWired
		} else {
			payload, err = s.handler.Components(ctx)
		}
	default:
		return errorResponse(msg.ID, codeInvalidParams, fmt.Sprintf("unknown resource %q", p.URI))
	}
	if err != nil {
		logger.Printf("resource %q failed: %v", p.URI, err)
		return errorResponse(msg.ID, codeInternalError, err.Error())
	}
	resp, mErr := response(msg.ID, readResourceResult{
		Contents: []resourceContents{{URI: p.URI, MimeType: mimeJSON, Text: string(payload)}},
	})
	if mErr != nil {
		return errorResponse(msg.ID, codeInternalError, mErr.Error())
	}
	return resp
}

// ErrNotWired is returned by tools/call and resources/read when the server was
// built with a nil handler — the M1 protocol-only stub state, before the CLI
// injects the real closures.
var ErrNotWired = errors.New("not wired yet: this depdog increment ships the protocol only, tool/resource work lands in a later milestone")

// toolErrorResult builds a tools/call result flagged isError, carrying msg as
// the text content — the MCP way to report a tool-level failure the model can
// read, distinct from a transport error.
func toolErrorResult(id *json.RawMessage, msg string) *message {
	resp, err := response(id, callToolResult{
		Content: []textContent{{Type: "text", Text: msg}},
		IsError: true,
	})
	if err != nil {
		return errorResponse(id, codeInternalError, err.Error())
	}
	return resp
}
