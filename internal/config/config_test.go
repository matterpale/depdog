package config

import (
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

const valid = `
version: 2
components:
  main:   { path: "cmd/**", allow: ["*"] }
  domain: { path: "internal/domain/**", allow: [std] }
default: deny
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
		t.Errorf("default/test_files not compiled: %+v", rs)
	}
	if len(rs.Rules["main"].Allow) != 1 || rs.Rules["main"].Allow[0].Kind != core.RefAny {
		t.Errorf("main rule = %+v", rs.Rules["main"])
	}
}

func TestParseDefaultStance(t *testing.T) {
	// `default` is optional; absent it defaults to the open (allow) stance, so
	// a rule-less component may import anything.
	rs, err := Parse([]byte("version: 2\ncomponents: {a: {path: \"x/**\"}}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if rs.Policy != core.PolicyAllow {
		t.Errorf("absent default = %v, want allow (open)", rs.Policy)
	}
	if ok, _ := rs.Decide("a", "external"); !ok {
		t.Errorf("a rule-less component must import anything under the open default")
	}
}

// TestParsePolicyRenamed checks the actionable error when a config still uses
// the old top-level `policy` key (renamed to `default` in v2).
func TestParsePolicyRenamed(t *testing.T) {
	_, err := Parse([]byte("version: 2\ncomponents: {a: {path: \"x/**\"}}\npolicy: deny\n"))
	if err == nil {
		t.Fatal("a config using the old `policy` key must be rejected")
	}
	for _, want := range []string{"`policy`", "`default`", "default: deny"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("rename error missing %q:\n%s", want, err)
		}
	}
}

func TestParseGroups(t *testing.T) {
	rs, err := Parse([]byte(`
version: 2
components:
  ui:     { path: "internal/ui/**", allow: [inner, std] }
  app:    { path: "internal/app/**" }
  domain: { path: "internal/domain/**" }
groups:
  inner: [app, domain]
default: deny
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
	rs, err := Parse([]byte("version: 2\ncomponents: {a: {path: \"x/**\", allow: [std, \"golang.org/x/sync\"]}}\ndefault: deny\n"))
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
	// path accepts a bare scalar as well as a list.
	rs, err := Parse([]byte("version: 2\ncomponents:\n  main: { path: cmd/** }\ndefault: deny\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := rs.Components[0].Patterns; len(got) != 1 || got[0] != "cmd/**" {
		t.Errorf("patterns = %v", got)
	}
}

func TestParseMultiPattern(t *testing.T) {
	// A component may claim several path globs.
	rs, err := Parse([]byte("version: 2\ncomponents:\n  api: { path: [\"internal/api/**\", \"internal/rpc/**\"], allow: [std] }\ndefault: deny\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := rs.Components[0].Patterns; len(got) != 2 || got[0] != "internal/api/**" || got[1] != "internal/rpc/**" {
		t.Errorf("patterns = %v, want both api and rpc globs", got)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, yaml, wantErr string
	}{
		{"empty", "", "empty"},
		{"bad version", "version: 3\ncomponents: {a: {path: \"x/**\"}}", "version 2"},
		{"no components", "version: 2\ndefault: deny", `no "components"`},
		{"reserved name", "version: 2\ncomponents: {std: {path: \"x/**\"}}", "reserved"},
		{"empty patterns", "version: 2\ncomponents: {a: {path: []}}", "no patterns"},
		{"bad glob", "version: 2\ncomponents: {a: {path: [\"x/[bad/**\"]}}", "segment"},
		{"bad default", "version: 2\ncomponents: {a: {path: \"x/**\"}}\ndefault: strict", "default must be"},
		{"unknown ref", "version: 2\ncomponents: {a: {path: \"x/**\", allow: [nope]}}", `unknown component or group "nope"`},
		{"group unknown member", "version: 2\ncomponents: {a: {path: \"x/**\"}}\ngroups: {g: [nope]}", "not a known component"},
		{"group collides", "version: 2\ncomponents: {a: {path: \"x/**\"}}\ngroups: {a: [a]}", "collides"},
		{"group reserved", "version: 2\ncomponents: {a: {path: \"x/**\"}}\ngroups: {std: [a]}", "reserved"},
		{"bad test_files", "version: 2\ncomponents: {a: {path: \"x/**\"}}\noptions: {test_files: never}", "test_files"},
		{"typo field", "version: 2\ncomponents: {a: {path: \"x/**\"}}\nrulez: {}", "rulez"},
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

// TestParseLegacyMigrationError checks that a version-1 config (separate
// components: and rules: blocks) is rejected with an actionable rewrite built
// from the user's own first component, not a generic decode failure.
func TestParseLegacyMigrationError(t *testing.T) {
	old := `version: 1
components:
  domain:  ["internal/domain/**"]
  handler: ["internal/handler/**"]
policy: deny
rules:
  domain: { allow: [std] }
`
	_, err := Parse([]byte(old))
	if err == nil {
		t.Fatal("a version 1 config must be rejected")
	}
	msg := err.Error()
	for _, want := range []string{"version 1", "version: 2", `domain: { path: "internal/domain/**", allow: [std] }`} {
		if !strings.Contains(msg, want) {
			t.Errorf("migration error missing %q:\n%s", want, msg)
		}
	}
}
