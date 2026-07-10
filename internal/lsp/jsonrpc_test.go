package lsp

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
			Params: json.RawMessage(`{"rootUri":"file:///w"}`),
		}},
		{"string id", &message{JSONRPC: "2.0", ID: rawID(`"abc"`), Method: "shutdown"}},
		{"notification", &message{
			JSONRPC: "2.0", Method: "initialized", Params: json.RawMessage(`{}`),
		}},
		{"response with result", &message{
			JSONRPC: "2.0", ID: rawID("2"), Result: json.RawMessage(`{"ok":true}`),
		}},
		{"response with null result", &message{
			JSONRPC: "2.0", ID: rawID("3"), Result: json.RawMessage(`null`),
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
			want := mustJSON(t, tt.msg)
			if g := string(mustJSON(t, got)); g != string(want) {
				t.Errorf("round trip mismatch:\n got %s\nwant %s", g, want)
			}
		})
	}
}

func TestCodecTwoConsecutiveMessages(t *testing.T) {
	var buf bytes.Buffer
	first := &message{JSONRPC: "2.0", ID: rawID("1"), Method: "initialize"}
	second := &message{JSONRPC: "2.0", Method: "initialized"}
	for _, m := range []*message{first, second} {
		if err := writeMessage(&buf, m); err != nil {
			t.Fatal(err)
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
	if got2.Method != "initialized" {
		t.Errorf("second method = %q, want initialized", got2.Method)
	}
	if _, err := readMessage(r); !errors.Is(err, io.EOF) {
		t.Errorf("after both messages err = %v, want io.EOF", err)
	}
}

func TestCodecHeaderVariants(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"initialized"}`
	tests := []struct {
		name  string
		frame string
	}{
		{"canonical", fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)},
		{"lowercase content-length", fmt.Sprintf("content-length: %d\r\n\r\n%s", len(body), body)},
		{"uppercase", fmt.Sprintf("CONTENT-LENGTH: %d\r\n\r\n%s", len(body), body)},
		{"content-type ignored", fmt.Sprintf(
			"Content-Type: application/vscode-jsonrpc; charset=utf-8\r\nContent-Length: %d\r\n\r\n%s",
			len(body), body)},
		{"padded value", fmt.Sprintf("Content-Length:  %d \r\n\r\n%s", len(body), body)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readMessage(bufio.NewReader(strings.NewReader(tt.frame)))
			if err != nil {
				t.Fatalf("readMessage: %v", err)
			}
			if got.Method != "initialized" {
				t.Errorf("method = %q, want initialized", got.Method)
			}
		})
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
	good := `{"jsonrpc":"2.0","method":"exit"}`
	stream := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(bad), bad) +
		fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(good), good)
	r := bufio.NewReader(strings.NewReader(stream))
	_, err := readMessage(r)
	if !errors.Is(err, errBadJSON) {
		t.Fatalf("bad body err = %v, want errBadJSON", err)
	}
	// The framing stayed intact: the next message still reads cleanly.
	got, err := readMessage(r)
	if err != nil {
		t.Fatalf("readMessage after bad body: %v", err)
	}
	if got.Method != "exit" {
		t.Errorf("method = %q, want exit", got.Method)
	}
}
