package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"

	"github.com/matterpale/depdog/internal/core"
)

// CheckFunc runs one architecture check and returns the result plus the
// absolute project root its Positions are relative to. The server takes it
// injected so this package never learns about config discovery, language
// adapters or cobra — internal/cli owns that wiring.
type CheckFunc func(ctx context.Context) (*core.Result, string, error)

// Server is a stdio LSP server that runs one check per session (after the
// client's initialized notification) and publishes every violation as a
// textDocument/publishDiagnostics notification. Re-checking on didSave is the
// next increment (lsp-02, see docs/lsp.md).
type Server struct {
	check   CheckFunc
	version string // stamped into serverInfo.version (cli.Version)
}

func NewServer(check CheckFunc, version string) *Server {
	return &Server{check: check, version: version}
}

// initializeResult is the subset of the LSP InitializeResult depdog serves.
// textDocumentSync is fully off for this increment: the server neither tracks
// document contents nor re-checks on change.
type initializeResult struct {
	Capabilities serverCapabilities `json:"capabilities"`
	ServerInfo   serverInfo         `json:"serverInfo"`
}

type serverCapabilities struct {
	TextDocumentSync textDocumentSyncOptions `json:"textDocumentSync"`
}

type textDocumentSyncOptions struct {
	OpenClose bool `json:"openClose"`
	Change    int  `json:"change"` // 0 = TextDocumentSyncKind.None
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Serve runs the JSON-RPC loop on in/out until the client sends exit (or the
// stream ends). It is deliberately single-goroutine: messages are handled in
// arrival order and diagnostics are published in sorted order, so a session
// transcript is deterministic. logw (stderr in production) receives every
// operational message — out carries protocol frames only.
//
// Return value: nil after an orderly shutdown → exit handshake; an error when
// the client exits without shutdown or vanishes mid-session, so the CLI can
// exit non-zero as the LSP spec prescribes.
func (s *Server) Serve(ctx context.Context, in io.Reader, out, logw io.Writer) error {
	logger := log.New(logw, "depdog lsp: ", 0)
	r := bufio.NewReader(in)
	var (
		clientRoot string // from initialize rootUri/rootPath, if given
		shutdown   bool
	)
	for {
		msg, err := readMessage(r)
		switch {
		case errors.Is(err, io.EOF):
			if shutdown {
				return nil // client closed the pipe after shutdown: orderly enough
			}
			return errors.New("client closed the stream without shutdown/exit — the session did not end cleanly")
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

		if msg.ID != nil { // request: must be answered
			var resp *message
			switch msg.Method {
			case "initialize":
				clientRoot = rootFromInitialize(msg.Params)
				resp, err = response(msg.ID, initializeResult{
					Capabilities: serverCapabilities{
						TextDocumentSync: textDocumentSyncOptions{OpenClose: false, Change: 0},
					},
					ServerInfo: serverInfo{Name: "depdog", Version: s.version},
				})
				if err != nil {
					return err
				}
			case "shutdown":
				shutdown = true
				resp = &message{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage("null")}
			default:
				resp = errorResponse(msg.ID, codeMethodNotFound,
					fmt.Sprintf("method %q is not supported by this depdog increment", msg.Method))
			}
			if err := writeMessage(out, resp); err != nil {
				return err
			}
			continue
		}

		// Notification: never answered.
		switch msg.Method {
		case "initialized":
			if err := s.publish(ctx, out, logger, clientRoot); err != nil {
				return err
			}
		case "exit":
			if shutdown {
				return nil
			}
			return errors.New("exit received before shutdown — the client skipped the shutdown handshake")
		default:
			// Unknown notifications are ignored, per the LSP spec.
		}
	}
}

// publish runs the injected check once and sends one publishDiagnostics
// notification per violating file. A failing check is logged to stderr and
// publishes nothing — the editor session stays alive so the user can fix the
// config and restart. The returned error is a protocol write failure only.
func (s *Server) publish(ctx context.Context, out io.Writer, logger *log.Logger, clientRoot string) error {
	res, root, err := s.check(ctx)
	if err != nil {
		logger.Printf("check failed, no diagnostics published: %v", err)
		return nil
	}
	if clientRoot != "" {
		root = clientRoot
	}
	params := diagnosticsFor(res, root)
	total := 0
	for _, p := range params {
		n, err := notification("textDocument/publishDiagnostics", p)
		if err != nil {
			return err
		}
		if err := writeMessage(out, n); err != nil {
			return err
		}
		total += len(p.Diagnostics)
	}
	logger.Printf("published %d diagnostic(s) across %d file(s)", total, len(params))
	return nil
}

// rootFromInitialize extracts the workspace root the client announced:
// rootUri (a file:// URI) preferred, the deprecated rootPath as fallback,
// "" when the client sent neither or the URI does not parse.
func rootFromInitialize(params json.RawMessage) string {
	var p struct {
		RootURI  string `json:"rootUri"`
		RootPath string `json:"rootPath"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ""
	}
	if p.RootURI != "" {
		if u, err := url.Parse(p.RootURI); err == nil && u.Scheme == "file" {
			return u.Path
		}
		return ""
	}
	return p.RootPath
}
