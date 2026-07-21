package config

import (
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

const renameFixture = `# depdog architecture
version: 2

components:
  domain:  { path: "internal/domain/**", allow: [ std ] }
  handler: { path: "internal/handler/**", deny: [ service, repository ] }   # peers
  service: { path: "internal/service/**", allow: [ domain ] }
  repository: { path: "internal/repository/**" }

aliases:
  inner: [ domain, service ]

boundaries:
  layers: [ handler, service, repository ]
  cmd:
    members: [ service, repository ]
    sealed: true

default: allow
`

func TestRenameComponent(t *testing.T) {
	out, err := RenameComponent([]byte(renameFixture), "service", "svc")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	// The key, the deny ref on handler, the alias entry, and both boundary member
	// lists all move to "svc".
	rs, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !hasComponent(rs, "svc") {
		t.Errorf("renamed component svc not found")
	}
	if hasComponent(rs, "service") {
		t.Errorf("old name service should be gone")
	}
	if ok, _ := rs.Decide("handler", "svc"); ok {
		t.Errorf("handler should deny svc (the renamed peer)")
	}
	if ok, _ := rs.Decide("handler", "repository"); ok {
		t.Errorf("handler should still deny repository")
	}

	// The path glob is deliberately untouched (it is not a name reference).
	if !strings.Contains(lineFor(t, out, "svc:"), `internal/service/**`) {
		t.Errorf("svc should keep its original path glob:\n%s", lineFor(t, out, "svc:"))
	}
	// alias + boundaries carry the new name.
	if !strings.Contains(lineFor(t, out, "inner:"), "svc") {
		t.Errorf("alias inner should reference svc:\n%s", lineFor(t, out, "inner:"))
	}
	if !strings.Contains(lineFor(t, out, "layers:"), "svc") || strings.Contains(lineFor(t, out, "layers:"), " service") {
		t.Errorf("boundary layers should reference svc, not service:\n%s", lineFor(t, out, "layers:"))
	}
	// The header comment and other lines survive.
	if !strings.HasPrefix(s, "# depdog architecture\n") {
		t.Errorf("header comment lost")
	}
	if !strings.Contains(s, "sealed: true") {
		t.Errorf("expanded boundary body lost")
	}
}

func TestRenameComponentRefusals(t *testing.T) {
	// Collision with an existing component.
	if _, err := RenameComponent([]byte(renameFixture), "service", "domain"); err == nil {
		t.Error("renaming onto an existing component should be refused")
	}
	// Collision with an alias name.
	if _, err := RenameComponent([]byte(renameFixture), "service", "inner"); err == nil {
		t.Error("renaming onto an existing alias name should be refused")
	}
	// Unknown component.
	if _, err := RenameComponent([]byte(renameFixture), "nope", "x"); err == nil {
		t.Error("renaming an unknown component should be refused")
	}
	// No-op.
	out, err := RenameComponent([]byte(renameFixture), "service", "service")
	if err != nil || string(out) != renameFixture {
		t.Error("renaming to the same name should be a no-op")
	}
}

// TestRenameComponentGroupsSynonym confirms a rename carries references through
// the deprecated `groups:` block, not just the current `aliases:` one, so a
// not-yet-migrated 1.x config renames cleanly.
func TestRenameComponentGroupsSynonym(t *testing.T) {
	in := "version: 2\ncomponents:\n  domain: { path: \"internal/domain/**\" }\n  service: { path: \"internal/service/**\", allow: [domain] }\ngroups:\n  inner: [domain, service]\ndefault: deny\n"
	out, err := RenameComponent([]byte(in), "service", "svc")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lineFor(t, out, "inner:"), "svc") || strings.Contains(lineFor(t, out, "inner:"), " service") {
		t.Errorf("the deprecated groups block should carry the new name:\n%s", lineFor(t, out, "inner:"))
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("renamed config does not parse: %v\n%s", err, out)
	}
}

func hasComponent(rs *core.RuleSet, name string) bool {
	for _, c := range rs.Components {
		if c.Name == name {
			return true
		}
	}
	return false
}
