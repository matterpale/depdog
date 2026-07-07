package wizard

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"domain", "http-api", "pkg_util", "v2", "a.b"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", ok, err)
		}
	}
	bad := []struct {
		name, wantSub string
	}{
		{"", "empty"},
		{"std", "reserved"},
		{"external", "reserved"},
		{"unassigned", "reserved"},
		{"*", "reserved"},
		{"has space", "letters"},
		{"a/b", "letters"},
		{"a:b", "letters"},
	}
	for _, tt := range bad {
		err := ValidateName(tt.name)
		if err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", tt.name)
			continue
		}
		if !strings.Contains(err.Error(), tt.wantSub) {
			t.Errorf("ValidateName(%q) = %q, want mention of %q", tt.name, err, tt.wantSub)
		}
	}
}

func TestRenameRewritesRefs(t *testing.T) {
	scan := Scan{Dirs: []string{"cmd/app", "internal/domain/order", "internal/handler"}}
	cfg := Suggest(mustPreset(t, "ddd"), scan, PolicyDeny)

	got, err := cfg.Rename("domain", "model")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, ok := find(got, "domain"); ok {
		t.Error("old name still present after rename")
	}
	model, ok := find(got, "model")
	if !ok {
		t.Fatal("renamed component missing")
	}
	// The component keeps everything but its name.
	orig, _ := find(cfg, "domain")
	if !reflect.DeepEqual(model.Patterns, orig.Patterns) || model.Comment != orig.Comment {
		t.Errorf("rename changed more than the name: %+v vs %+v", model, orig)
	}
	// Every rule ref to the old name is rewritten.
	h, _ := find(got, "handler")
	if want := []string{"model", "std", "external"}; !reflect.DeepEqual(h.Allow, want) {
		t.Errorf("handler.Allow = %v, want %v", h.Allow, want)
	}
	// The original config is untouched (value semantics).
	if _, ok := find(cfg, "domain"); !ok {
		t.Error("Rename mutated its receiver")
	}
}

func TestRenameRejects(t *testing.T) {
	scan := Scan{Dirs: []string{"internal/domain/order", "internal/handler"}}
	cfg := Suggest(mustPreset(t, "ddd"), scan, PolicyDeny)

	if _, err := cfg.Rename("domain", "handler"); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("collision: err = %v, want 'already exists'", err)
	}
	if _, err := cfg.Rename("domain", "std"); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("reserved: err = %v, want 'reserved'", err)
	}
	if _, err := cfg.Rename("nope", "fine"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown component: err = %v, want it named", err)
	}
	// Renaming to itself is a no-op, not a collision.
	same, err := cfg.Rename("domain", "domain")
	if err != nil {
		t.Errorf("self-rename: %v", err)
	}
	if !reflect.DeepEqual(names(same), names(cfg)) {
		t.Errorf("self-rename changed components: %v", names(same))
	}
}

func TestSetPatterns(t *testing.T) {
	scan := Scan{Dirs: []string{"internal/domain/order", "internal/handler"}}
	cfg := Suggest(mustPreset(t, "ddd"), scan, PolicyDeny)

	got, err := cfg.SetPatterns("domain", []string{"internal/domain/**", "internal/model/**"})
	if err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	d, _ := find(got, "domain")
	if want := []string{"internal/domain/**", "internal/model/**"}; !reflect.DeepEqual(d.Patterns, want) {
		t.Errorf("Patterns = %v, want %v", d.Patterns, want)
	}
	// The original config is untouched.
	if d, _ := find(cfg, "domain"); !reflect.DeepEqual(d.Patterns, []string{"internal/domain/**"}) {
		t.Error("SetPatterns mutated its receiver")
	}

	if _, err := cfg.SetPatterns("domain", nil); err == nil {
		t.Error("empty patterns: want error")
	}
	if _, err := cfg.SetPatterns("domain", []string{"/abs/**"}); err == nil {
		t.Error("invalid pattern: want error")
	}
	if _, err := cfg.SetPatterns("nope", []string{"x/**"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown component: err = %v, want it named", err)
	}
}

func TestParsePatterns(t *testing.T) {
	got, err := ParsePatterns(" internal/api/** , pkg/util ,")
	if err != nil {
		t.Fatalf("ParsePatterns: %v", err)
	}
	if want := []string{"internal/api/**", "pkg/util"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParsePatterns = %v, want %v", got, want)
	}
	if _, err := ParsePatterns("   "); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Errorf("blank input: err = %v, want 'at least one pattern'", err)
	}
	if _, err := ParsePatterns("/abs/**"); err == nil {
		t.Error("absolute pattern: want error")
	}
	if _, err := ParsePatterns("a[**"); err == nil {
		t.Error("malformed glob: want error")
	}
}

func TestComponentLookup(t *testing.T) {
	cfg := Suggest(mustPreset(t, "flat"), Scan{Dirs: []string{"internal/foo"}}, PolicyDeny)
	if c, ok := cfg.Component("foo"); !ok || c.Name != "foo" {
		t.Errorf("Component(foo) = %+v, %v", c, ok)
	}
	if _, ok := cfg.Component("bar"); ok {
		t.Error("Component(bar) = ok, want miss")
	}
}
