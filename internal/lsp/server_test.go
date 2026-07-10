package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

// clientInput frames a sequence of raw JSON bodies the way an LSP client would
// send them over stdin.
func clientInput(bodies ...string) *bytes.Buffer {
	var buf bytes.Buffer
	for _, b := range bodies {
		fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}
	return &buf
}

// decodeStream parses every framed message the server wrote to its output.
func decodeStream(t *testing.T, out []byte) []*message {
	t.Helper()
	r := bufio.NewReader(bytes.NewReader(out))
	var msgs []*message
	for {
		m, err := readMessage(r)
		if errors.Is(err, io.EOF) {
			return msgs
		}
		if err != nil {
			t.Fatalf("decoding server output: %v", err)
		}
		msgs = append(msgs, m)
	}
}

// stubResult builds a Result with violations across two files, listed in an
// order that only comes out right if the server actually sorts.
func stubResult() *core.Result {
	return &core.Result{
		ModulePath: "example.com/stub",
		Violations: []core.Violation{
			{
				FromPackage:   "example.com/stub/api",
				FromComponent: "api",
				ImportPath:    "example.com/stub/repo",
				Target:        "repo",
				Rule:          "api: allow [service, std]",
				Positions:     []core.Position{{File: "src/b.go", Line: 7}},
			},
			{
				FromPackage:   "example.com/stub/domain",
				FromComponent: "domain",
				ImportPath:    "example.com/stub/util",
				Target:        "util",
				Rule:          "domain: allow [std]",
				Positions: []core.Position{
					{File: "src/a.go", Line: 9},
					{File: "src/a.go", Line: 3},
				},
			},
		},
	}
}

// stubResultOnlyA is stubResult after the src/b.go violation was fixed: only
// the src/a.go violation remains, so b.go must be cleared on the next round.
func stubResultOnlyA() *core.Result {
	return &core.Result{
		ModulePath: "example.com/stub",
		Violations: []core.Violation{
			{
				FromPackage:   "example.com/stub/domain",
				FromComponent: "domain",
				ImportPath:    "example.com/stub/util",
				Target:        "util",
				Rule:          "domain: allow [std]",
				Positions:     []core.Position{{File: "src/a.go", Line: 9}},
			},
		},
	}
}

func stubCheck(res *core.Result, root string) CheckFunc {
	return func(ctx context.Context) (*core.Result, string, error) {
		return res, root, nil
	}
}

// checkStep is one planned outcome of a seqCheck CheckFunc.
type checkStep struct {
	res *core.Result
	err error
}

// seqCheck returns a CheckFunc that replays the given outcomes in call order,
// failing the test if it is invoked more often than planned.
func seqCheck(t *testing.T, root string, steps []checkStep) CheckFunc {
	call := 0
	return func(ctx context.Context) (*core.Result, string, error) {
		if call >= len(steps) {
			t.Fatalf("CheckFunc call %d, but only %d outcomes were planned", call+1, len(steps))
		}
		s := steps[call]
		call++
		return s.res, root, s.err
	}
}

// publishedParams returns every publishDiagnostics notification in stream
// order, both decoded and as raw params bytes for wire-level assertions.
func publishedParams(t *testing.T, msgs []*message) (decoded []diagsParams, raw []string) {
	t.Helper()
	for _, m := range msgs {
		if m.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var p diagsParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			t.Fatalf("publishDiagnostics params: %v", err)
		}
		decoded = append(decoded, p)
		raw = append(raw, string(m.Params))
	}
	return decoded, raw
}

type diagsParams struct {
	URI         string `json:"uri"`
	Diagnostics []struct {
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
		Severity int    `json:"severity"`
		Code     string `json:"code"`
		Source   string `json:"source"`
		Message  string `json:"message"`
	} `json:"diagnostics"`
}

