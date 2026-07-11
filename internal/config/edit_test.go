package config

import (
	"strings"
	"testing"
)

const editFixture = `# depdog architecture
version: 2

components:
  domain:     { path: "internal/domain/**", allow: [ std ] }   # only std
  handler:    { path: "internal/handler/**", deny: [ service, repository ] }
  service:    { path: "internal/service/**" }
  repository: { path: "internal/repository/**" }
  main:       { path: "cmd/**" }

default: allow

options:
  test_files: hybrid
`

// setRule is a test helper: edit and fail on error.
func setRule(t *testing.T, data []byte, comp, target, verdict string) []byte {
	t.Helper()
	out, err := SetComponentRule(data, comp, target, verdict)
	if err != nil {
		t.Fatalf("SetComponentRule(%s, %s, %s): %v", comp, target, verdict, err)
	}
	return out
}

func TestSetComponentRuleSemantics(t *testing.T) {
	// Add external to domain's allow list (a whitelist).
	out := setRule(t, []byte(editFixture), "domain", "external", "allow")
	rs, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ok, _ := rs.Decide("domain", "external"); !ok {
		t.Errorf("domain should now allow external")
	}
	if ok, _ := rs.Decide("domain", "std"); !ok {
		t.Errorf("domain should still allow std")
	}

	// Toggle a denied peer to allowed: handler deny[service] -> allow service
	// moves it out of deny and into allow.
	out = setRule(t, []byte(editFixture), "handler", "service", "allow")
	rs, _ = Parse(out)
	if ok, _ := rs.Decide("handler", "service"); !ok {
		t.Errorf("handler should now allow service")
	}
	if ok, _ := rs.Decide("handler", "repository"); ok {
		t.Errorf("handler should still deny repository")
	}

	// default removes a ref; emptying domain's allow drops the whole key so the
	// component becomes rule-less (falls back to default: allow).
	out = setRule(t, []byte(editFixture), "domain", "std", "default")
	if strings.Contains(lineFor(t, out, "domain:"), "allow") {
		t.Errorf("domain's now-empty allow key should be gone:\n%s", lineFor(t, out, "domain:"))
	}
	rs, _ = Parse(out)
	// Rule-less now, so it follows default: allow — domain may import anything.
	if ok, _ := rs.Decide("domain", "handler"); !ok {
		t.Errorf("rule-less domain should fall back to default: allow")
	}
}

func TestSetComponentRulePreservesEverythingElse(t *testing.T) {
	out := string(setRule(t, []byte(editFixture), "domain", "external", "allow"))

	// Every line except domain's is byte-identical, and domain keeps its comment.
	orig := strings.Split(editFixture, "\n")
	got := strings.Split(out, "\n")
	if len(orig) != len(got) {
		t.Fatalf("line count changed: %d -> %d\n%s", len(orig), len(got), out)
	}
	for i := range orig {
		if strings.HasPrefix(strings.TrimSpace(orig[i]), "domain:") {
			if !strings.Contains(got[i], "# only std") {
				t.Errorf("domain line lost its comment:\n%q", got[i])
			}
			if !strings.HasPrefix(got[i], "  domain:") {
				t.Errorf("domain line lost its indentation:\n%q", got[i])
			}
			continue
		}
		if orig[i] != got[i] {
			t.Errorf("line %d changed unexpectedly:\n- %q\n+ %q", i+1, orig[i], got[i])
		}
	}
}

func TestSetComponentRuleNoOp(t *testing.T) {
	// Removing a ref that isn't there changes nothing.
	out, err := SetComponentRule([]byte(editFixture), "handler", "domain", "default")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != editFixture {
		t.Errorf("no-op edit should return the input unchanged")
	}
}

func TestSetComponentPath(t *testing.T) {
	// Single pattern -> scalar, preserving the trailing comment and other lines.
	out := string(mustPath(t, []byte(editFixture), "domain", []string{"internal/model/**"}))
	dl := lineFor(t, []byte(out), "domain:")
	if !strings.Contains(dl, `internal/model/**`) || strings.Contains(dl, "internal/domain/**") {
		t.Errorf("domain path not rewritten:\n%s", dl)
	}
	if !strings.Contains(dl, "# only std") {
		t.Errorf("domain kept its rule and comment? line:\n%s", dl)
	}
	rs, err := Parse([]byte(out))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ok, _ := rs.Decide("domain", "std"); !ok {
		t.Errorf("domain should still allow std after a re-path")
	}

	// Several patterns -> a flow sequence.
	out = string(mustPath(t, []byte(editFixture), "main", []string{"cmd/**", "tools/**"}))
	ml := lineFor(t, []byte(out), "  main:")
	if !strings.Contains(ml, "cmd/**") || !strings.Contains(ml, "tools/**") {
		t.Errorf("main should carry both patterns:\n%s", ml)
	}
	if _, err := Parse([]byte(out)); err != nil {
		t.Fatalf("multi-pattern re-path did not parse: %v", err)
	}

	// No-op: setting the same path returns the input unchanged.
	same, err := SetComponentPath([]byte(editFixture), "main", []string{"cmd/**"})
	if err != nil {
		t.Fatal(err)
	}
	if string(same) != editFixture {
		t.Error("re-pathing to the current path should be a no-op")
	}

	// Empty patterns are rejected.
	if _, err := SetComponentPath([]byte(editFixture), "main", nil); err == nil {
		t.Error("expected an error for zero patterns")
	}
}

func mustPath(t *testing.T, data []byte, comp string, patterns []string) []byte {
	t.Helper()
	out, err := SetComponentPath(data, comp, patterns)
	if err != nil {
		t.Fatalf("SetComponentPath(%s, %v): %v", comp, patterns, err)
	}
	return out
}

func TestSetComponentRuleRefusals(t *testing.T) {
	if _, err := SetComponentRule([]byte(editFixture), "nope", "std", "allow"); err == nil {
		t.Error("expected an error for an unknown component")
	}
	flow := "components: { a: { path: \"a/**\" } }\ndefault: allow\n"
	if _, err := SetComponentRule([]byte(flow), "a", "std", "allow"); err == nil {
		t.Error("expected a refusal for a flow-style components mapping")
	}
}

// lineFor returns the first line containing needle.
func lineFor(t *testing.T, data []byte, needle string) string {
	t.Helper()
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.Contains(ln, needle) {
			return ln
		}
	}
	t.Fatalf("no line contains %q", needle)
	return ""
}
