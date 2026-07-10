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
	"sort"

	"github.com/matterpale/depdog/internal/core"
)

// CheckFunc runs one architecture check and returns the result plus the
// absolute project root its Positions are relative to. The server takes it
// injected so this package never learns about config discovery, language
// adapters or cobra — internal/cli owns that wiring.
type CheckFunc func(ctx context.Context) (*core.Result, string, error)

// Server is a stdio LSP server that runs the check once the client finishes
// initializing and again on every textDocument/didSave, publishing every
// violation as a textDocument/publishDiagnostics notification. Files that had
// diagnostics in the previous round and are clean now are cleared with an
// empty diagnostics array (lsp-02, see docs/lsp.md). Hover explain is the
// next increment (lsp-03).
type Server struct {
	check   CheckFunc
	version string // stamped into serverInfo.version (cli.Version)
}

func NewServer(check CheckFunc, version string) *Server {
	return &Server{check: check, version: version}
}

// initializeResult is the subset of the LSP InitializeResult depdog serves.
// textDocumentSync announces openClose and save interest — clients gate
// didSave delivery on both — while change stays TextDocumentSyncKind.None:
// the server never tracks document contents (every re-check re-reads the
// project from disk), so didOpen/didClose/didChange are accepted and ignored.
type initializeResult struct {
	Capabilities serverCapabilities `json:"capabilities"`
	ServerInfo   serverInfo         `json:"serverInfo"`
}

type serverCapabilities struct {
	TextDocumentSync textDocumentSyncOptions `json:"textDocumentSync"`
}

type textDocumentSyncOptions struct {
	OpenClose bool        `json:"openClose"`
	Change    int         `json:"change"` // 0 = TextDocumentSyncKind.None
	Save      saveOptions `json:"save"`
}

// saveOptions with IncludeText false: the saved text is useless to us — the
// check reloads the whole project from disk anyway.
type saveOptions struct {
	IncludeText bool `json:"includeText"`
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
		// lastPublished tracks the URIs that received non-empty diagnostics
		// in the previous round, so the next round can clear the ones that
		// went clean. Deliberately Serve-local: the loop is single-goroutine
		// (no lock needed) and the state must not leak across sessions.
		lastPublished map[string]struct{}
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
						TextDocumentSync: textDocumentSyncOptions{
							OpenClose: true,
							Change:    0,
							Save:      saveOptions{IncludeText: false},
						},
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
			next, err := s.publish(ctx, out, logger, clientRoot, lastPublished)
			if err != nil {
				return err
			}
			lastPublished = next
		case "textDocument/didSave":
			// The saved URI is informational only: the check re-reads the
			// whole project from disk, so one save re-checks everything.
			logger.Printf("didSave %s — re-checking", savedURI(msg.Params))
			next, err := s.publish(ctx, out, logger, clientRoot, lastPublished)
			if err != nil {
				return err
			}
			lastPublished = next
		case "exit":
			if shutdown {
				return nil
			}
			return errors.New("exit received before shutdown — the client skipped the shutdown handshake")
		default:
			// Unknown notifications are ignored, per the LSP spec. That
			// includes textDocument/didOpen, didChange and didClose, which
			// clients now send (openClose is advertised) and the server
			// deliberately drops: document contents are never tracked.
		}
	}
}

// publish runs the injected check once and sends one publishDiagnostics
// notification per violating file, plus one with an empty diagnostics array
// for every URI in prev that went clean this round (clients only un-squiggle
// on an explicit empty publish). All notifications of a round are emitted in
// one sorted-by-URI pass over the union of current and cleared URIs, so
// session transcripts stay deterministic. It returns the URI set of this
// round's non-empty publishes, to be passed back in as the next prev.
//
// A failing check is logged to stderr, publishes nothing and returns prev
// unchanged — existing diagnostics stay visible, the session stays alive, and
// the next successful round still clears whatever became stale. The returned
// error is a protocol write failure only.
func (s *Server) publish(ctx context.Context, out io.Writer, logger *log.Logger, clientRoot string, prev map[string]struct{}) (map[string]struct{}, error) {
	res, root, err := s.check(ctx)
	if err != nil {
		logger.Printf("check failed, no diagnostics published: %v", err)
		return prev, nil
	}
	if clientRoot != "" {
		root = clientRoot
	}

	params := diagnosticsFor(res, root)
	byURI := make(map[string]publishDiagnosticsParams, len(params))
	current := make(map[string]struct{}, len(params))
	uris := make([]string, 0, len(params)+len(prev))
	for _, p := range params {
		byURI[p.URI] = p
		current[p.URI] = struct{}{}
		uris = append(uris, p.URI)
	}
	for uri := range prev {
		if _, dirty := current[uri]; !dirty {
			uris = append(uris, uri) // stale: gets an empty-array publish
		}
	}
	sort.Strings(uris)

	total, cleared := 0, 0
	for _, uri := range uris {
		p, dirty := byURI[uri]
		if !dirty {
			// Diagnostics must be a non-nil empty slice: nil marshals to
			// null, and the wire contract for clearing is "diagnostics":[].
			p = publishDiagnosticsParams{URI: uri, Diagnostics: []diagnostic{}}
			cleared++
		}
		n, err := notification("textDocument/publishDiagnostics", p)
		if err != nil {
			return prev, err
		}
		if err := writeMessage(out, n); err != nil {
			return prev, err
		}
		total += len(p.Diagnostics)
	}
	logger.Printf("published %d diagnostic(s) across %d file(s), cleared %d file(s)",
		total, len(params), cleared)
	return current, nil
}

// savedURI extracts textDocument.uri from didSave params for the log line;
// "" when the client sent none or the params do not parse.
func savedURI(params json.RawMessage) string {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ""
	}
	return p.TextDocument.URI
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