func TestSessionPublishesDiagnostics(t *testing.T) {
	in := clientInput(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var out, logs bytes.Buffer
	srv := NewServer(stubCheck(stubResult(), "/work/proj"), "1.2.3")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	msgs := decodeStream(t, out.Bytes())
	if len(msgs) != 4 {
		t.Fatalf("got %d server messages, want 4 (init resp, 2 publishes, shutdown resp)", len(msgs))
	}

	// 1) initialize response.
	init := msgs[0]
	if string(*init.ID) != "1" {
		t.Errorf("initialize response id = %s, want 1", string(*init.ID))
	}
	var initRes struct {
		Capabilities json.RawMessage `json:"capabilities"`
		ServerInfo   struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(init.Result, &initRes); err != nil {
		t.Fatalf("initialize result: %v", err)
	}
	if initRes.ServerInfo.Name != "depdog" {
		t.Errorf("serverInfo.name = %q, want depdog", initRes.ServerInfo.Name)
	}
	if initRes.ServerInfo.Version != "1.2.3" {
		t.Errorf("serverInfo.version = %q, want 1.2.3", initRes.ServerInfo.Version)
	}
	var caps struct {
		TextDocumentSync struct {
			OpenClose bool            `json:"openClose"`
			Change    int             `json:"change"`
			Save      json.RawMessage `json:"save"`
		} `json:"textDocumentSync"`
	}
	if err := json.Unmarshal(initRes.Capabilities, &caps); err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if !caps.TextDocumentSync.OpenClose {
		t.Error("textDocumentSync.openClose = false, want true (clients gate didSave delivery on it)")
	}
	if caps.TextDocumentSync.Change != 0 {
		t.Errorf("textDocumentSync.change = %d, want 0 (document contents are never tracked)",
			caps.TextDocumentSync.Change)
	}
	if len(caps.TextDocumentSync.Save) == 0 {
		t.Fatal("textDocumentSync.save is absent — clients will not deliver didSave")
	}
	var save struct {
		IncludeText bool `json:"includeText"`
	}
	if err := json.Unmarshal(caps.TextDocumentSync.Save, &save); err != nil {
		t.Fatalf("textDocumentSync.save: %v", err)
	}
	if save.IncludeText {
		t.Error("save.includeText = true, want false (saved text is ignored; the check re-reads from disk)")
	}

	// 2) publishDiagnostics notifications, sorted by URI, diagnostics by line.
	wantURIs := []string{
		"file:///work/proj/src/a.go",
		"file:///work/proj/src/b.go",
	}
	wantLines := [][]int{{2, 8}, {6}} // 0-based: source lines 3, 9 and 7
	for i, wantURI := range wantURIs {
		m := msgs[1+i]
		if m.Method != "textDocument/publishDiagnostics" {
			t.Fatalf("message %d method = %q, want textDocument/publishDiagnostics", 1+i, m.Method)
		}
		if m.ID != nil {
			t.Errorf("publishDiagnostics %d carries an id — must be a notification", i)
		}
		var p diagsParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			t.Fatalf("publishDiagnostics params: %v", err)
		}
		if p.URI != wantURI {
			t.Errorf("publish %d uri = %q, want %q", i, p.URI, wantURI)
		}
		if len(p.Diagnostics) != len(wantLines[i]) {
			t.Fatalf("publish %d has %d diagnostics, want %d", i, len(p.Diagnostics), len(wantLines[i]))
		}
		for j, d := range p.Diagnostics {
			if d.Range.Start.Line != wantLines[i][j] || d.Range.End.Line != wantLines[i][j] {
				t.Errorf("publish %d diag %d line = %d..%d, want %d",
					i, j, d.Range.Start.Line, d.Range.End.Line, wantLines[i][j])
			}
			if d.Range.Start.Character != 0 || d.Range.End.Character != 0 {
				t.Errorf("publish %d diag %d character = %d..%d, want 0 (line-level)",
					i, j, d.Range.Start.Character, d.Range.End.Character)
			}
			if d.Source != "depdog" {
				t.Errorf("publish %d diag %d source = %q, want depdog", i, j, d.Source)
			}
			if d.Severity != 1 {
				t.Errorf("publish %d diag %d severity = %d, want 1 (Error)", i, j, d.Severity)
			}
		}
	}

	// Spot-check content on the src/a.go diagnostics (domain violation).
	var a diagsParams
	if err := json.Unmarshal(msgs[1].Params, &a); err != nil {
		t.Fatal(err)
	}
	d := a.Diagnostics[0]
	if d.Code != "domain: allow [std]" {
		t.Errorf("code = %q, want the fired rule", d.Code)
	}
	if !strings.Contains(d.Message, "domain: allow [std]") {
		t.Errorf("message %q does not name the fired rule", d.Message)
	}
	if !strings.Contains(d.Message, "example.com/stub/util") {
		t.Errorf("message %q does not name the offending import path", d.Message)
	}

	// 3) shutdown response is null; exit ended the loop cleanly (Serve nil above).
	sd := msgs[3]
	if string(*sd.ID) != "2" {
		t.Errorf("shutdown response id = %s, want 2", string(*sd.ID))
	}
	if string(sd.Result) != "null" {
		t.Errorf("shutdown result = %s, want null", string(sd.Result))
	}
	if sd.Error != nil {
		t.Errorf("shutdown returned error %v", sd.Error)
	}
}

func TestInitializeRootURIWins(t *testing.T) {
	in := clientInput(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"rootUri":"file:///client/root"}}`,
		`{"jsonrpc":"2.0","method":"initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var out, logs bytes.Buffer
	srv := NewServer(stubCheck(stubResult(), "/work/proj"), "test")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeStream(t, out.Bytes())
	var uris []string
	for _, m := range msgs {
		if m.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var p diagsParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			t.Fatal(err)
		}
		uris = append(uris, p.URI)
	}
	want := []string{"file:///client/root/src/a.go", "file:///client/root/src/b.go"}
	if len(uris) != 2 || uris[0] != want[0] || uris[1] != want[1] {
		t.Errorf("uris = %v, want %v", uris, want)
	}
}

