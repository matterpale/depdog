package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

// hoverRules is the rule set of the hover fixture: three components under a
// whitelist policy, a skipped dir, an unsealed boundary separating api from
// domain, and a sealed boundary around secret.
func hoverRules() *core.RuleSet {
	return &core.RuleSet{
		Components: []core.Component{
			{Name: "api", Patterns: []string{"api"}},
			{Name: "domain", Patterns: []string{"domain"}},
			{Name: "service", Patterns: []string{"service"}},
		},
		Rules: map[string]core.Rule{
			"api":    {Allow: []core.Ref{{Kind: core.RefComponent, Name: "service"}, {Kind: core.RefStd}}},
			"domain": {Allow: []core.Ref{{Kind: core.RefStd}}},
			"service": {Allow: []core.Ref{
				{Kind: core.RefComponent, Name: "domain"},
				{Kind: core.RefStd},
				{Kind: core.RefExternalModule, Name: "golang.org/x"},
			}},
		},
		Policy: core.PolicyDeny,
		Skip:   []string{"gen"},
		Boundaries: []core.Boundary{
			{
				Name:    "sealedbox",
				Sealed:  true,
				Members: []core.BoundaryMember{{Patterns: []string{"secret"}, Label: "secret"}},
			},
			{
				Name: "walls",
				Members: []core.BoundaryMember{
					{Patterns: []string{"api"}, Label: "api"},
					{Patterns: []string{"domain"}, Label: "domain"},
				},
			},
		},
	}
}

// hoverGraph exercises every verdict shape: boundary deny (api→domain),
// sealed deny (service→secret), in-module allow (api→service), std allow,
// external deny by whitelist, external-module allow by prefix, a test-only
// deny, an unassigned source (util), a skipped package (gen) and an edge
// into it, plus two edges sharing api/handler.go line 6 and one import (fmt)
// with two positions in the same file.
func hoverGraph() *core.Graph {
	return &core.Graph{
		ModulePath: "example.com/stub",
		Packages: []core.Package{
			{
				ImportPath: "example.com/stub/api", RelDir: "api",
				Imports: []core.Import{
					{Path: "example.com/stub/domain", Class: core.ClassInModule, RelDir: "domain",
						Positions: []core.Position{{File: "api/handler.go", Line: 5}}},
					{Path: "example.com/stub/service", Class: core.ClassInModule, RelDir: "service",
						Positions: []core.Position{{File: "api/handler.go", Line: 6}}},
					{Path: "fmt", Class: core.ClassStd,
						Positions: []core.Position{{File: "api/handler.go", Line: 3}, {File: "api/handler.go", Line: 6}}},
					{Path: "github.com/pkg/errors", Class: core.ClassExternal,
						Positions: []core.Position{{File: "api/handler.go", Line: 4}}},
				},
			},
			{
				ImportPath: "example.com/stub/domain", RelDir: "domain",
				Imports: []core.Import{
					{Path: "strings", Class: core.ClassStd,
						Positions: []core.Position{{File: "domain/order.go", Line: 3}}},
				},
			},
			{
				ImportPath: "example.com/stub/gen", RelDir: "gen",
				Imports: []core.Import{
					{Path: "fmt", Class: core.ClassStd,
						Positions: []core.Position{{File: "gen/z.go", Line: 3}}},
				},
			},
			{ImportPath: "example.com/stub/secret", RelDir: "secret"},
			{
				ImportPath: "example.com/stub/service", RelDir: "service",
				Imports: []core.Import{
					{Path: "example.com/stub/gen", Class: core.ClassInModule, RelDir: "gen",
						Positions: []core.Position{{File: "service/svc.go", Line: 8}}},
					{Path: "example.com/stub/secret", Class: core.ClassInModule, RelDir: "secret",
						Positions: []core.Position{{File: "service/svc.go", Line: 7}}},
					{Path: "example.com/stub/util", Class: core.ClassInModule, RelDir: "util", TestOnly: true,
						Positions: []core.Position{{File: "service/svc_test.go", Line: 4}}},
					{Path: "golang.org/x/sync/errgroup", Class: core.ClassExternal,
						Positions: []core.Position{{File: "service/svc.go", Line: 5}}},
				},
			},
			{
				ImportPath: "example.com/stub/util", RelDir: "util",
				Imports: []core.Import{
					{Path: "fmt", Class: core.ClassStd,
						Positions: []core.Position{{File: "util/u.go", Line: 3}}},
				},
			},
		},
	}
}

func hoverCheck() *Check {
	return &Check{
		Result: &core.Result{ModulePath: "example.com/stub"},
		Graph:  hoverGraph(),
		Rules:  hoverRules(),
		Root:   "/work/proj",
	}
}

