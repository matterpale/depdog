package config

import (
	"strings"
	"testing"
)

// yaml.v3 reports Node.Column in characters, not bytes. These configs put a
// multibyte rune (ö / å) before the spliced position on a line, so a byte-index
// bug would mislocate the splice and wrongly refuse (or corrupt) a valid config.

func TestSetRuleMultibyteName(t *testing.T) {
	const cfg = `version: 2
default: allow
components:
  wörk: { path: "internal/w/**" }
  api:  { path: "internal/api/**" }
`
	out, err := SetComponentRule([]byte(cfg), "wörk", "api", "deny")
	if err != nil {
		t.Fatalf("SetComponentRule on a multibyte-named component: %v", err)
	}
	if !strings.Contains(string(out), `deny: [api]`) {
		t.Errorf("expected deny: [api] spliced in, got:\n%s", out)
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("Parse after edit: %v", err)
	}
}

func TestAddBoundaryMemberMultibyteName(t *testing.T) {
	const cfg = `version: 2
default: allow
components:
  api: { path: "internal/api/**" }
  db:  { path: "internal/db/**" }
boundaries:
  wåll: [api, db]
`
	out, err := AddBoundaryMember([]byte(cfg), "wåll", "cmd/x/**")
	if err != nil {
		t.Fatalf("AddBoundaryMember on a multibyte-named boundary: %v", err)
	}
	if !strings.Contains(string(out), `"cmd/x/**"`) {
		t.Errorf("expected the new member spliced in, got:\n%s", out)
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("Parse after add: %v", err)
	}
}

func TestRenameRefAfterMultibyte(t *testing.T) {
	const cfg = `version: 2
default: allow
components:
  wörk: { path: "internal/w/**", allow: [api] }
  api:  { path: "internal/api/**" }
`
	out, err := RenameComponent([]byte(cfg), "api", "svc")
	if err != nil {
		t.Fatalf("RenameComponent with a ref after a multibyte rune: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `allow: [svc]`) {
		t.Errorf("the allow ref after the multibyte rune was not renamed:\n%s", s)
	}
	if !strings.Contains(s, "svc:  { path:") {
		t.Errorf("the component key was not renamed:\n%s", s)
	}
	if strings.Contains(s, "[api]") {
		t.Errorf("an allow/deny ref to the old name survived:\n%s", s)
	}
	// The path glob is deliberately left alone even though it contains "api".
	if !strings.Contains(s, `"internal/api/**"`) {
		t.Errorf("a path glob equal to the old name should not be touched:\n%s", s)
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("Parse after rename: %v", err)
	}
}