func TestUnknownMethodAndNotification(t *testing.T) {
	in := clientInput(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":7,"method":"textDocument/definition","params":{}}`,
		`{"jsonrpc":"2.0","method":"$/setTrace","params":{"value":"off"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var out, logs bytes.Buffer
	checked := 0
	check := func(ctx context.Context) (*core.Result, string, error) {
		checked++
		return &core.Result{}, "/r", nil
	}
	srv := NewServer(check, "test")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v — unknown methods must not kill the session", err)
	}
	if checked != 0 {
		t.Errorf("CheckFunc ran %d times without an initialized notification", checked)
	}

	msgs := decodeStream(t, out.Bytes())
	if len(msgs) != 3 {
		t.Fatalf("got %d server messages, want 3 (init, method-not-found, shutdown)", len(msgs))
	}
	e := msgs[1]
	if string(*e.ID) != "7" {
		t.Errorf("error response id = %s, want 7", string(*e.ID))
	}
	if e.Error == nil || e.Error.Code != codeMethodNotFound {
		t.Fatalf("error = %+v, want code %d (MethodNotFound)", e.Error, codeMethodNotFound)
	}
	if !strings.Contains(e.Error.Message, "textDocument/definition") {
		t.Errorf("error message %q does not name the unknown method", e.Error.Message)
	}
	// The unknown $/setTrace notification produced no reply and the session
	// still shut down cleanly (asserted by Serve returning nil above).
	if string(msgs[2].Result) != "null" {
		t.Errorf("shutdown result = %s, want null", string(msgs[2].Result))
	}
}

func TestExitWithoutShutdownIsNotClean(t *testing.T) {
	in := clientInput(`{"jsonrpc":"2.0","method":"exit"}`)
	var out, logs bytes.Buffer
	srv := NewServer(stubCheck(&core.Result{}, "/r"), "test")
	if err := srv.Serve(context.Background(), in, &out, &logs); err == nil {
		t.Error("Serve returned nil for exit before shutdown, want an error")
	}
}

func TestEOFWithoutExitIsNotClean(t *testing.T) {
	in := clientInput(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
	)
	var out, logs bytes.Buffer
	srv := NewServer(stubCheck(&core.Result{}, "/r"), "test")
	if err := srv.Serve(context.Background(), in, &out, &logs); err == nil {
		t.Error("Serve returned nil after the client vanished mid-session, want an error")
	}
}

func TestCheckErrorIsLoggedNotPublished(t *testing.T) {
	in := clientInput(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var out, logs bytes.Buffer
	check := func(ctx context.Context) (*core.Result, string, error) {
		return nil, "", errors.New("depdog.yaml: boom")
	}
	srv := NewServer(check, "test")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v — a failing check must not kill the session", err)
	}
	for _, m := range decodeStream(t, out.Bytes()) {
		if m.Method == "textDocument/publishDiagnostics" {
			t.Error("published diagnostics although the check failed")
		}
	}
	if !strings.Contains(logs.String(), "boom") {
		t.Errorf("stderr log %q does not carry the check error", logs.String())
	}
}

func TestDidSaveRechecksAndClearsStale(t *testing.T) {
	in := clientInput(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didSave","params":{"textDocument":{"uri":"file:///work/proj/src/b.go"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var out, logs bytes.Buffer
	srv := NewServer(seqCheck(t, "/work/proj", []checkStep{
		{res: stubResult()},      // round 1: violations in src/a.go and src/b.go
		{res: stubResultOnlyA()}, // round 2 (didSave): src/b.go fixed
	}), "test")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	decoded, raw := publishedParams(t, decodeStream(t, out.Bytes()))
	wantURIs := []string{
		"file:///work/proj/src/a.go", // round 1
		"file:///work/proj/src/b.go",
		"file:///work/proj/src/a.go", // round 2: fresh + cleared, sorted by URI
		"file:///work/proj/src/b.go",
	}
	if len(decoded) != len(wantURIs) {
		t.Fatalf("got %d publishes, want %d (2 per round)", len(decoded), len(wantURIs))
	}
	for i, want := range wantURIs {
		if decoded[i].URI != want {
			t.Errorf("publish %d uri = %q, want %q", i, decoded[i].URI, want)
		}
	}
	if len(decoded[2].Diagnostics) != 1 {
		t.Errorf("round 2 src/a.go has %d diagnostics, want 1 (still dirty)", len(decoded[2].Diagnostics))
	}
	if len(decoded[3].Diagnostics) != 0 {
		t.Errorf("round 2 src/b.go has %d diagnostics, want 0 (cleared)", len(decoded[3].Diagnostics))
	}
	if !strings.Contains(raw[3], `"diagnostics":[]`) {
		t.Errorf("cleared publish params = %s, want a literal \"diagnostics\":[] (never null)", raw[3])
	}
}

func TestDidSaveCheckFailureKeepsStaleSet(t *testing.T) {
	in := clientInput(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didSave","params":{"textDocument":{"uri":"file:///work/proj/depdog.yaml"}}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didSave","params":{"textDocument":{"uri":"file:///work/proj/src/b.go"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var out, logs bytes.Buffer
	srv := NewServer(seqCheck(t, "/work/proj", []checkStep{
		{res: stubResult()},                             // round 1: a.go and b.go dirty
		{err: errors.New("depdog.yaml: mid-edit boom")}, // round 2: transient failure
		{res: stubResultOnlyA()},                        // round 3: b.go fixed
	}), "test")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v — a failing didSave check must not kill the session", err)
	}

	decoded, raw := publishedParams(t, decodeStream(t, out.Bytes()))
	// Round 1 publishes both files, the failed round publishes nothing, and the
	// stale set survived the failure: round 3 still clears src/b.go.
	wantURIs := []string{
		"file:///work/proj/src/a.go",
		"file:///work/proj/src/b.go",
		"file:///work/proj/src/a.go",
		"file:///work/proj/src/b.go",
	}
	if len(decoded) != len(wantURIs) {
		t.Fatalf("got %d publishes, want %d (failed round must publish nothing)", len(decoded), len(wantURIs))
	}
	for i, want := range wantURIs {
		if decoded[i].URI != want {
			t.Errorf("publish %d uri = %q, want %q", i, decoded[i].URI, want)
		}
	}
	if len(decoded[3].Diagnostics) != 0 || !strings.Contains(raw[3], `"diagnostics":[]`) {
		t.Errorf("src/b.go was not cleared after the failed round: params = %s", raw[3])
	}
	if !strings.Contains(logs.String(), "boom") {
		t.Errorf("stderr log %q does not carry the didSave check error", logs.String())
	}
}

func TestFixingAllViolationsClearsEverythingThenGoesQuiet(t *testing.T) {
	in := clientInput(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didSave","params":{"textDocument":{"uri":"file:///work/proj/src/a.go"}}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didSave","params":{"textDocument":{"uri":"file:///work/proj/src/a.go"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var out, logs bytes.Buffer
	srv := NewServer(seqCheck(t, "/work/proj", []checkStep{
		{res: stubResult()},   // round 1: a.go and b.go dirty
		{res: &core.Result{}}, // round 2: everything fixed
		{res: &core.Result{}}, // round 3: still clean
	}), "test")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	decoded, raw := publishedParams(t, decodeStream(t, out.Bytes()))
	// Round 2 clears every previously dirty URI (sorted); round 3, with both
	// rounds clean, publishes nothing at all.
	wantURIs := []string{
		"file:///work/proj/src/a.go",
		"file:///work/proj/src/b.go",
		"file:///work/proj/src/a.go",
		"file:///work/proj/src/b.go",
	}
	if len(decoded) != len(wantURIs) {
		t.Fatalf("got %d publishes, want %d (clean-in-both-rounds files must stay silent)",
			len(decoded), len(wantURIs))
	}
	for i, want := range wantURIs {
		if decoded[i].URI != want {
			t.Errorf("publish %d uri = %q, want %q", i, decoded[i].URI, want)
		}
	}
	for i := 2; i < 4; i++ {
		if len(decoded[i].Diagnostics) != 0 {
			t.Errorf("clearing publish %d has %d diagnostics, want 0", i, len(decoded[i].Diagnostics))
		}
		if !strings.Contains(raw[i], `"diagnostics":[]`) {
			t.Errorf("clearing publish %d params = %s, want a literal \"diagnostics\":[] (never null)", i, raw[i])
		}
	}
}

func TestDocumentSyncNotificationsAreIgnored(t *testing.T) {
	in := clientInput(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///w/a.go","languageId":"go","version":1,"text":""}}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":"file:///w/a.go","version":2},"contentChanges":[]}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didClose","params":{"textDocument":{"uri":"file:///w/a.go"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var out, logs bytes.Buffer
	checked := 0
	check := func(ctx context.Context) (*core.Result, string, error) {
		checked++
		return &core.Result{}, "/r", nil
	}
	srv := NewServer(check, "test")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v — didOpen/didChange/didClose must be ignored without error", err)
	}
	if checked != 0 {
		t.Errorf("CheckFunc ran %d times — document sync notifications must not trigger a check", checked)
	}
	msgs := decodeStream(t, out.Bytes())
	if len(msgs) != 2 {
		t.Errorf("got %d server messages, want 2 (init, shutdown) — sync notifications must not be answered", len(msgs))
	}
}