// hoverRequest frames one textDocument/hover request body.
func hoverRequest(id int, uri string, line int) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"textDocument/hover","params":{"textDocument":{"uri":%q},"position":{"line":%d,"character":2}}}`,
		id, uri, line)
}

// responseByID finds the response echoing the given numeric request id.
func responseByID(t *testing.T, msgs []*message, id int) *message {
	t.Helper()
	want := fmt.Sprintf("%d", id)
	for _, m := range msgs {
		if m.ID != nil && m.Method == "" && string(*m.ID) == want {
			return m
		}
	}
	t.Fatalf("no response with id %d in the session transcript", id)
	return nil
}

type decodedHover struct {
	Contents struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} `json:"contents"`
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
}

func TestHoverVerdicts(t *testing.T) {
	uri := func(rel string) string { return "file:///work/proj/" + rel }
	cases := []struct {
		name string
		uri  string
		line int // 0-based LSP line
		want string
	}{
		{
			name: "boundary denied",
			uri:  uri("api/handler.go"), line: 4,
			want: "**depdog** — `example.com/stub/api` (api) → `example.com/stub/domain` (domain)\n\n" +
				"denied by `boundary \"walls\"`",
		},
		{
			name: "std allowed",
			uri:  uri("domain/order.go"), line: 2,
			want: "**depdog** — `example.com/stub/domain` (domain) → `strings` (std)\n\n" +
				"allowed by `domain: allow [std]`",
		},
		{
			name: "sealed boundary denied",
			uri:  uri("service/svc.go"), line: 6,
			want: "**depdog** — `example.com/stub/service` (service) → `example.com/stub/secret` (unassigned)\n\n" +
				"denied by `boundary \"sealedbox\" (sealed)`",
		},
		{
			name: "external denied by whitelist",
			uri:  uri("api/handler.go"), line: 3,
			want: "**depdog** — `example.com/stub/api` (api) → `github.com/pkg/errors` (external)\n\n" +
				"denied by `api: allow [service, std]`",
		},
		{
			name: "external module allowed by prefix",
			uri:  uri("service/svc.go"), line: 4,
			want: "**depdog** — `example.com/stub/service` (service) → `golang.org/x/sync/errgroup` (external)\n\n" +
				"allowed by `service: allow [domain, std, golang.org/x]`",
		},
		{
			name: "two edges on one line, sorted by import path",
			uri:  uri("api/handler.go"), line: 5,
			want: "**depdog** — `example.com/stub/api` (api) → `example.com/stub/service` (service)\n\n" +
				"allowed by `api: allow [service, std]`\n\n" +
				"**depdog** — `example.com/stub/api` (api) → `fmt` (std)\n\n" +
				"allowed by `api: allow [service, std]`",
		},
		{
			name: "test-only edge carries the [test] suffix",
			uri:  uri("service/svc_test.go"), line: 3,
			want: "**depdog** — `example.com/stub/service` (service) → `example.com/stub/util` (unassigned)\n\n" +
				"denied by `service: allow [domain, std, golang.org/x]` [test]",
		},
		{
			name: "unassigned source",
			uri:  uri("util/u.go"), line: 2,
			want: "**depdog** — `example.com/stub/util` (unassigned) → `fmt` (std)\n\n" +
				"the source package is unassigned — no rule governs its imports",
		},
		{name: "non-import line", uri: uri("api/handler.go"), line: 0, want: ""},
		{name: "unknown file under the root", uri: uri("api/nope.go"), line: 4, want: ""},
		{name: "URI outside the root", uri: "file:///elsewhere/api/handler.go", line: 4, want: ""},
		{name: "non-file URI", uri: "https://example.com/api/handler.go", line: 4, want: ""},
		{name: "file of a skipped package", uri: uri("gen/z.go"), line: 2, want: ""},
		{name: "edge into a skipped package", uri: uri("service/svc.go"), line: 7, want: ""},
	}

	bodies := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
	}
	for i, c := range cases {
		bodies = append(bodies, hoverRequest(100+i, c.uri, c.line))
	}
	bodies = append(bodies,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)

	var out, logs bytes.Buffer
	chk := hoverCheck()
	srv := NewServer(func(ctx context.Context) (*Check, error) { return chk, nil }, "test", "depdog.yaml")
	if err := srv.Serve(context.Background(), clientInput(bodies...), &out, &logs); err != nil {
		t.Fatalf("Serve: %v\nlogs: %s", err, logs.String())
	}
	msgs := decodeStream(t, out.Bytes())

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := responseByID(t, msgs, 100+i)
			if resp.Error != nil {
				t.Fatalf("hover returned error %+v", resp.Error)
			}
			if c.want == "" {
				if string(resp.Result) != "null" {
					t.Fatalf("hover result = %s, want null", string(resp.Result))
				}
				return
			}
			var h decodedHover
			if err := json.Unmarshal(resp.Result, &h); err != nil {
				t.Fatalf("hover result: %v", err)
			}
			if h.Contents.Kind != "markdown" {
				t.Errorf("contents.kind = %q, want markdown", h.Contents.Kind)
			}
			if h.Contents.Value != c.want {
				t.Errorf("contents.value =\n%q\nwant\n%q", h.Contents.Value, c.want)
			}
			if h.Range.Start.Line != c.line || h.Range.End.Line != c.line {
				t.Errorf("range lines = %d..%d, want %d (the hovered line)",
					h.Range.Start.Line, h.Range.End.Line, c.line)
			}
			if h.Range.Start.Character != 0 || h.Range.End.Character != 0 {
				t.Errorf("range characters = %d..%d, want 0 (line-level, no column data)",
					h.Range.Start.Character, h.Range.End.Character)
			}
		})
	}
}

