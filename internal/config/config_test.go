package config

import (
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

const valid = `
version: 1
components:
  main:   ["cmd/**"]
  domain: ["internal/domain/**"]
policy: deny
rules:
  main:   { allow: ["*"] }
  domain: { allow: [std] }
options:
  test_files: hybrid
  skip: ["internal/legacy/**"]
`

func TestParseValid(t *testing.T) {
	rs, err := Parse([]byte(valid))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rs.Components) != 2 {
		t.Errorf("components = %d, want 2", len(rs.Components))
	}
	if rs.Components[0].Name != "domain" { // sorted for determinism
		t.Errorf("first component = %q, want domain", rs.Components[0].Name)
	}
	if rs.Policy != core.PolicyDeny || rs.TestFiles != core.TestHybrid {
		t.Errorf("policy/test_files not compiled: %+v", rs)
	}
	if len(rs.Rules["main"].Allow) != 1 || rs.Rules["main"].Allow[0].Kind != core.RefAny {
		t.Errorf("main rule = %+v", rs.Rules["main"])
	}
}

func TestParseDefaultPolicy(t *testing.T) {
	// policy is optional; absent it defaults to the strict deny stance.
	rs, err := Parse([]byte("version: 1\ncomponents: {a: [\"x/**\"]}\nrules: {a: {allow: [std]}}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if rs.Policy != core.PolicyDeny {
		t.Errorf("default policy = %v, want deny", rs.Policy)
	}
}

func TestParseGroups(t *testing.T) {
	rs, err := Parse([]byte(`
version: 1
components:
  ui:     ["internal/ui/**"]
  app:    ["internal/app/**"]
  domain: ["internal/domain/**"]
groups:
  inner: [app, domain]
policy: deny
rules:
  ui: { allow: [inner, std] }
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	allow := rs.Rules["ui"].Allow
	got := map[string]bool{}
	for _, r := range allow {
		got[r.String()] = true
	}
	for _, want := range []string{"app", "domain", "std"} {
		if !got[want] {
			t.Errorf("ui allow should expand the group to include %q: %+v", want, allow)
		}
	}
	if len(allow) != 3 {
		t.Errorf("ui allow = %d refs, want 3 (app, domain, std)", len(allow))
	}
}

func TestParseExternalModuleRef(t *testing.T) {
	rs, err := Parse([]byte("version: 1\ncomponents: {a: [\"x/**\"]}\npolicy: deny\nrules: {a: {allow: [std, \"golang.org/x/sync\"]}}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	allow := rs.Rules["a"].Allow
	if len(allow) != 2 {
		t.Fatalf("allow = %+v, want 2 refs", allow)
	}
	found := false
	for _, r := range allow {
		if r.Kind == core.RefExternalModule && r.Name == "golang.org/x/sync" {
			found = true
		}
	}
	if !found {
		t.Errorf("a module-path ref should parse to an external-module ref: %+v", allow)
	}
}

func TestParseScalarPattern(t *testing.T) {
	rs, err := Parse([]byte("version: 1\ncomponents:\n  main: cmd/**\npolicy: deny\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := rs.Components[0].Patterns; len(got) != 1 || got[0] != "cmd/**" {
		t.Errorf("patterns = %v", got)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, yaml, wantErr string
	}{
		{"empty", "", "empty"},
		{"bad version", "version: 2\ncomponents: {a: [\"x/**\"]}\npolicy: deny", "version 2"},
		{"no components", "version: 1\npolicy: deny", `no "components"`},
		{"reserved name", "version: 1\ncomponents: {std: [\"x/**\"]}\npolicy: deny", "reserved"},
		{"empty patterns", "version: 1\ncomponents: {a: []}\npolicy: deny", "no patterns"},
		{"bad glob", "version: 1\ncomponents: {a: [\"x/[bad/**\"]}\npolicy: deny", "segment"},
		{"bad policy", "version: 1\ncomponents: {a: [\"x/**\"]}\npolicy: strict", "policy must be"},
		{"rule for unknown", "version: 1\ncomponents: {a: [\"x/**\"]}\npolicy: deny\nrules: {b: {allow: [std]}}", `unknown component "b"`},
		{"unknown ref", "version: 1\ncomponents: {a: [\"x/**\"]}\npolicy: deny\nrules: {a: {allow: [nope]}}", `unknown component or group "nope"`},
		{"group unknown member", "version: 1\ncomponents: {a: [\"x/**\"]}\ngroups: {g: [nope]}\npolicy: deny", "not a known component"},
		{"group collides", "version: 1\ncomponents: {a: [\"x/**\"]}\ngroups: {a: [a]}\npolicy: deny", "collides"},
		{"group reserved", "version: 1\ncomponents: {a: [\"x/**\"]}\ngroups: {std: [a]}\npolicy: deny", "reserved"},
		{"bad test_files", "version: 1\ncomponents: {a: [\"x/**\"]}\npolicy: deny\noptions: {test_files: never}", "test_files"},
		{"typo field", "version: 1\ncomponents: {a: [\"x/**\"]}\npolicy: deny\nrulez: {}", "rulez"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}
