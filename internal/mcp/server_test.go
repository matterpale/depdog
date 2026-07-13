package mcp

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
)

// clientInput frames a sequence of raw JSON bodies the way an MCP client would
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

// serve runs a server over the framed bodies and returns the decoded responses.
// A nil handler exercises the M1 stub (not-wired) state.
func serve(t *testing.T, h Handler, bodies ...string) []*message {
	t.Helper()
	in := clientInput(bodies...)
	var out, logs bytes.Buffer
	srv := NewServer(h, "test-version")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	return decodeStream(t, out.Bytes())
}

// byID returns the first response echoing the given raw id.
func byID(t *testing.T, msgs []*message, id string) *message {
	t.Helper()
	for _, m := range msgs {
		if m.ID != nil && string(*m.ID) == id {
			return m
		}
	}
	t.Fatalf("no response with id %s in %d messages", id, len(msgs))
	return nil
}

func TestInitializeHandshake(t *testing.T) {
	msgs := serve(t,
		nil,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	)
	resp := byID(t, msgs, "1")
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %+v", resp.Error)
	}
	var r struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools     *json.RawMessage `json:"tools"`
			Resources *json.RawMessage `json:"resources"`
		} `json:"capabilities"`
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}
	if r.ProtocolVersion != "2025-06-18" {
		t.Errorf("protocolVersion = %q, want 2025-06-18 (echoed)", r.ProtocolVersion)
	}
	if r.Capabilities.Tools == nil {
		t.Error("capabilities.tools missing")
	}
	if r.Capabilities.Resources == nil {
		t.Error("capabilities.resources missing")
	}
	if r.ServerInfo.Name != "depdog" {
		t.Errorf("serverInfo.name = %q, want depdog", r.ServerInfo.Name)
	}
	if r.ServerInfo.Version != "test-version" {
		t.Errorf("serverInfo.version = %q, want test-version (injected)", r.ServerInfo.Version)
	}
	// The initialized notification is never answered.
	if len(msgs) != 1 {
		t.Errorf("got %d responses, want 1 (the notification must not be answered)", len(msgs))
	}
}

func TestInitializeVersionNegotiation(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{"known version echoed", "2025-06-18", "2025-06-18"},
		{"unknown version falls back to ours", "1999-01-01", "2025-06-18"},
		{"missing version falls back to ours", "", "2025-06-18"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + tt.requested + `"}}`
			if tt.requested == "" {
				body = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
			}
			resp := byID(t, serve(t, nil, body), "1")
			var r struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			if err := json.Unmarshal(resp.Result, &r); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if r.ProtocolVersion != tt.want {
				t.Errorf("protocolVersion = %q, want %q", r.ProtocolVersion, tt.want)
			}
		})
	}
}

