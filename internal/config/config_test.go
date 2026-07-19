package config

import (
	"os"
	"path/filepath"
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

// TestParseLang checks the optional `lang:` key: it is carried verbatim onto
// the RuleSet (config attaches no meaning — the CLI validates it), and is empty
// when absent. Config does not reject an unknown value; that is the CLI's job.
func TestParseLang(t *testing.T) {
	rs, err := Parse([]byte("version: 2\nlang: go\ncomponents: {a: {path: \"x/**\"}}\ndefault: deny\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if rs.Lang != "go" {
		t.Errorf("Lang = %q, want go", rs.Lang)
	}

	rs, err = Parse([]byte(valid))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if rs.Lang != "" {
		t.Errorf("absent lang = %q, want empty", rs.Lang)
	}

	// An unknown value parses fine — config only carries the string.
	rs, err = Parse([]byte("version: 2\nlang: klingon\ncomponents: {a: {path: \"x/**\"}}\ndefault: deny\n"))
	if err != nil {
		t.Fatalf("unknown lang: must parse (CLI validates), got %v", err)
	}
	if rs.Lang != "klingon" {
		t.Errorf("Lang = %q, want klingon (carried verbatim)", rs.Lang)
	}
}

// TestPeekLang checks the lenient lang-only peek used by the CLI to resolve a
// unit's adapter before the full Load.
func TestPeekLang(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, DefaultName)
	if err := os.WriteFile(p, []byte("version: 2\nlang: ts\ncomponents: {a: {path: \"x/**\"}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := PeekLang(p); got != "ts" {
		t.Errorf("PeekLang = %q, want ts", got)
	}
	// Missing file, non-scalar lang, and unrelated garbage all peek to "".
	if got := PeekLang(filepath.Join(dir, "nope.yaml")); got != "" {
		t.Errorf("PeekLang(missing) = %q, want empty", got)
	}
	nonScalar := filepath.Join(dir, "nonscalar.yaml")
	if err := os.WriteFile(nonScalar, []byte("lang: [go, ts]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := PeekLang(nonScalar); got != "" {
		t.Errorf("PeekLang(non-scalar) = %q, want empty", got)
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

func TestParseBoundariesShorthand(t *testing.T) {
	rs, err := Parse([]byte(`
version: 2
components:
  service-a:   { path: "cmd/service-a/**" }
  service-b: { path: "cmd/service-b/**" }
default: allow
boundaries:
  services: [service-a, service-b]
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rs.Boundaries) != 1 {
		t.Fatalf("boundaries = %d, want 1: %+v", len(rs.Boundaries), rs.Boundaries)
	}
	b := rs.Boundaries[0]
	if b.Name != "services" || b.Sealed {
		t.Errorf("boundary = %+v, want services, sealed=false", b)
	}
	if len(b.Members) != 2 || b.Members[0].Label != "service-a" || b.Members[1].Label != "service-b" {
		t.Errorf("members = %+v, want sorted [service-a service-b]", b.Members)
	}
	// A component member carries its component's patterns.
	if b.Members[1].Component != "service-b" || len(b.Members[1].Patterns) != 1 || b.Members[1].Patterns[0] != "cmd/service-b/**" {
		t.Errorf("service-b member = %+v, want component with its pattern", b.Members[1])
	}
}

func TestParseBoundariesExpanded(t *testing.T) {
	rs, err := Parse([]byte(`
version: 2
components:
  service-a:   { path: "cmd/service-a/**" }
  service-b: { path: "cmd/service-b/**" }
default: allow
boundaries:
  services:
    members: [service-a, service-b]
    sealed: true
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rs.Boundaries) != 1 || !rs.Boundaries[0].Sealed {
		t.Fatalf("expanded form should set sealed=true: %+v", rs.Boundaries)
	}
}

func TestParseBoundariesGlobMembers(t *testing.T) {
	rs, err := Parse([]byte(`
version: 2
components:
  app: { path: "**" }
default: allow
boundaries:
  cmd-services:
    members: ["cmd/service-a/**", "cmd/service-b/**"]
    sealed: true
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	b := rs.Boundaries[0]
	if len(b.Members) != 2 {
		t.Fatalf("members = %+v, want 2 globs", b.Members)
	}
	for _, m := range b.Members {
		if m.Component != "" {
			t.Errorf("glob member should have no component: %+v", m)
		}
		if len(m.Patterns) != 1 {
			t.Errorf("glob member should carry its pattern: %+v", m)
		}
	}
}

func TestParseBoundariesMixedMembers(t *testing.T) {
	rs, err := Parse([]byte(`
version: 2
components:
  service-a: { path: "cmd/service-a/**" }
default: allow
boundaries:
  mix: [service-a, "cmd/service-b/**"]
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	b := rs.Boundaries[0]
	if len(b.Members) != 2 {
		t.Fatalf("members = %+v, want a component and a glob", b.Members)
	}
	// Sorted by label: "cmd/service-b/**" < "service-a".
	if b.Members[0].Component != "" || b.Members[0].Label != "cmd/service-b/**" {
		t.Errorf("first member should be the glob: %+v", b.Members[0])
	}
	if b.Members[1].Component != "service-a" {
		t.Errorf("second member should be the component: %+v", b.Members[1])
	}
}

func TestParseBoundariesSingleSegmentGlob(t *testing.T) {
	// A bare-glob member like "cmd*" has no "/" but does have a metachar, so it
	// must be read as a glob, not mis-read as an unknown component.
	rs, err := Parse([]byte(`
version: 2
components:
  app: { path: "**" }
default: allow
boundaries:
  b: ["cmd*", "svc*"]
`))
	if err != nil {
		t.Fatalf("a single-segment glob member should parse: %v", err)
	}
	for _, m := range rs.Boundaries[0].Members {
		if m.Component != "" {
			t.Errorf("single-segment glob mis-read as component: %+v", m)
		}
	}
}

func TestParseBoundariesErrors(t *testing.T) {
	tests := []struct {
		name, yaml, wantErr string
	}{
		{
			"unknown member",
			"version: 2\ncomponents: {a: {path: \"x/**\"}}\ndefault: allow\nboundaries: {b: [nope]}",
			"is not a known component or a path glob",
		},
		{
			"empty boundary",
			"version: 2\ncomponents: {a: {path: \"x/**\"}}\ndefault: allow\nboundaries: {b: []}",
			"has no members",
		},
		{
			"duplicate component member",
			"version: 2\ncomponents: {a: {path: \"x/**\"}}\ndefault: allow\nboundaries: {b: [a, a]}",
			"twice",
		},
		{
			"identical glob members overlap",
			"version: 2\ncomponents: {a: {path: \"x/**\"}}\ndefault: allow\nboundaries: {b: [\"y/**\", \"y/**\"]}",
			"overlap",
		},
		{
			"bad glob member",
			"version: 2\ncomponents: {a: {path: \"x/**\"}}\ndefault: allow\nboundaries: {b: [\"y/[bad/**\", \"z/**\"]}",
			"segment",
		},
		{
			"unknown expanded sub-key",
			"version: 2\ncomponents: {a: {path: \"x/**\"}}\ndefault: allow\nboundaries: {b: {members: [\"y/**\", \"z/**\"], seald: true}}",
			"seald",
		},
		{
			"reserved boundary name",
			"version: 2\ncomponents: {a: {path: \"x/**\"}}\ndefault: allow\nboundaries: {std: [\"y/**\", \"z/**\"]}",
			"reserved",
		},
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

func TestParseBoundariesBadForm(t *testing.T) {
	// A scalar (neither a list nor a mapping) is neither boundary form.
	_, err := Parse([]byte("version: 2\ncomponents: {a: {path: \"x/**\"}}\ndefault: allow\nboundaries: {b: oops}"))
	if err == nil {
		t.Fatal("a scalar boundary value must be rejected")
	}
	if !strings.Contains(err.Error(), "members") {
		t.Errorf("error should describe the accepted forms: %v", err)
	}
}

func TestParseNoBoundaries(t *testing.T) {
	// Absent the key, boundaries is nil and everything else is unchanged.
	rs, err := Parse([]byte(valid))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if rs.Boundaries != nil {
		t.Errorf("absent boundaries should compile to nil: %+v", rs.Boundaries)
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

func TestParseSeverity(t *testing.T) {
	rs, err := Parse([]byte(`version: 2
components:
  app: { path: "app/**", allow: [std], severity: warn }
  lib: { path: "lib/**" }
boundaries:
  walls: { members: [app, lib], severity: warn }
default: allow
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	sevOf := func(name string) core.Severity {
		for _, c := range rs.Components {
			if c.Name == name {
				return c.Severity
			}
		}
		t.Fatalf("no component %q", name)
		return 0
	}
	if sevOf("app") != core.SeverityWarn {
		t.Errorf("app severity = %v, want warn", sevOf("app"))
	}
	if sevOf("lib") != core.SeverityError {
		t.Errorf("lib severity = %v, want error (the absent default)", sevOf("lib"))
	}
	if len(rs.Boundaries) != 1 || rs.Boundaries[0].Severity != core.SeverityWarn {
		t.Errorf("boundary severity = %+v, want warn", rs.Boundaries)
	}

	// An unknown severity is an actionable config error.
	_, err = Parse([]byte("version: 2\ncomponents: {a: {path: \"a/**\", severity: loud}}\ndefault: allow\n"))
	if err == nil {
		t.Fatal("expected an error for severity: loud")
	}
	if !strings.Contains(err.Error(), "severity") {
		t.Errorf("error should mention severity: %v", err)
	}
}
