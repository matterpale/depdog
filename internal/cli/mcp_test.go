package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// mcpTestHandler builds a real mcpHandler pinned to a fixture's depdog.yaml via
// --config, so resolution is deterministic regardless of the test's working
// directory. The command carries a context (adapters load through it) and no
// --lang flag, so language is auto-detected — exactly the discovery path the
// server uses.
func mcpTestHandler(t *testing.T, fixture string) *mcpHandler {
	t.Helper()
	cfg := mustAbs(t, filepath.Join("..", "..", "testdata", "fixtures", fixture, "depdog.yaml"))
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return newMCPHandler(cmd, cfg)
}

// TestMCPCheckRealWiring drives the real check closure against the dirty
// fixture: evaluateModule + the JSON reporter, byte-for-byte the `--format
// json` payload. It asserts the known domain → repository violation is present.
func TestMCPCheckRealWiring(t *testing.T) {
	h := mcpTestHandler(t, "dirty")
	payload, err := h.Check(context.Background(), "", false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	var report struct {
		Module     string `json:"module"`
		Violations []struct {
			FromComponent string `json:"from_component"`
			Target        string `json:"target"`
			Rule          string `json:"rule"`
		} `json:"violations"`
	}
	if err := json.Unmarshal(payload, &report); err != nil {
		t.Fatalf("check payload is not the JSON report: %v\n%s", err, payload)
	}
	if report.Module != "example.test/dirty" {
		t.Errorf("module = %q, want example.test/dirty", report.Module)
	}
	found := false
	for _, v := range report.Violations {
		if v.FromComponent == "domain" && v.Target == "repository" {
			found = true
			if v.Rule != "domain: allow [std]" {
				t.Errorf("deciding rule = %q, want domain: allow [std]", v.Rule)
			}
		}
	}
	if !found {
		t.Errorf("dirty fixture's known domain → repository violation missing\n%s", payload)
	}
}

// TestMCPExplainRealWiring asserts explain matches `depdog explain`: the
// handler → service edge is denied by the handler component's allow rule, with
// the offending file:line from the graph.
func TestMCPExplainRealWiring(t *testing.T) {
	h := mcpTestHandler(t, "dirty")
	payload, err := h.Explain(context.Background(), "internal/handler/checkout", "internal/service")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	var out struct {
		Allowed   bool   `json:"allowed"`
		DecidedBy string `json:"decided_by"`
		Reason    string `json:"reason"`
		Positions []struct {
			File string `json:"file"`
			Line int    `json:"line"`
		} `json:"positions"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("explain payload: %v\n%s", err, payload)
	}
	if out.Allowed {
		t.Error("handler → service should be denied")
	}
	if out.DecidedBy != "rule" {
		t.Errorf("decided_by = %q, want rule", out.DecidedBy)
	}
	if out.Reason != "handler: allow [domain, std]" {
		t.Errorf("reason = %q, want handler: allow [domain, std]", out.Reason)
	}
	if len(out.Positions) == 0 {
		t.Error("expected the offending edge's file:line positions")
	}
}

// TestMCPCanImportRuleSetOnly asserts can_import answers from the compiled rule
// set: allowed for handler → domain, denied for handler → service, with no
// graph loaded (the handler has no Graph on the CanImport path).
func TestMCPCanImportRuleSetOnly(t *testing.T) {
	h := mcpTestHandler(t, "dirty")
	tests := []struct {
		from, to    string
		wantAllowed bool
	}{
		{"handler", "domain", true},
		{"handler", "service", false},
		{"domain", "std", true},
		{"domain", "external", false},
	}
	for _, tt := range tests {
		payload, err := h.CanImport(context.Background(), tt.from, tt.to)
		if err != nil {
			t.Fatalf("CanImport(%s,%s): %v", tt.from, tt.to, err)
		}
		var out struct {
			Allowed   bool   `json:"allowed"`
			DecidedBy string `json:"decided_by"`
		}
		if err := json.Unmarshal(payload, &out); err != nil {
			t.Fatalf("can_import payload: %v\n%s", err, payload)
		}
		if out.Allowed != tt.wantAllowed {
			t.Errorf("can_import(%s → %s) allowed = %v, want %v", tt.from, tt.to, out.Allowed, tt.wantAllowed)
		}
	}
}

// TestMCPConfigAndComponents asserts the resources render the compiled rule set
// as JSON — the config resource carries the default policy and every component;
// the components resource is just the component list.
func TestMCPConfigAndComponents(t *testing.T) {
	h := mcpTestHandler(t, "dirty")

	cfgBytes, err := h.Config(context.Background())
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	var cfg struct {
		Default    string `json:"default"`
		Components []struct {
			Name     string   `json:"name"`
			Stance   string   `json:"stance"`
			Patterns []string `json:"patterns"`
			Allow    []string `json:"allow"`
		} `json:"components"`
	}
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		t.Fatalf("config payload: %v\n%s", err, cfgBytes)
	}
	if cfg.Default != "deny" {
		t.Errorf("default = %q, want deny", cfg.Default)
	}
	byName := map[string]bool{}
	for _, c := range cfg.Components {
		byName[c.Name] = true
	}
	for _, want := range []string{"main", "domain", "handler", "service", "repository"} {
		if !byName[want] {
			t.Errorf("config missing component %q", want)
		}
	}

	compBytes, err := h.Components(context.Background())
	if err != nil {
		t.Fatalf("Components: %v", err)
	}
	var comps struct {
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
	}
	if err := json.Unmarshal(compBytes, &comps); err != nil {
		t.Fatalf("components payload: %v\n%s", err, compBytes)
	}
	if len(comps.Components) != 5 {
		t.Errorf("got %d components, want 5", len(comps.Components))
	}
}