func TestToolsList(t *testing.T) {
	resp := byID(t, serve(t, nil, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), "2")
	if resp.Error != nil {
		t.Fatalf("tools/list error: %+v", resp.Error)
	}
	var r struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	if len(r.Tools) != 3 {
		t.Fatalf("got %d tools, want 3", len(r.Tools))
	}
	want := map[string][]string{
		"check":      nil,            // no required fields
		"explain":    {"from", "to"}, // both required
		"can_import": {"from", "to"}, // both required
	}
	for _, tool := range r.Tools {
		reqWant, ok := want[tool.Name]
		if !ok {
			t.Errorf("unexpected tool %q", tool.Name)
			continue
		}
		delete(want, tool.Name)
		if tool.Description == "" {
			t.Errorf("tool %q has no description", tool.Name)
		}
		var schema struct {
			Type       string          `json:"type"`
			Properties json.RawMessage `json:"properties"`
			Required   []string        `json:"required"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("tool %q inputSchema not an object: %v", tool.Name, err)
		}
		if schema.Type != "object" {
			t.Errorf("tool %q inputSchema.type = %q, want object", tool.Name, schema.Type)
		}
		if len(schema.Properties) == 0 {
			t.Errorf("tool %q inputSchema has no properties", tool.Name)
		}
		if strings.Join(schema.Required, ",") != strings.Join(reqWant, ",") {
			t.Errorf("tool %q required = %v, want %v", tool.Name, schema.Required, reqWant)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing tools: %v", want)
	}
}

func TestResourcesList(t *testing.T) {
	resp := byID(t, serve(t, nil, `{"jsonrpc":"2.0","id":3,"method":"resources/list"}`), "3")
	if resp.Error != nil {
		t.Fatalf("resources/list error: %+v", resp.Error)
	}
	var r struct {
		Resources []struct {
			URI         string `json:"uri"`
			Name        string `json:"name"`
			Description string `json:"description"`
			MimeType    string `json:"mimeType"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatalf("unmarshal resources/list: %v", err)
	}
	got := map[string]string{} // name -> uri
	for _, res := range r.Resources {
		got[res.Name] = res.URI
		if res.MimeType != "application/json" {
			t.Errorf("resource %q mimeType = %q, want application/json", res.Name, res.MimeType)
		}
		if res.Description == "" {
			t.Errorf("resource %q has no description", res.Name)
		}
	}
	if got["config"] != "depdog://config" {
		t.Errorf("config uri = %q, want depdog://config", got["config"])
	}
	if got["components"] != "depdog://components" {
		t.Errorf("components uri = %q, want depdog://components", got["components"])
	}
	if len(r.Resources) != 2 {
		t.Errorf("got %d resources, want 2", len(r.Resources))
	}
}

func TestPing(t *testing.T) {
	resp := byID(t, serve(t, nil, `{"jsonrpc":"2.0","id":4,"method":"ping"}`), "4")
	if resp.Error != nil {
		t.Fatalf("ping error: %+v", resp.Error)
	}
	if string(resp.Result) != "{}" {
		t.Errorf("ping result = %s, want {}", string(resp.Result))
	}
}

func TestUnknownMethodIsMethodNotFound(t *testing.T) {
	resp := byID(t, serve(t, nil, `{"jsonrpc":"2.0","id":5,"method":"no/such/method"}`), "5")
	if resp.Error == nil {
		t.Fatal("unknown method returned a result, want an error")
	}
	if resp.Error.Code != codeMethodNotFound {
		t.Errorf("error code = %d, want %d (MethodNotFound)", resp.Error.Code, codeMethodNotFound)
	}
}

func TestEOFEndsSessionCleanly(t *testing.T) {
	// A well-formed request followed by stream close: Serve must return nil.
	in := clientInput(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	var out, logs bytes.Buffer
	srv := NewServer(nil, "test-version")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Errorf("Serve on EOF = %v, want nil (MCP has no shutdown handshake)", err)
	}
	// The one request still got its answer before EOF.
	if len(decodeStream(t, out.Bytes())) != 1 {
		t.Error("the ping before EOF was not answered")
	}
}

func TestEmptyStreamIsClean(t *testing.T) {
	var out, logs bytes.Buffer
	srv := NewServer(nil, "test-version")
	if err := srv.Serve(context.Background(), strings.NewReader(""), &out, &logs); err != nil {
		t.Errorf("Serve on empty stream = %v, want nil", err)
	}
}

func TestBadJSONFrameGetsParseErrorAndKeepsGoing(t *testing.T) {
	bad := "not json"
	good := `{"jsonrpc":"2.0","id":9,"method":"ping"}`
	var in bytes.Buffer
	fmt.Fprintf(&in, "Content-Length: %d\r\n\r\n%s", len(bad), bad)
	fmt.Fprintf(&in, "Content-Length: %d\r\n\r\n%s", len(good), good)
	var out, logs bytes.Buffer
	srv := NewServer(nil, "test-version")
	if err := srv.Serve(context.Background(), &in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v — a bad frame must not kill the session", err)
	}
	msgs := decodeStream(t, out.Bytes())
	if len(msgs) != 2 {
		t.Fatalf("got %d responses, want 2 (parse error + ping)", len(msgs))
	}
	if msgs[0].Error == nil || msgs[0].Error.Code != codeParseError {
		t.Errorf("first response = %+v, want a parse error (%d)", msgs[0].Error, codeParseError)
	}
	if string(*msgs[1].ID) != "9" {
		t.Errorf("second response id = %s, want 9 (the recovered ping)", string(*msgs[1].ID))
	}
}

// --- stub / not-wired dispatch (M1 handler is nil) ---

func TestToolsCallNotWiredIsToolError(t *testing.T) {
	resp := byID(t, serve(t, nil,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"check","arguments":{}}}`), "6")
	// A nil handler is a tool-level failure (isError result), not a transport error.
	if resp.Error != nil {
		t.Fatalf("tools/call returned a transport error, want a tool error result: %+v", resp.Error)
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatalf("unmarshal tools/call result: %v", err)
	}
	if !r.IsError {
		t.Error("tools/call with nil handler is not flagged isError")
	}
	if len(r.Content) == 0 || !strings.Contains(r.Content[0].Text, "not wired yet") {
		t.Errorf("tool error text = %+v, want a 'not wired yet' message", r.Content)
	}
}

func TestToolsCallUnknownToolIsInvalidParams(t *testing.T) {
	resp := byID(t, serve(t, nil,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nope","arguments":{}}}`), "7")
	if resp.Error == nil || resp.Error.Code != codeInvalidParams {
		t.Errorf("unknown tool error = %+v, want code %d", resp.Error, codeInvalidParams)
	}
}

