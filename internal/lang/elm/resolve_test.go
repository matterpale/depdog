package elm

import (
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

func TestClassifyStd(t *testing.T) {
	declared := map[string]string{}
	for _, mod := range []string{
		"List", "Dict", "Set", "Array", "Maybe", "Result", "String", "Char",
		"Basics", "Bitwise", "Tuple", "Debug", "Task", "Process",
		"Platform", "Platform.Cmd", "Platform.Sub",
	} {
		ref := importRef{Module: mod}
		class, relDir, display, ok := classify(ref, declared)
		if !ok || class != core.ClassStd || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want std", mod, class, relDir, ok)
		}
		if display != mod {
			t.Errorf("classify(%q): display=%q, want %q", mod, display, mod)
		}
	}
}

func TestClassifyExternal(t *testing.T) {
	declared := map[string]string{}
	for _, mod := range []string{
		// elm/json and elm/html are dependency packages, NOT elm/core std.
		"Json.Decode", "Json.Encode", "Html", "Html.Attributes",
		"Http", "Browser", "Url", "Time",
		"Listicle", // NOT "List": exact-match std, no prefix false-match
	} {
		ref := importRef{Module: mod}
		class, relDir, _, ok := classify(ref, declared)
		if !ok || class != core.ClassExternal || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want external", mod, class, relDir, ok)
		}
	}
}

func TestClassifyInModule(t *testing.T) {
	declared := map[string]string{
		"Domain.Order":   "src/Domain",
		"Service.Orders": "src/Service",
	}
	ref := importRef{Module: "Domain.Order"}
	class, relDir, display, ok := classify(ref, declared)
	if !ok || class != core.ClassInModule {
		t.Fatalf("classify: class=%v ok=%v, want in-module", class, ok)
	}
	if relDir != "src/Domain" {
		t.Errorf("relDir = %q, want src/Domain", relDir)
	}
	if display != "Domain.Order" {
		t.Errorf("display = %q, want Domain.Order", display)
	}
}

func TestClassifyInModuleWinsOverStdShape(t *testing.T) {
	// A first-party module that happens to be named like an elm/core module is
	// still in-module because the project declares (ships) it.
	declared := map[string]string{"List": "src"}
	ref := importRef{Module: "List"}
	class, relDir, _, ok := classify(ref, declared)
	if !ok || class != core.ClassInModule || relDir != "src" {
		t.Errorf("declared module should win over std: class=%v relDir=%q", class, relDir)
	}
}

func TestClassifyUndeclaredNonStdIsExternal(t *testing.T) {
	declared := map[string]string{"Domain.Order": "src/Domain"}
	ref := importRef{Module: "Service.Nowhere"}
	class, relDir, _, ok := classify(ref, declared)
	if !ok || class != core.ClassExternal || relDir != "" {
		t.Errorf("undeclared non-std module: class=%v relDir=%q, want external", class, relDir)
	}
}

func TestIsStdlib(t *testing.T) {
	std := []string{
		"List", "Dict", "Set", "Array", "Maybe", "Result", "String", "Char",
		"Basics", "Bitwise", "Tuple", "Debug", "Task", "Process",
		"Platform", "Platform.Cmd", "Platform.Sub",
	}
	for _, m := range std {
		if !isStdlib(m) {
			t.Errorf("isStdlib(%q) = false, want true", m)
		}
	}
	notStd := []string{
		"Json.Decode", "Json.Encode", "Html", "Http", "Browser",
		"Platform.Internal", // Platform is std but Platform.Internal is not
		"list", "",          // case-sensitive; empty is not std
		"Domain.Order",
	}
	for _, m := range notStd {
		if isStdlib(m) {
			t.Errorf("isStdlib(%q) = true, want false", m)
		}
	}
}

func TestModuleNodeDir(t *testing.T) {
	tests := []struct {
		srcDir string
		module string
		want   string
	}{
		{"src", "Domain.Order", "src/Domain"},
		{"src", "Main", "src"},
		{"src", "Service.Orders.Notify", "src/Service/Orders"},
		{"app", "Domain.Order", "app/Domain"},
		{".", "Main", "."},
		{".", "Domain.Order", "Domain"},
	}
	for _, tc := range tests {
		if got := moduleNodeDir(tc.srcDir, tc.module); got != tc.want {
			t.Errorf("moduleNodeDir(%q, %q) = %q, want %q", tc.srcDir, tc.module, got, tc.want)
		}
	}
}

func TestModuleFromFile(t *testing.T) {
	tests := []struct {
		srcDir  string
		relFile string
		want    string
		wantOK  bool
	}{
		{"src", "src/Domain/Order.elm", "Domain.Order", true},
		{"src", "src/Main.elm", "Main", true},
		{"src", "src/Service/Orders/Notify.elm", "Service.Orders.Notify", true},
		{"app", "app/Domain/Order.elm", "Domain.Order", true},
		{".", "Main.elm", "Main", true},
		{".", "Domain/Order.elm", "Domain.Order", true},
		{"src", "other/Thing.elm", "", false},      // not under this source dir
		{"src", "src/Domain/Order.txt", "", false}, // not a .elm file
		{"src", "src", "", false},                  // the source dir itself
	}
	for _, tc := range tests {
		got, ok := moduleFromFile(tc.srcDir, tc.relFile)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("moduleFromFile(%q, %q) = (%q, %v), want (%q, %v)",
				tc.srcDir, tc.relFile, got, ok, tc.want, tc.wantOK)
		}
	}
}
