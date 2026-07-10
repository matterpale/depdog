package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// lspFrames encodes JSON bodies with LSP Content-Length framing, the way an
// editor writes them to the server's stdin.
func lspFrames(bodies ...string) *bytes.Buffer {
	var buf bytes.Buffer
	for _, b := range bodies {
		fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}
	return &buf
}

// lspDecode splits a server's framed output stream back into JSON bodies.
func lspDecode(t *testing.T, out []byte) []json.RawMessage {
	t.Helper()
	r := bufio.NewReader(bytes.NewReader(out))
	var bodies []json.RawMessage
	for {
		length := -1
		for {
			line, err := r.ReadString('\n')
			if err == io.EOF && line == "" && length == -1 {
				return bodies
			}
			if err != nil {
				t.Fatalf("decoding server output: %v", err)
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
				n, err := strconv.Atoi(strings.TrimSpace(value))
				if err != nil {
					t.Fatalf("bad Content-Length in server output: %v", err)
				}
				length = n
			}
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(r, body); err != nil {
			t.Fatalf("reading server output body: %v", err)
		}
		bodies = append(bodies, json.RawMessage(body))
	}
}

// TestLSPRealWiring drives `depdog lsp` end to end — real evaluateModule, real
// TypeScript adapter — against the ts-dirty fixture, whose known violation is
// src/api/server.ts importing ../domain/order on line 6 (api may not reach
// past service into domain).
func TestLSPRealWiring(t *testing.T) {
	cfg := mustAbs(t, filepath.Join("..", "..", "testdata", "fixtures", "ts-dirty", "depdog.yaml"))
	root := filepath.Dir(cfg)

	in := lspFrames(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var out, errOut bytes.Buffer
	cmd := Root()
	cmd.SetIn(in)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"lsp", "--config", cfg})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("depdog lsp: %v\nstderr: %s", err, errOut.String())
	}

	wantPath := filepath.ToSlash(filepath.Join(root, "src", "api", "server.ts"))
	if !strings.HasPrefix(wantPath, "/") {
		wantPath = "/" + wantPath // Windows drive paths need the leading slash file URIs require
	}
	wantURI := "file://" + wantPath
	found := false
	for _, body := range lspDecode(t, out.Bytes()) {
		var m struct {
			Method string `json:"method"`
			Params struct {
				URI         string `json:"uri"`
				Diagnostics []struct {
					Range struct {
						Start struct {
							Line int `json:"line"`
						} `json:"start"`
					} `json:"range"`
					Source  string `json:"source"`
					Message string `json:"message"`
				} `json:"diagnostics"`
			} `json:"params"`
		}
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("server message: %v", err)
		}
		if m.Method != "textDocument/publishDiagnostics" || m.Params.URI != wantURI {
			continue
		}
		for _, d := range m.Params.Diagnostics {
			if d.Range.Start.Line != 5 { // source line 6, 0-based
				continue
			}
			if d.Source != "depdog" {
				t.Errorf("source = %q, want depdog", d.Source)
			}
			if !strings.Contains(d.Message, "../domain/order") {
				t.Errorf("message %q does not name the offending import", d.Message)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("no publishDiagnostics for %s line 6 — the fixture's known violation is missing\noutput: %s",
			wantURI, out.String())
	}
}
