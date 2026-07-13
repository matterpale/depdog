package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// toolResult decodes a tools/call result into its text block and isError flag.
// A transport-level error fails the test — a tool-level failure is an isError
// RESULT, not a JSON-RPC error (D6).
func toolResult(t *testing.T, resp *message) (text string, isError bool) {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("expected a tool result, got JSON-RPC error %+v", resp.Error)
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if len(r.Content) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(r.Content))
	}
	if r.Content[0].Type != "text" {
		t.Errorf("content type = %q, want text", r.Content[0].Type)
	}
	return r.Content[0].Text, r.IsError
}

// TestWiredExplainDispatches proves the explain tool decodes from/to, reaches
// the handler, and passes its JSON payload through as a text result. (M1's
// wired tests cover only check + resources/read.)
func TestWiredExplainDispatches(t *testing.T) {
	h := &fakeHandler{}
	resp := byID(t, serve(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"explain","arguments":{"from":"a","to":"b"}}}`), "1")
	text, isErr := toolResult(t, resp)
	if isErr {
		t.Fatalf("explain flagged isError: %s", text)
	}
	if text != `{"tool":"explain"}` {
		t.Errorf("explain text = %q, want the handler payload", text)
	}
	if h.explainArgs != `from="a" to="b"` {
		t.Errorf("Explain received %q, want from=\"a\" to=\"b\"", h.explainArgs)
	}
}

// TestWiredCanImportDispatches proves the can_import tool routes to the handler
// (it shares argument decoding with explain but a distinct handler method).
func TestWiredCanImportDispatches(t *testing.T) {
	h := &fakeHandler{}
	resp := byID(t, serve(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"can_import","arguments":{"from":"a","to":"b"}}}`), "1")
	text, isErr := toolResult(t, resp)
	if isErr {
		t.Fatalf("can_import flagged isError: %s", text)
	}
	if text != `{"tool":"can_import"}` {
		t.Errorf("can_import text = %q, want the handler payload", text)
	}
}

// TestWiredConfigResourceDispatches proves the config resource routes to the
// handler (M1 covers only the components resource).
func TestWiredConfigResourceDispatches(t *testing.T) {
	h := &fakeHandler{}
	resp := byID(t, serve(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"depdog://config"}}`), "1")
	if resp.Error != nil {
		t.Fatalf("resources/read error: %+v", resp.Error)
	}
	var r readResourceResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(r.Contents) != 1 || r.Contents[0].URI != "depdog://config" ||
		r.Contents[0].Text != `{"resource":"config"}` {
		t.Errorf("contents = %+v, want the config payload", r.Contents)
	}
}

// TestToolsCallBadArgumentsJSON: arguments that are not an object are a
// per-tool decode failure surfaced as a tool error result, not a crash.
func TestToolsCallBadArgumentsJSON(t *testing.T) {
	resp := byID(t, serve(t, &fakeHandler{},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"explain","arguments":"not-an-object"}}`), "1")
	_, isErr := toolResult(t, resp)
	if !isErr {
		t.Fatal("malformed arguments should be a tool error result")
	}
}

// errHandler fails every method — it proves a wired handler's failure becomes a
// tool error RESULT (tools) or a JSON-RPC error (resources), never a crash.
type errHandler struct{ err error }

func (e errHandler) Check(context.Context, string, bool) ([]byte, error) { return nil, e.err }
func (e errHandler) Explain(context.Context, string, string) ([]byte, error) {
	return nil, e.err
}
func (e errHandler) CanImport(context.Context, string, string) ([]byte, error) {
	return nil, e.err
}
func (e errHandler) Config(context.Context) ([]byte, error)     { return nil, e.err }
func (e errHandler) Components(context.Context) ([]byte, error) { return nil, e.err }

// TestToolsCallHandlerErrorIsToolResult: a wired handler that returns an error
// (an unresolvable ref, a config load failure) becomes an isError tool result
// carrying the message — the session keeps running.
func TestToolsCallHandlerErrorIsToolResult(t *testing.T) {
	h := errHandler{err: errors.New("boom")}
	resp := byID(t, serve(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"check","arguments":{}}}`), "1")
	text, isErr := toolResult(t, resp)
	if !isErr {
		t.Fatalf("a handler error should flag isError, got %q", text)
	}
	if text != "boom" {
		t.Errorf("tool error text = %q, want boom", text)
	}
}

// TestResourcesReadHandlerErrorIsRPCError: resources have no per-result error
// channel, so a wired handler failure is a JSON-RPC internal error.
func TestResourcesReadHandlerErrorIsRPCError(t *testing.T) {
	h := errHandler{err: errors.New("cannot load config")}
	resp := byID(t, serve(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"depdog://config"}}`), "1")
	if resp.Error == nil {
		t.Fatal("a resource handler error should be a JSON-RPC error")
	}
	if resp.Error.Code != codeInternalError {
		t.Errorf("error code = %d, want %d (InternalError)", resp.Error.Code, codeInternalError)
	}
}
