package e2e

import "testing"

// TestCheckCSClean drives depdog against a C# project served entirely by the
// declarative adapter engine (internal/lang/spec) — no C#-specific Go. It is
// auto-detected via the *.csproj glob marker, and `using` directives resolve by
// namespace (name-index): App.Domain is in-module, System.* is std, and
// Newtonsoft.Json is external. The clean config permits all three, so exit 0.
func TestCheckCSClean(t *testing.T) {
	out, stderr, exit := run(t, fixture("cs-clean"), "check")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	golden(t, "cs_clean_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

// TestCheckCSDirty forbids Services from importing external packages, so the
// `using Newtonsoft.Json;` directive is a violation — proving the declarative
// adapter's classification flows through evaluation and reporting exactly like a
// hand-written adapter's, with the right source position and exit code 1.
func TestCheckCSDirty(t *testing.T) {
	out, _, exit := run(t, fixture("cs-dirty"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "cs_dirty_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}