func TestToolsCallMissingRequiredArgIsToolError(t *testing.T) {
	// explain requires both from and to; a nil handler must not be reached
	// with invalid args — the missing arg is caught first as a tool error.
	resp := byID(t, serve(t, nil,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"explain","arguments":{"from":"a"}}}`), "8")
	if resp.Error != nil {
		t.Fatalf("want a tool error result, got transport error: %+v", resp.Error)
	}
	var r struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !r.IsError {
		t.Error("missing required arg not flagged isError")
	}
	if len(r.Content) == 0 || !strings.Contains(r.Content[0].Text, "from") {
		t.Errorf("error text = %+v, want mention of the missing arg", r.Content)
	}
}

func TestResourcesReadNotWiredIsError(t *testing.T) {
	resp := byID(t, serve(t, nil,
		`{"jsonrpc":"2.0","id":10,"method":"resources/read","params":{"uri":"depdog://config"}}`), "10")
	if resp.Error == nil {
		t.Fatal("resources/read with nil handler returned a result, want an error")
	}
	if !strings.Contains(resp.Error.Message, "not wired yet") {
		t.Errorf("error message = %q, want a 'not wired yet' message", resp.Error.Message)
	}
}

func TestResourcesReadUnknownURIIsInvalidParams(t *testing.T) {
	resp := byID(t, serve(t, nil,
		`{"jsonrpc":"2.0","id":11,"method":"resources/read","params":{"uri":"depdog://bogus"}}`), "11")
	if resp.Error == nil || resp.Error.Code != codeInvalidParams {
		t.Errorf("unknown resource error = %+v, want code %d", resp.Error, codeInvalidParams)
	}
}

// --- wired dispatch (a fake handler proves the routing) ---

type fakeHandler struct {
	checkArgs   string
	explainArgs string
}

func (f *fakeHandler) Check(_ context.Context, path string, all bool) ([]byte, error) {
	f.checkArgs = fmt.Sprintf("path=%q all=%v", path, all)
	return []byte(`{"tool":"check"}`), nil
}
func (f *fakeHandler) Explain(_ context.Context, from, to string) ([]byte, error) {
	f.explainArgs = fmt.Sprintf("from=%q to=%q", from, to)
	return []byte(`{"tool":"explain"}`), nil
}
func (f *fakeHandler) CanImport(_ context.Context, from, to string) ([]byte, error) {
	return []byte(`{"tool":"can_import"}`), nil
}
func (f *fakeHandler) Config(_ context.Context) ([]byte, error) {
	return []byte(`{"resource":"config"}`), nil
}
func (f *fakeHandler) Components(_ context.Context) ([]byte, error) {
	return []byte(`{"resource":"components"}`), nil
}

func TestWiredToolsCallDispatches(t *testing.T) {
	h := &fakeHandler{}
	resp := byID(t, serve(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"check","arguments":{"path":"./app","all":true}}}`), "1")
	if resp.Error != nil {
		t.Fatalf("tools/call error: %+v", resp.Error)
	}
	if h.checkArgs != `path="./app" all=true` {
		t.Errorf("Check received %q, want path=\"./app\" all=true", h.checkArgs)
	}
	var r callToolResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.IsError {
		t.Error("wired check flagged isError")
	}
	if len(r.Content) != 1 || r.Content[0].Text != `{"tool":"check"}` {
		t.Errorf("content = %+v, want the handler payload", r.Content)
	}
}

func TestWiredResourcesReadDispatches(t *testing.T) {
	h := &fakeHandler{}
	resp := byID(t, serve(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"depdog://components"}}`), "1")
	if resp.Error != nil {
		t.Fatalf("resources/read error: %+v", resp.Error)
	}
	var r readResourceResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(r.Contents) != 1 {
		t.Fatalf("got %d contents, want 1", len(r.Contents))
	}
	c := r.Contents[0]
	if c.URI != "depdog://components" || c.MimeType != "application/json" || c.Text != `{"resource":"components"}` {
		t.Errorf("content = %+v, want the components payload", c)
	}
}
