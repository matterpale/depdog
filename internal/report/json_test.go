package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

func TestJSONWarningKinds(t *testing.T) {
	res := &core.Result{
		ModulePath: "m",
		Warnings: []core.Warning{
			{Kind: core.WarnUnassigned, Package: "m/x", RelDir: "x"},
			{Kind: core.WarnEmptyComponent, Component: "ghost"},
		},
	}
	var buf bytes.Buffer
	if err := JSON(&buf, res, 0); err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Warnings []map[string]any `json:"warnings"`
	}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(parsed.Warnings) != 2 {
		t.Fatalf("warnings = %d, want 2", len(parsed.Warnings))
	}
	// Unassigned keeps package/dir and omits component.
	un := parsed.Warnings[0]
	if un["kind"] != "unassigned" || un["package"] != "m/x" {
		t.Errorf("unassigned warning = %v", un)
	}
	if _, ok := un["component"]; ok {
		t.Errorf("unassigned warning should omit component: %v", un)
	}
	// Empty-component carries component and omits package/dir.
	em := parsed.Warnings[1]
	if em["kind"] != "empty-component" || em["component"] != "ghost" {
		t.Errorf("empty-component warning = %v", em)
	}
	if _, ok := em["package"]; ok {
		t.Errorf("empty-component warning should omit package: %v", em)
	}
	if strings.Contains(buf.String(), `"component": ""`) {
		t.Errorf("empty component field should be omitted, not blank:\n%s", buf.String())
	}
}
