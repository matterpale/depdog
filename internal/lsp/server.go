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
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// Check is one architecture-check snapshot: the evaluation result plus the
// graph and rule set it was computed from, and the absolute project root its
// Positions are relative to. Diagnostics and hover both answer from the same
// snapshot, so they can never describe different check rounds.
type Check struct {
	Result *core.Result
	Graph  *core.Graph
	Rules  *core.RuleSet
	Root   string
	// Rel is the checked module's directory relative to the client's announced
	// workspace root (slash-separated), or "" for a single-module project. In a
	// Go workspace the server checks the member owning the triggering file, so
	// Rel ("app") rebases the member's Positions onto the client's root.
	Rel string
}

// CheckFunc runs one architecture check and returns its snapshot. path is the
// filesystem path of the file that triggered this round (a saved or watched
// file), or "" when none is known (the initialize round); a workspace check
// uses it to resolve the file's owning member. The server takes CheckFunc
// injected so this package never learns about config discovery, language
// adapters or cobra — internal/cli owns that wiring.
type CheckFunc func(ctx context.Context, path string) (*Check, error)

// Server is a stdio LSP server that runs the check once the client finishes
// initializing and again on every textDocument/didSave, publishing every
// violation as a textDocument/publishDiagnostics notification. Files that had
// diagnostics in the previous round and are clean now are cleared with an
// empty diagnostics array (lsp-02, see docs/lsp.md). textDocument/hover on an
// import line renders the `depdog explain` verdict for that edge (lsp-03,
// hover.go). When the client supports dynamic registration of
// workspace/didChangeWatchedFiles, the server asks it to watch the config
// file, so external edits (git checkout, terminal, `depdog baseline`)
// re-check too (lsp-04); clients without that capability keep the exact
// didSave-only behavior.
type Server struct {
	check      CheckFunc
	version    string // stamped into serverInfo.version (cli.Version)
	configBase string // basename of the loaded config file, watched via didChangeWatchedFiles
}

