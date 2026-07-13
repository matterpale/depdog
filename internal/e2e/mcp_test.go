package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// mcpFrame wraps a JSON body in the Content-Length framing an MCP client sends.
func mcpFrame(body string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
}

// mcpSession runs `depdog mcp` in dir, feeding the framed bodies on stdin and
// returning the decoded JSON-RPC responses (results and errors, keyed nowhere —
// the caller matches by id). stdin closing ends the session (MCP has no
// shutdown handshake).
func mcpSession(t *testing.T, dir string, bodies ...string) []map[string]json.RawMessage {
	t.Helper()
	var in bytes.Buffer
	for _, b := range bodies {
		in.WriteString(mcpFrame(b))
	}
	cmd := exec.Command(binary, "mcp")
	cmd.Dir = dir
	// Hermetic against workspaces and terminal styling on dev machines, matching
	// the rest of the e2e suite's run helper.
	cmd.Env = append(os.Environ(), "GOWORK=off", "NO_COLOR=1", "TERM=dumb")
	cmd.Stdin = &in
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("running depdog mcp: %v\nstderr:\n%s", err, errb.String())
		}
	}
	return decodeMCPStream(t, out.Bytes())
}

// decodeMCPStream parses every Content-Length-framed JSON-RPC message.
func decodeMCPStream(t *testing.T, out []byte) []map[string]json.RawMessage {
	t.Helper()
	r := bufio.NewReader(bytes.NewReader(out))
	var msgs []map[string]json.RawMessage
	for {
		var length int
		haveLen := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				if len(msgs) == 0 && !haveLen {
					t.Fatalf("no framed messages in server output:\n%s", out)
				}
				return msgs
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break // end of headers
			}
			if v, ok := strings.CutPrefix(line, "Content-Length: "); ok {
				n, perr := strconv.Atoi(v)
				if perr != nil {
					t.Fatalf("bad Content-Length %q: %v", v, perr)
				}
				length, haveLen = n, true
			}
		}
		if !haveLen {
			return msgs
		}
		body := make([]byte, length)
		if _, err := readFull(r, body); err != nil {
			t.Fatalf("short read of framed body: %v", err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("decoding message %q: %v", body, err)
		}
		msgs = append(msgs, m)
	}
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// byMCPID returns the response with the given id (as a JSON number).
func byMCPID(t *testing.T, msgs []map[string]json.RawMessage, id int) map[string]json.RawMessage {
	t.Helper()
	want := strconv.Itoa(id)
	for _, m := range msgs {
		if raw, ok := m["id"]; ok && string(raw) == want {
			return m
		}
	}
	t.Fatalf("no response with id %d in %d messages", id, len(msgs))
	return nil
}

// mcpToolText decodes a tools/call result and returns its single text block.
func mcpToolText(t *testing.T, resp map[string]json.RawMessage) string {
	t.Helper()
	if _, isErr := resp["error"]; isErr {
		t.Fatalf("expected a tool result, got JSON-RPC error: %s", resp["error"])
	}
	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp["result"], &r); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if r.IsError {
		t.Fatalf("tool result flagged isError: %s", r.Content[0].Text)
	}
	if len(r.Content) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(r.Content))
	}
	return r.Content[0].Text
}

// TestMCPPipedSession drives a full JSON-RPC session over stdio against the
// dirty fixture: initialize → tools/list → tools/call check/explain/can_import
// → resources/read. It is the e2e proof that the real closures answer over the
// wire, not just in a unit test. Deterministic: the check payload's duration_ms
// is not asserted (it varies per run).
func TestMCPPipedSession(t *testing.T) {
	init := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	}
	msgs := mcpSession(t, fixture("dirty"), append(init,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"check","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"explain","arguments":{"from":"internal/handler/checkout","to":"internal/service"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"can_import","arguments":{"from":"handler","to":"service"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":"depdog://config"}}`,
	)...)

	// initialize
	initResp := byMCPID(t, msgs, 1)
	if _, isErr := initResp["error"]; isErr {
		t.Fatalf("initialize error: %s", initResp["error"])
	}

	// tools/list — the three tools are advertised.
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(byMCPID(t, msgs, 2)["result"], &list); err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range list.Tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"check", "explain", "can_import"} {
		if !names[want] {
			t.Errorf("tools/list missing %q", want)
		}
	}

	// tools/call check — the known violation appears in the JSON payload.
	checkJSON := mcpToolText(t, byMCPID(t, msgs, 3))
	var check struct {
		Module     string `json:"module"`
		Violations []struct {
			FromComponent string `json:"from_component"`
			Target        string `json:"target"`
		} `json:"violations"`
	}
	if err := json.Unmarshal([]byte(checkJSON), &check); err != nil {
		t.Fatalf("check payload not JSON: %v\n%s", err, checkJSON)
	}
	if check.Module != "example.test/dirty" {
		t.Errorf("check module = %q, want example.test/dirty", check.Module)
	}
	foundViolation := false
	for _, v := range check.Violations {
		if v.FromComponent == "domain" && v.Target == "repository" {
			foundViolation = true
		}
	}
	if !foundViolation {
		t.Errorf("check JSON missing the dirty fixture's domain → repository violation\n%s", checkJSON)
	}

	// tools/call explain — the deciding rule.
	explainJSON := mcpToolText(t, byMCPID(t, msgs, 4))
	var explain struct {
		Allowed   bool   `json:"allowed"`
		DecidedBy string `json:"decided_by"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(explainJSON), &explain); err != nil {
		t.Fatalf("explain payload not JSON: %v\n%s", err, explainJSON)
	}
	if explain.Allowed {
		t.Error("explain: handler → service should be denied")
	}
	if explain.Reason != "handler: allow [domain, std]" {
		t.Errorf("explain reason = %q, want handler: allow [domain, std]", explain.Reason)
	}

	// tools/call can_import — a boolean verdict.
	canJSON := mcpToolText(t, byMCPID(t, msgs, 5))
	var can struct {
		Allowed   bool   `json:"allowed"`
		DecidedBy string `json:"decided_by"`
	}
	if err := json.Unmarshal([]byte(canJSON), &can); err != nil {
		t.Fatalf("can_import payload not JSON: %v\n%s", err, canJSON)
	}
	if can.Allowed {
		t.Error("can_import: handler → service should be denied")
	}
	if can.DecidedBy != "rule" {
		t.Errorf("can_import decided_by = %q, want rule", can.DecidedBy)
	}

	// resources/read depdog://config — the compiled rule set as JSON.
	cfgResp := byMCPID(t, msgs, 6)
	if _, isErr := cfgResp["error"]; isErr {
		t.Fatalf("resources/read error: %s", cfgResp["error"])
	}
	var read struct {
		Contents []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(cfgResp["result"], &read); err != nil {
		t.Fatalf("resources/read result: %v", err)
	}
	if len(read.Contents) != 1 || read.Contents[0].URI != "depdog://config" {
		t.Fatalf("unexpected config contents: %+v", read.Contents)
	}
	if read.Contents[0].MimeType != "application/json" {
		t.Errorf("config mimeType = %q, want application/json", read.Contents[0].MimeType)
	}
	var cfg struct {
		Default    string          `json:"default"`
		Components json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal([]byte(read.Contents[0].Text), &cfg); err != nil {
		t.Fatalf("config resource not JSON: %v\n%s", err, read.Contents[0].Text)
	}
	if cfg.Default != "deny" {
		t.Errorf("config default = %q, want deny", cfg.Default)
	}
}