// TestHoverBeforeFirstRoundIsNull covers a hover arriving between initialize
// and initialized: no check has run, so there is no snapshot to answer from.
func TestHoverBeforeFirstRoundIsNull(t *testing.T) {
	in := clientInput(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		hoverRequest(9, "file:///work/proj/api/handler.go", 4),
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var out, logs bytes.Buffer
	checked := 0
	srv := NewServer(func(ctx context.Context) (*Check, error) {
		checked++
		return hoverCheck(), nil
	}, "test", "depdog.yaml")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if checked != 0 {
		t.Errorf("CheckFunc ran %d times — hover must not trigger a check", checked)
	}
	resp := responseByID(t, decodeStream(t, out.Bytes()), 9)
	if string(resp.Result) != "null" {
		t.Errorf("hover before the first check round = %s, want null", string(resp.Result))
	}
}

// TestHoverAfterFailedRecheckUsesLastSnapshot: a didSave round whose check
// fails must leave the previous snapshot in place, so hover still answers.
func TestHoverAfterFailedRecheckUsesLastSnapshot(t *testing.T) {
	in := clientInput(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didSave","params":{"textDocument":{"uri":"file:///work/proj/depdog.yaml"}}}`,
		hoverRequest(9, "file:///work/proj/domain/order.go", 2),
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var out, logs bytes.Buffer
	srv := NewServer(seqCheck(t, "/work/proj", []checkStep{
		{chk: hoverCheck()},                             // round 1: good snapshot
		{err: errors.New("depdog.yaml: mid-edit boom")}, // round 2: transient failure
	}), "test", "depdog.yaml")
	if err := srv.Serve(context.Background(), in, &out, &logs); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	resp := responseByID(t, decodeStream(t, out.Bytes()), 9)
	var h decodedHover
	if err := json.Unmarshal(resp.Result, &h); err != nil {
		t.Fatalf("hover result after failed re-check = %s: %v", string(resp.Result), err)
	}
	if !strings.Contains(h.Contents.Value, "allowed by `domain: allow [std]`") {
		t.Errorf("hover after a failed re-check = %q, want the last good snapshot's verdict", h.Contents.Value)
	}
}

func TestRelativeFile(t *testing.T) {
	cases := []struct {
		root, uri string
		want      string
		ok        bool
	}{
		{"/work/proj", "file:///work/proj/src/a.go", "src/a.go", true},
		{"/work/proj/", "file:///work/proj/src/a.go", "src/a.go", true}, // trailing slash cleaned
		{"/work/my proj", "file:///work/my%20proj/a.go", "a.go", true},  // percent-encoding decoded
		{"/work/proj", "file:///work/projX/a.go", "", false},            // prefix, not a path boundary
		{"/work/proj", "file:///work/other/a.go", "", false},
		{"/work/proj", "file:///work/proj", "", false}, // the root itself is not a file
		{"/work/proj", "https://work/proj/a.go", "", false},
		{"/work/proj", "not a uri at all ::", "", false},
	}
	for _, c := range cases {
		got, ok := relativeFile(c.root, c.uri)
		if got != c.want || ok != c.ok {
			t.Errorf("relativeFile(%q, %q) = (%q, %v), want (%q, %v)", c.root, c.uri, got, ok, c.want, c.ok)
		}
	}
}

func TestBuildEdgeIndex(t *testing.T) {
	index := buildEdgeIndex(hoverGraph(), hoverRules())

	if _, ok := index["gen/z.go"]; ok {
		t.Error("gen/z.go is indexed although the gen package is skipped")
	}

	svc := index["service/svc.go"]
	var svcPaths []string
	for _, e := range svc {
		svcPaths = append(svcPaths, e.imp.Path)
	}
	// The edge into skipped gen is excluded; the rest sorted by import path.
	want := []string{"example.com/stub/secret", "golang.org/x/sync/errgroup"}
	if len(svcPaths) != len(want) || svcPaths[0] != want[0] || svcPaths[1] != want[1] {
		t.Errorf("service/svc.go edges = %v, want %v (skipped target excluded, sorted)", svcPaths, want)
	}

	api := index["api/handler.go"]
	var apiPaths []string
	for _, e := range api {
		apiPaths = append(apiPaths, e.imp.Path)
	}
	// fmt has two positions in the file but is indexed once; order is by path.
	wantAPI := []string{"example.com/stub/domain", "example.com/stub/service", "fmt", "github.com/pkg/errors"}
	if fmt.Sprint(apiPaths) != fmt.Sprint(wantAPI) {
		t.Errorf("api/handler.go edges = %v, want %v", apiPaths, wantAPI)
	}

	if got := buildEdgeIndex(nil, hoverRules()); got != nil {
		t.Errorf("buildEdgeIndex(nil graph) = %v, want nil", got)
	}
	if got := buildEdgeIndex(hoverGraph(), nil); got != nil {
		t.Errorf("buildEdgeIndex(nil rules) = %v, want nil", got)
	}
}

func TestEdgesAt(t *testing.T) {
	index := buildEdgeIndex(hoverGraph(), hoverRules())

	if got := edgesAt(index, "api/handler.go", 6); len(got) != 2 ||
		got[0].imp.Path != "example.com/stub/service" || got[1].imp.Path != "fmt" {
		t.Errorf("line 6 edges = %+v, want [service, fmt] (multi-edge line, sorted)", got)
	}
	// fmt's second position (line 3) resolves via the same single index entry.
	if got := edgesAt(index, "api/handler.go", 3); len(got) != 1 || got[0].imp.Path != "fmt" {
		t.Errorf("line 3 edges = %+v, want just fmt (multi-position import)", got)
	}
	if got := edgesAt(index, "api/handler.go", 99); len(got) != 0 {
		t.Errorf("line 99 edges = %+v, want none", got)
	}
	if got := edgesAt(index, "unknown.go", 1); len(got) != 0 {
		t.Errorf("unknown file edges = %+v, want none", got)
	}
}

func TestVerdictFor(t *testing.T) {
	rs := hoverRules()
	edge := func(from string, i int) (string, core.Import) {
		for _, p := range hoverGraph().Packages {
			if p.RelDir == from {
				return p.RelDir, p.Imports[i]
			}
		}
		t.Fatalf("no package at %q in the fixture", from)
		return "", core.Import{}
	}

	cases := []struct {
		name   string
		relDir string
		imp    int
		want   edgeVerdict
	}{
		{
			name:   "in-module boundary deny wins over everything",
			relDir: "api", imp: 0, // api → domain
			want: edgeVerdict{Component: "api", Target: "domain", Allowed: false, Reason: `boundary "walls"`},
		},
		{
			name:   "in-module allowed by component rule",
			relDir: "api", imp: 1, // api → service
			want: edgeVerdict{Component: "api", Target: "service", Allowed: true, Reason: "api: allow [service, std]"},
		},
		{
			name:   "std allowed",
			relDir: "api", imp: 2, // api → fmt
			want: edgeVerdict{Component: "api", Target: "std", Allowed: true, Reason: "api: allow [service, std]"},
		},
		{
			name:   "external denied by whitelist fallback",
			relDir: "api", imp: 3, // api → github.com/pkg/errors
			want: edgeVerdict{Component: "api", Target: "external", Allowed: false, Reason: "api: allow [service, std]"},
		},
		{
			name:   "sealed boundary deny into an unassigned member",
			relDir: "service", imp: 1, // service → secret
			want: edgeVerdict{Component: "service", Target: "unassigned", Allowed: false, Reason: `boundary "sealedbox" (sealed)`},
		},
		{
			name:   "external module allowed by prefix ref",
			relDir: "service", imp: 3, // service → golang.org/x/sync/errgroup
			want: edgeVerdict{Component: "service", Target: "external", Allowed: true, Reason: "service: allow [domain, std, golang.org/x]"},
		},
		{
			name:   "in-module unassigned target denied",
			relDir: "service", imp: 2, // service → util (test-only; suffix is the renderer's job)
			want: edgeVerdict{Component: "service", Target: "unassigned", Allowed: false, Reason: "service: allow [domain, std, golang.org/x]"},
		},
		{
			name:   "unassigned source has no verdict",
			relDir: "util", imp: 0, // util → fmt
			want: edgeVerdict{Component: "", Target: "std", Allowed: false, Reason: ""},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			relDir, imp := edge(c.relDir, c.imp)
			got, err := verdictFor(rs, relDir, imp)
			if err != nil {
				t.Fatalf("verdictFor: %v", err)
			}
			if got != c.want {
				t.Errorf("verdictFor = %+v, want %+v", got, c.want)
			}
		})
	}
}
