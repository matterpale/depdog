package kotlin

import (
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

func TestClassifyStd(t *testing.T) {
	declared := map[string]string{}
	for _, pkg := range []string{
		"kotlin", "kotlin.collections", "kotlinx.coroutines",
		"java.util", "javax.swing", "jakarta.persistence",
	} {
		ref := importRef{Pkg: pkg, Display: pkg + ".Thing"}
		class, relDir, _, ok := classify(ref, declared)
		if !ok || class != core.ClassStd || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want std", pkg, class, relDir, ok)
		}
	}
}

func TestClassifyExternal(t *testing.T) {
	declared := map[string]string{}
	for _, pkg := range []string{
		"com.squareup.moshi", "org.junit.jupiter.api", "io.ktor.server",
		"kotlinfoo.bar", // NOT the kotlin stdlib: segment boundary matters
		"javafoo.bar",   // NOT the java platform either
	} {
		ref := importRef{Pkg: pkg, Display: pkg + ".Thing"}
		class, relDir, _, ok := classify(ref, declared)
		if !ok || class != core.ClassExternal || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want external", pkg, class, relDir, ok)
		}
	}
}

func TestClassifyInModule(t *testing.T) {
	declared := map[string]string{
		"com.example.domain":  "src/main/kotlin/com/example/domain",
		"com.example.service": "src/main/kotlin/com/example/service",
	}
	ref := importRef{Pkg: "com.example.domain", Display: "com.example.domain.Order"}
	class, relDir, display, ok := classify(ref, declared)
	if !ok || class != core.ClassInModule {
		t.Fatalf("classify: class=%v ok=%v, want in-module", class, ok)
	}
	if relDir != "src/main/kotlin/com/example/domain" {
		t.Errorf("relDir = %q, want src/main/kotlin/com/example/domain", relDir)
	}
	if display != "com.example.domain.Order" {
		t.Errorf("display = %q, want com.example.domain.Order", display)
	}
}

func TestClassifyInModuleWinsOverStdShape(t *testing.T) {
	// A first-party package that happens to sit under a std-looking prefix is
	// still in-module because it is declared by the project.
	declared := map[string]string{"kotlin.internal.tool": "src/main/kotlin/kotlin/internal/tool"}
	ref := importRef{Pkg: "kotlin.internal.tool", Display: "kotlin.internal.tool.Helper"}
	class, relDir, _, ok := classify(ref, declared)
	if !ok || class != core.ClassInModule || relDir != "src/main/kotlin/kotlin/internal/tool" {
		t.Errorf("declared package should win: class=%v relDir=%q", class, relDir)
	}
}

func TestClassifyUndeclaredIsExternal(t *testing.T) {
	// A non-std package not declared anywhere in the project degrades to external
	// rather than fabricating an in-module edge.
	declared := map[string]string{"com.example.domain": "src/main/kotlin/com/example/domain"}
	ref := importRef{Pkg: "com.example.nowhere", Display: "com.example.nowhere.Thing"}
	class, relDir, _, ok := classify(ref, declared)
	if !ok || class != core.ClassExternal || relDir != "" {
		t.Errorf("undeclared package: class=%v relDir=%q, want external", class, relDir)
	}
}

func TestIsStdlib(t *testing.T) {
	std := []string{"kotlin", "kotlin.collections", "kotlinx.coroutines", "java.util", "javax.swing", "jakarta.ws.rs"}
	for _, p := range std {
		if !isStdlib(p) {
			t.Errorf("isStdlib(%q) = false, want true", p)
		}
	}
	notStd := []string{"com.example", "kotlinfoo", "javafoo", "org.kotlinlib", ""}
	for _, p := range notStd {
		if isStdlib(p) {
			t.Errorf("isStdlib(%q) = true, want false", p)
		}
	}
}
