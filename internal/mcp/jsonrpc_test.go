package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// mustJSON marshals v or fails the test — keeps table entries terse.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rawID(s string) *json.RawMessage {
	r := json.RawMessage(s)
	return &r
}

func TestCodecRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  *message
	}{
		{"request with params", &message{
			JSONRPC: "2.0", ID: rawID("1"), Method: "initialize",
			Params: json.RawMessage(`{"protocolVersion":"2025-06-18"}`),
		}},
		{"string id", &message{JSONRPC: "2.0", ID: rawID(`"abc"`), Method: "ping"}},
		{"notification", &message{
			JSONRPC: "2.0", Method: "notifications/initialized", Params: json.RawMessage(`{}`),
		}},
		{"response with result", &message{
			JSONRPC: "2.0", ID: rawID("2"), Result: json.RawMessage(`{"ok":true}`),
		}},
		{"response with empty result", &message{
			JSONRPC: "2.0", ID: rawID("3"), Result: json.RawMessage(`{}`),
		}},
		{"error response", &message{
			JSONRPC: "2.0", ID: rawID("4"),
			Error: &responseError{Code: codeMethodNotFound, Message: "no such method"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeMessage(&buf, tt.msg); err != nil {
				t.Fatalf("writeMessage: %v", err)
			}
			if !strings.HasPrefix(buf.String(), "Content-Length: ") {
				t.Fatalf("frame missing Content-Length header: %q", buf.String())
			}
			got, err := readMessage(bufio.NewReader(&buf))
			if err != nil {
				t.Fatalf("readMessage: %v", err)
			}
			if g := string(mustJSON(t, got)); g != string(mustJSON(t, tt.msg)) {
				t.Errorf("round trip mismatch:\n got %s\nwant %s", g, mustJSON(t, tt.msg))
			}
		})
	}
}

func TestCodecTwoConsecutiveMessages(t *testing.T) {
	var buf bytes.Buffer
	first := &message{JSONRPC: "2.0", ID: rawID("1"), Method: "initialize"}
	second := &message{JSONRPC: "2.0", Method: "notifications/initialized"}
	for _, m := range []*message{first, second} {
		if err := writeMessage(&buf, m); err != nil {
			t.Fatalf("writeMessage: %v", err)
		}
	}
	r := bufio.NewReader(&buf)
	got1, err := readMessage(r)
	if err != nil {
		t.Fatalf("first readMessage: %v", err)
	}
	if got1.Method != "initialize" {
		t.Errorf("first method = %q, want initialize", got1.Method)
	}
	got2, err := readMessage(r)
	if err != nil {
		t.Fatalf("second readMessage: %v", err)
	}
	if got2.Method != "notifications/initialized" {
		t.Errorf("second method = %q, want notifications/initialized", got2.Method)
	}
	if _, err := readMessage(r); !errors.Is(err, io.EOF) {
		t.Errorf("third readMessage err = %v, want EOF", err)
	}
}

func TestCodecMalformedFrames(t *testing.T) {
	tests := []struct {
		name  string
		frame string
	}{
		{"no content-length", "Content-Type: text/plain\r\n\r\n{}"},
		{"non-numeric length", "Content-Length: many\r\n\r\n{}"},
		{"negative length", "Content-Length: -5\r\n\r\n{}"},
		{"header without colon", "Content-Length 12\r\n\r\n{}"},
		{"truncated body", "Content-Length: 100\r\n\r\n{\"jsonrpc\":\"2.0\"}"},
		{"headers cut off", "Content-Length: 12\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := readMessage(bufio.NewReader(strings.NewReader(tt.frame))); err == nil {
				t.Error("readMessage accepted a malformed frame, want error")
			}
		})
	}
}

func TestCodecBadJSONBodyIsRecoverable(t *testing.T) {
	bad := "not json at all"
	good := `{"jsonrpc":"2.0","method":"ping"}`
	stream := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(bad), bad) +
		fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(good), good)
	r := bufio.NewReader(strings.NewReader(stream))
	if _, err := readMessage(r); !errors.Is(err, errBadJSON) {
		t.Fatalf("bad body err = %v, want errBadJSON", err)
	}
	// The framing stayed intact: the next message still reads cleanly.
	got, err := readMessage(r)
	if err != nil {
		t.Fatalf("readMessage after bad body: %v", err)
	}
	if got.Method != "ping" {
		t.Errorf("method = %q, want ping", got.Method)
	}
}