// NewServer builds a Server around the injected check. configBase is the
// basename of the loaded config file (e.g. "depdog.yaml"); when the client
// supports dynamic registration of workspace/didChangeWatchedFiles, the
// server asks it to watch "**/"+configBase so external config edits re-check
// without a save. An empty configBase disables the registration.
func NewServer(check CheckFunc, version, configBase string) *Server {
	return &Server{check: check, version: version, configBase: configBase}
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
	HoverProvider    bool                    `json:"hoverProvider"`
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

// watchRegistrationID is the server-issued JSON-RPC id of the session's
// single client/registerCapability request, and the registration id inside
// it. One constant is enough: the server sends exactly one server→client
// request per session, so routing the client's response is a string
// comparison instead of a pending-request table.
const watchRegistrationID = "depdog-watch-1"

// registrationParams is the client/registerCapability payload asking the
// client to watch the config file. workspace/didChangeWatchedFiles has no
// static server capability — dynamic registration is the only way to get it —
// which is why depdog needs its one server→client request here.
type registrationParams struct {
	Registrations []registration `json:"registrations"`
}

type registration struct {
	ID              string       `json:"id"`
	Method          string       `json:"method"`
	RegisterOptions watchOptions `json:"registerOptions"`
}

type watchOptions struct {
	Watchers []fileSystemWatcher `json:"watchers"`
}

// fileSystemWatcher deliberately omits `kind`: the LSP default is 7
// (create|change|delete), and depdog wants all three — a config file being
// created or deleted changes the verdict as much as an edit does.
type fileSystemWatcher struct {
	GlobPattern string `json:"globPattern"`
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
		// canWatch records whether the client announced dynamic-registration
		// support for workspace/didChangeWatchedFiles in initialize; only then
		// is the config-watcher registration sent on initialized.
		canWatch bool
		// watchPending is true between sending the client/registerCapability
		// request and consuming the client's response to it.
		watchPending bool
		// lastPublished tracks the URIs that received non-empty diagnostics
		// in the previous round, so the next round can clear the ones that
		// went clean. Deliberately Serve-local: the loop is single-goroutine
		// (no lock needed) and the state must not leak across sessions.
		lastPublished map[string]struct{}
		// hover is the last successful round's snapshot plus its file→edge
		// index; textDocument/hover answers from it. nil until the first
		// successful check; a failed re-check keeps the previous value, the
		// same way stale diagnostics stay visible.
		hover *hoverState
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

		if msg.ID != nil && msg.Method == "" {
			// An id without a method is a RESPONSE to a server→client request
			// — it must be consumed, never answered (routing it into the
			// request branch below would bounce a MethodNotFound error back at
			// the client for its own answer). The only request this server
			// sends is the watcher registration.
			if watchPending && string(*msg.ID) == strconv.Quote(watchRegistrationID) {
				watchPending = false
				if msg.Error != nil {
					logger.Printf("client refused the %q watcher registration (code %d: %s) — external config edits will not re-check; saving a file still does",
						s.configBase, msg.Error.Code, msg.Error.Message)
				}
			} else {
				logger.Printf("dropping response to unknown request id %s", string(*msg.ID))
			}
			continue
		}

		if msg.ID != nil { // request: must be answered
			var resp *message
			switch msg.Method {
			case "initialize":
				clientRoot = rootFromInitialize(msg.Params)
				canWatch = watchDynamicRegistration(msg.Params)
				resp, err = response(msg.ID, initializeResult{
					Capabilities: serverCapabilities{
						TextDocumentSync: textDocumentSyncOptions{
							OpenClose: true,
							Change:    0,
							Save:      saveOptions{IncludeText: false},
						},
						HoverProvider: true,
					},
					ServerInfo: serverInfo{Name: "depdog", Version: s.version},
				})
				if err != nil {
					return err
				}
			case "textDocument/hover":
				resp, err = s.hoverResponse(logger, msg, hover)
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
			// Register the config watcher before the first round, so a
			// capable client covers the whole session: external edits to
			// depdog.yaml (git checkout, terminal, `depdog baseline`)
			// re-check without a save. Incapable clients get nothing and
			// behave exactly as before this increment.
			if canWatch && s.configBase != "" {
				req, err := request(watchRegistrationID, "client/registerCapability", registrationParams{
					Registrations: []registration{{
						ID:     watchRegistrationID,
						Method: "workspace/didChangeWatchedFiles",
						RegisterOptions: watchOptions{
							Watchers: []fileSystemWatcher{{GlobPattern: "**/" + s.configBase}},
						},
					}},
				})
				if err != nil {
					return err
				}
				if err := writeMessage(out, req); err != nil {
					return err
				}
				watchPending = true
				logger.Printf("asked the client to watch **/%s", s.configBase)
			}
			// No file has been named yet: resolve from the process working
			// directory (in a workspace this only pins a member once the client
			// saves or watches a file inside one).
			next, snap, err := s.publish(ctx, out, logger, clientRoot, "", lastPublished)
			if err != nil {
				return err
			}
			lastPublished = next
			if snap != nil {
				hover = snap
			}
		case "workspace/didChangeWatchedFiles":
			// The client watched the config file for us (see the
			// registration above). However many FileEvents the notification
			// carries, they coalesce into ONE re-check: the check re-reads
			// the whole project from disk anyway. Failure semantics are
			// identical to didSave — publish keeps the previous diagnostics,
			// stale set and hover snapshot on a failed check.
			changed := changedURIs(msg.Params)
			logger.Printf("didChangeWatchedFiles %s — re-checking", strings.Join(changed, " "))
			var trigger string
			if len(changed) > 0 {
				trigger = pathFromURI(changed[0])
			}
			next, snap, err := s.publish(ctx, out, logger, clientRoot, trigger, lastPublished)
			if err != nil {
				return err
			}
			lastPublished = next
			if snap != nil {
				hover = snap
			}
		case "textDocument/didSave":
			// The saved URI is informational only: the check re-reads the
			// whole project from disk, so one save re-checks everything.
			saved := savedURI(msg.Params)
			logger.Printf("didSave %s — re-checking", saved)
			next, snap, err := s.publish(ctx, out, logger, clientRoot, pathFromURI(saved), lastPublished)
			if err != nil {
				return err
			}
			lastPublished = next
			if snap != nil {
				hover = snap
			}
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
// round's non-empty publishes (to be passed back in as the next prev) plus
// the round's hover snapshot: the Check and a file→edge index over its graph.
//
// A failing check is logged to stderr, publishes nothing and returns prev
// unchanged with a nil snapshot — existing diagnostics and the previous hover
// snapshot stay live, the session stays alive, and the next successful round
// still clears whatever became stale. The returned error is a protocol write
// failure only.
func (s *Server) publish(ctx context.Context, out io.Writer, logger *log.Logger, clientRoot, triggerPath string, prev map[string]struct{}) (map[string]struct{}, *hoverState, error) {
	chk, err := s.check(ctx, triggerPath)
	if err != nil {
		logger.Printf("check failed, no diagnostics published: %v", err)
		return prev, nil, nil
	}
	root := chk.Root
	if clientRoot != "" {
		// The client's announced root wins for URI construction; a workspace
		// member rebases onto it via Rel, so its files resolve under the root
		// the editor opened (Rel is "" for a single-module project).
		root = filepath.Join(clientRoot, filepath.FromSlash(chk.Rel))
	}

	var configURI string
	if s.configBase != "" {
		configURI = fileURI(root, s.configBase)
	}
	params := diagnosticsFor(chk.Result, root, configURI)
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
			return prev, nil, err
		}
		if err := writeMessage(out, n); err != nil {
			return prev, nil, err
		}
		total += len(p.Diagnostics)
	}
	logger.Printf("published %d diagnostic(s) across %d file(s), cleared %d file(s)",
		total, len(params), cleared)
	return current, &hoverState{check: chk, root: root, configURI: configURI, index: buildEdgeIndex(chk.Graph, chk.Rules)}, nil
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

// pathFromURI turns a file:// URI into a local filesystem path, or "" when the
// URI is empty, unparseable, or not a file URI. The server uses it to learn
// which file a didSave/didChangeWatchedFiles round is about, so the check can
// resolve that file's workspace member.
func pathFromURI(uri string) string {
	if uri == "" {
		return ""
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return u.Path
}

// watchDynamicRegistration reports whether the initialize params announce
// client support for dynamically registering workspace/didChangeWatchedFiles
// — the gate for the config-watcher registration.
func watchDynamicRegistration(params json.RawMessage) bool {
	var p struct {
		Capabilities struct {
			Workspace struct {
				DidChangeWatchedFiles struct {
					DynamicRegistration bool `json:"dynamicRegistration"`
				} `json:"didChangeWatchedFiles"`
			} `json:"workspace"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return false
	}
	return p.Capabilities.Workspace.DidChangeWatchedFiles.DynamicRegistration
}

// changedURIs extracts the FileEvent URIs from didChangeWatchedFiles params
// for the log line; nil when the params do not parse.
func changedURIs(params json.RawMessage) []string {
	var p struct {
		Changes []struct {
			URI string `json:"uri"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	uris := make([]string, 0, len(p.Changes))
	for _, c := range p.Changes {
		uris = append(uris, c.URI)
	}
	return uris
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
