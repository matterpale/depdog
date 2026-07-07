package wizard

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPresetByName(t *testing.T) {
	for _, name := range PresetNames() {
		if _, err := PresetByName(name); err != nil {
			t.Errorf("PresetByName(%q): %v", name, err)
		}
	}
	if want := []string{"ddd", "hexagonal", "layered", "flat"}; !reflect.DeepEqual(PresetNames(), want) {
		t.Errorf("PresetNames() = %v, want %v", PresetNames(), want)
	}
	_, err := PresetByName("nope")
	if err == nil {
		t.Fatal("PresetByName(nope): want error")
	}
	if !strings.Contains(err.Error(), "ddd") {
		t.Errorf("error should list valid presets: %v", err)
	}
}

func TestScanModule(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go")                             // module root: never a component
	write("cmd/app/main.go")                     // -> cmd/app
	write("internal/domain/order/order.go")      // -> internal/domain/order
	write("internal/domain/order/order_test.go") // test file, dir already counted
	write("internal/testonly/foo_test.go")       // only a test file -> excluded
	write("internal/.hidden/x.go")               // hidden dir -> skipped
	write("_scratch/z.go")                       // underscore dir -> skipped
	write("testdata/fix/y.go")                   // testdata tree -> skipped
	write("vendor/dep/pkg.go")                   // vendor tree -> skipped

	s, err := ScanModule(root)
	if err != nil {
		t.Fatalf("ScanModule: %v", err)
	}
	want := []string{"cmd/app", "internal/domain/order"}
	if !reflect.DeepEqual(s.Dirs, want) {
		t.Errorf("dirs = %v, want %v", s.Dirs, want)
	}
}

func names(cfg Config) []string {
	out := make([]string, len(cfg.Components))
	for i, c := range cfg.Components {
		out[i] = c.Name
	}
	return out
}

func find(cfg Config, name string) (Component, bool) {
	for _, c := range cfg.Components {
		if c.Name == name {
			return c, true
		}
	}
	return Component{}, false
}

