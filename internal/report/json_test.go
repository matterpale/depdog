package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

func TestJSONComponentsAndPolicy(t *testing.T) {
	rs := &core.RuleSet{
		Components: []core.Component{{Name: "app", Patterns: []string{"a"}}, {Name: "domain", Patterns: []string{"d"}}},
		Rules: map[string]core.Rule{
			"app":    {Deny: []core.Ref{{Kind: core.RefExternal}}},
			"domain": {Allow: []core.Ref{{Kind: core.RefStd}}},
		},
		Policy: core.PolicyDeny,
	}
	res := &core.Result{
		ModulePath: "m",
		Components: []core.ComponentStat{{Name: "app"}, {Name: "domain", Packages: 2}},
	}
	var buf bytes.Buffer
	if err := JSON(&buf, res, rs, 0); err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Default    string           `json:"default"`
		Components []map[string]any `json:"components"`
	}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if parsed.Default != "deny" {
		t.Errorf("default = %q, want deny", parsed.Default)
	}
	app := parsed.Components[0]
	if app["stance"] != "blacklist" { // a deny-only rule
		t.Errorf("app stance = %v, want blacklist", app["stance"])
	}
	if _, ok := app["allow"]; ok {
		t.Errorf("app should omit an empty allow: %v", app)
	}
	dom := parsed.Components[1]
	if dom["stance"] != "whitelist" {
		t.Errorf("domain stance = %v, want whitelist", dom["stance"])
	}
	if allow, _ := dom["allow"].([]any); len(allow) != 1 || allow[0] != "std" {
		t.Errorf("domain allow = %v, want [std]", dom["allow"])
	}
}

func TestJSONWarningKinds(t *testing.T) {
	res := &core.Result{
		ModulePath: "m",
		Warnings: []core.Warning{
			{Kind: core.WarnUnassigned, Package: "m/x", RelDir: "x"},
			{Kind: core.WarnEmptyComponent, Component: "ghost"},
		},
	}
	var buf bytes.Buffer
	if err := JSON(&buf, res, &core.RuleSet{Policy: core.PolicyDeny}, 0); err != nil {
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
