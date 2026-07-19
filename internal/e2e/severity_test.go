package e2e

import "testing"

// The severity fixture has one warn-severity violation (app → lib) and nothing
// else, so the check reports it but exits 0 — graduated severity's whole point.
func TestCheckSeverityWarnText(t *testing.T) {
	out, _, exit := run(t, fixture("severity"), "check")
	if exit != 0 {
		t.Fatalf("a warn-only tree must exit 0, got %d\n%s", exit, out)
	}
	golden(t, "severity_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckSeverityWarnJSON(t *testing.T) {
	out, _, exit := run(t, fixture("severity"), "check", "--format", "json")
	if exit != 0 {
		t.Fatalf("a warn-only tree must exit 0, got %d\n%s", exit, out)
	}
	golden(t, "severity_json.golden", reJSONDur.ReplaceAllString(out, `"duration_ms": 0`))
}

// A warn violation maps to a SARIF "warning" level (vs "error" for a failing one).
func TestCheckSeverityWarnSARIF(t *testing.T) {
	out, _, exit := run(t, fixture("severity"), "check", "--format", "sarif")
	if exit != 0 {
		t.Fatalf("a warn-only tree must exit 0, got %d\n%s", exit, out)
	}
	golden(t, "severity_sarif.golden", out)
}