func mustPreset(t *testing.T, name string) Preset {
	t.Helper()
	p, err := PresetByName(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSuggestConforming(t *testing.T) {
	scan := Scan{Dirs: []string{
		"cmd/app", "internal/domain/order", "internal/handler",
		"internal/service", "internal/repository", "internal/telemetry", "pkg/util",
	}}
	cfg := Suggest(mustPreset(t, "ddd"), scan, PolicyDeny)

	want := []string{"main", "domain", "handler", "service", "repository", "telemetry", "util"}
	if got := names(cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("components = %v, want %v", got, want)
	}
	if c, _ := find(cfg, "main"); !reflect.DeepEqual(c.Allow, []string{"*"}) {
		t.Errorf("main.Allow = %v", c.Allow)
	}
	tel, _ := find(cfg, "telemetry")
	if !reflect.DeepEqual(tel.Patterns, []string{"internal/telemetry/**"}) || !reflect.DeepEqual(tel.Allow, []string{"std", "external"}) {
		t.Errorf("telemetry proposed wrong: %+v", tel)
	}
	if c, _ := find(cfg, "util"); !reflect.DeepEqual(c.Patterns, []string{"pkg/util/**"}) {
		t.Errorf("util.Patterns = %v", c.Patterns)
	}
}

func TestSuggestEmptyModuleKeepsScaffold(t *testing.T) {
	cfg := Suggest(mustPreset(t, "ddd"), Scan{}, PolicyDeny)
	want := []string{"main", "domain", "handler", "service", "repository"}
	if got := names(cfg); !reflect.DeepEqual(got, want) {
		t.Errorf("components = %v, want full preset %v", got, want)
	}
}

func TestSuggestDropsUnmatchedAndScrubsRefs(t *testing.T) {
	// Only ui and domain exist; app, infra and main are dropped, so refs to
	// them must be scrubbed from the surviving rules.
	scan := Scan{Dirs: []string{"internal/ui/widgets", "internal/domain/order"}}

	deny := Suggest(mustPreset(t, "layered"), scan, PolicyDeny)
	if got := names(deny); !reflect.DeepEqual(got, []string{"ui", "domain"}) {
		t.Fatalf("components = %v, want [ui domain]", got)
	}
	ui, _ := find(deny, "ui")
	if want := []string{"domain", "std", "external"}; !reflect.DeepEqual(ui.Allow, want) {
		t.Errorf("ui.Allow = %v, want %v (app scrubbed)", ui.Allow, want)
	}

	allow := Suggest(mustPreset(t, "layered"), scan, PolicyAllow)
	dom, _ := find(allow, "domain")
	if want := []string{"ui", "external", "unassigned"}; !reflect.DeepEqual(dom.Deny, want) {
		t.Errorf("domain.Deny = %v, want %v (app, infra scrubbed)", dom.Deny, want)
	}
	if u, _ := find(allow, "ui"); len(u.Deny) != 0 {
		t.Errorf("ui.Deny = %v, want empty (infra scrubbed)", u.Deny)
	}
}

func TestSuggestFlat(t *testing.T) {
	scan := Scan{Dirs: []string{"cmd/app", "internal/foo", "pkg/bar/baz"}}
	cfg := Suggest(mustPreset(t, "flat"), scan, PolicyDeny)
	if got := names(cfg); !reflect.DeepEqual(got, []string{"bar", "cmd", "foo"}) {
		t.Fatalf("components = %v, want [bar cmd foo]", got)
	}
	if c, _ := find(cfg, "cmd"); !reflect.DeepEqual(c.Allow, []string{"*"}) {
		t.Errorf("cmd.Allow = %v, want [*] (entrypoint)", c.Allow)
	}
	if c, _ := find(cfg, "foo"); !reflect.DeepEqual(c.Allow, []string{"std", "external"}) {
		t.Errorf("foo.Allow = %v", c.Allow)
	}
}

func TestSuggestFlatEmptyModuleStarter(t *testing.T) {
	cfg := Suggest(mustPreset(t, "flat"), Scan{}, PolicyDeny)
	if got := names(cfg); !reflect.DeepEqual(got, []string{"app"}) {
		t.Fatalf("components = %v, want [app] starter", got)
	}
}

func TestKeepScrubsDroppedRefs(t *testing.T) {
	scan := Scan{Dirs: []string{"internal/domain/x", "internal/handler", "internal/service"}}
	cfg := Suggest(mustPreset(t, "ddd"), scan, PolicyDeny)
	// Drop domain; handler allowed [domain, std, external] must lose domain.
	kept := cfg.Keep([]string{"handler", "service"})
	if got := names(kept); !reflect.DeepEqual(got, []string{"handler", "service"}) {
		t.Fatalf("kept = %v", got)
	}
	h, _ := find(kept, "handler")
	if want := []string{"std", "external"}; !reflect.DeepEqual(h.Allow, want) {
		t.Errorf("handler.Allow = %v, want %v (domain scrubbed)", h.Allow, want)
	}
}

func TestMarshalStructure(t *testing.T) {
	cfg := Suggest(mustPreset(t, "ddd"), Scan{Dirs: []string{"cmd/app", "internal/domain/x"}}, PolicyDeny)
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"version: 1", "policy: deny", "components:", "rules:", `main:`, `{ allow: ["*"] }`} {
		if !strings.Contains(got, want) {
			t.Errorf("marshal output missing %q\n%s", want, got)
		}
	}
	// Determinism: marshalling twice yields identical bytes.
	again, _ := cfg.Marshal()
	if string(again) != got {
		t.Error("Marshal is not deterministic")
	}
}

func TestProposeMissing(t *testing.T) {
	existing := []Component{
		{Name: "main", Patterns: []string{"cmd/**"}},
		{Name: "domain", Patterns: []string{"internal/domain/**"}},
	}
	scan := Scan{Dirs: []string{
		"cmd/app",
		"internal/domain/order",
		"internal/telemetry",
		"pkg/util",
	}}
	got := ProposeMissing(existing, nil, scan, PolicyDeny)
	if want := []string{"telemetry", "util"}; !reflect.DeepEqual(componentNames(got), want) {
		t.Fatalf("proposed = %v, want %v (covered dirs must be skipped)", componentNames(got), want)
	}
	tel := got[0]
	if want := []string{"internal/telemetry/**"}; !reflect.DeepEqual(tel.Patterns, want) {
		t.Errorf("telemetry.Patterns = %v, want %v", tel.Patterns, want)
	}
	if want := []string{"std", "external"}; !reflect.DeepEqual(tel.Allow, want) {
		t.Errorf("telemetry.Allow = %v, want %v under policy deny", tel.Allow, want)
	}
}

func TestProposeMissingAllCovered(t *testing.T) {
	existing := []Component{{Name: "all", Patterns: []string{"**"}}}
	scan := Scan{Dirs: []string{"internal/a", "pkg/b"}}
	if got := ProposeMissing(existing, nil, scan, PolicyDeny); len(got) != 0 {
		t.Fatalf("proposed = %v, want none (everything covered)", componentNames(got))
	}
}

func TestProposeMissingNameCollisions(t *testing.T) {
	// "telemetry" is taken by an existing component (whose pattern does not
	// cover internal/telemetry) and "util" by a group name: both proposals must
	// be renamed deterministically instead of colliding.
	existing := []Component{{Name: "telemetry", Patterns: []string{"other/**"}}}
	scan := Scan{Dirs: []string{"internal/telemetry", "pkg/util"}}
	got := ProposeMissing(existing, []string{"util"}, scan, PolicyDeny)
	want := []string{"internal-telemetry", "pkg-util"}
	if !reflect.DeepEqual(componentNames(got), want) {
		t.Fatalf("proposed = %v, want %v", componentNames(got), want)
	}
}

func TestProposeMissingPolicyAllowHasNoAllowRefs(t *testing.T) {
	got := ProposeMissing(nil, nil, Scan{Dirs: []string{"pkg/util"}}, PolicyAllow)
	if len(got) != 1 || len(got[0].Allow) != 0 || len(got[0].Deny) != 0 {
		t.Fatalf("under policy allow a proposal is unconstrained, got %+v", got)
	}
	if RuleBody(got[0], PolicyAllow) != "" {
		t.Errorf("RuleBody must be empty for an unconstrained component")
	}
}

func TestRuleBody(t *testing.T) {
	c := Component{Name: "web", Allow: []string{"std", "external"}, Deny: []string{"core"}}
	if got, want := RuleBody(c, PolicyDeny), "{ allow: [std, external] }"; got != want {
		t.Errorf("RuleBody(deny) = %q, want %q", got, want)
	}
	if got, want := RuleBody(c, PolicyAllow), "{ deny: [core] }"; got != want {
		t.Errorf("RuleBody(allow) = %q, want %q", got, want)
	}
	if got, want := RuleBody(Component{Name: "cmd", Allow: []string{"*"}}, PolicyDeny), `{ allow: ["*"] }`; got != want {
		t.Errorf("RuleBody(*) = %q, want %q", got, want)
	}
}

func componentNames(cs []Component) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}
