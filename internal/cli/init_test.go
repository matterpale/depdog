package cli

import (
	"testing"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/wizard"
)

// TestGeneratedConfigsRoundTrip is the contract the whole wizard rests on:
// whatever preset, policy stance and repository shape it is handed, the file it
// produces must satisfy the same validator `depdog check` uses.
func TestGeneratedConfigsRoundTrip(t *testing.T) {
	scans := map[string]wizard.Scan{
		"empty":      {},
		"ddd":        {Dirs: []string{"cmd/app", "internal/domain/order", "internal/handler", "internal/service", "internal/repository", "pkg/util"}},
		"hexagonal":  {Dirs: []string{"cmd/app", "internal/core/model", "internal/ports", "internal/adapters/db"}},
		"layered":    {Dirs: []string{"cmd/app", "internal/ui", "internal/app", "internal/domain", "internal/infra"}},
		"nonconform": {Dirs: []string{"src/a", "src/b", "web/handler"}},
	}
	for _, preset := range wizard.PresetNames() {
		p, err := wizard.PresetByName(preset)
		if err != nil {
			t.Fatal(err)
		}
		for _, policy := range []string{wizard.PolicyDeny, wizard.PolicyAllow} {
			for scanName, scan := range scans {
				t.Run(preset+"/"+policy+"/"+scanName, func(t *testing.T) {
					data, err := wizard.Suggest(p, scan, policy).Marshal()
					if err != nil {
						t.Fatalf("Marshal: %v", err)
					}
					if _, err := config.Parse(data); err != nil {
						t.Fatalf("generated config does not parse: %v\n%s", err, data)
					}
				})
			}
		}
	}
}

// TestEditedConfigsRoundTrip extends the contract to the review's edit pass:
// after renaming a component and rewriting its patterns — what editComponents
// does interactively — the file must still satisfy the check validator.
func TestEditedConfigsRoundTrip(t *testing.T) {
	scan := wizard.Scan{Dirs: []string{"cmd/app", "internal/domain/order", "internal/handler", "internal/service"}}
	for _, preset := range wizard.PresetNames() {
		p, err := wizard.PresetByName(preset)
		if err != nil {
			t.Fatal(err)
		}
		for _, policy := range []string{wizard.PolicyDeny, wizard.PolicyAllow} {
			t.Run(p.Name+"/"+policy, func(t *testing.T) {
				cfg := wizard.Suggest(p, scan, policy)
				target := cfg.Components[len(cfg.Components)-1].Name
				if cfg, err = cfg.SetPatterns(target, []string{"internal/renamed/**", "pkg/extra"}); err != nil {
					t.Fatalf("SetPatterns: %v", err)
				}
				if cfg, err = cfg.Rename(target, "renamed"); err != nil {
					t.Fatalf("Rename: %v", err)
				}
				data, err := cfg.Marshal()
				if err != nil {
					t.Fatalf("Marshal: %v", err)
				}
				if _, err := config.Parse(data); err != nil {
					t.Fatalf("edited config does not parse: %v\n%s", err, data)
				}
			})
		}
	}
}

// TestMergedConfigsRoundTrip extends the contract to `init --merge`: whatever
// existing config shape and scan the merge pipeline (ProposeMissing → RuleBody
// → MergeComponents) is handed, the merged file must satisfy the same
// validator `depdog check` uses, and merging when everything is covered must
// return the input untouched.
func TestMergedConfigsRoundTrip(t *testing.T) {
	scan := wizard.Scan{Dirs: []string{
		"cmd/app", "internal/domain/order", "internal/handler",
		"internal/telemetry", "pkg/util", "web/assets",
	}}
	existing := map[string]string{
		"plain":      "version: 1\ncomponents:\n  main: [\"cmd/**\"]\n  domain: [\"internal/domain/**\"]\npolicy: deny\nrules:\n  domain: { allow: [std] }\n",
		"no rules":   "version: 1\ncomponents:\n  main: [\"cmd/**\"]\npolicy: deny\n",
		"allow":      "version: 1\ncomponents:\n  main: [\"cmd/**\"]\npolicy: allow\n",
		"groups":     "version: 1\ncomponents:\n  main: [\"cmd/**\"]\ngroups:\n  util: [main]\npolicy: deny\nrules:\n  main: { allow: [util, std] }\n",
		"everything": "version: 1\ncomponents:\n  all: [\"**\"]\npolicy: deny\n",
	}
	for name, in := range existing {
		t.Run(name, func(t *testing.T) {
			rs, err := config.Parse([]byte(in))
			if err != nil {
				t.Fatalf("fixture config does not parse: %v", err)
			}
			taken, err := config.DeclaredNames([]byte(in))
			if err != nil {
				t.Fatal(err)
			}
			comps := make([]wizard.Component, len(rs.Components))
			for i, c := range rs.Components {
				comps[i] = wizard.Component{Name: c.Name, Patterns: c.Patterns}
			}
			policy := wizard.PolicyDeny
			if rs.Policy == core.PolicyAllow {
				policy = wizard.PolicyAllow
			}
			proposed := wizard.ProposeMissing(comps, taken, scan, policy)
			if name == "everything" {
				if len(proposed) != 0 {
					t.Fatalf("nothing is uncovered, yet proposed %d components", len(proposed))
				}
				return
			}
			add := make([]config.MergeComponent, len(proposed))
			for i, c := range proposed {
				add[i] = config.MergeComponent{Name: c.Name, Patterns: c.Patterns, Comment: c.Comment, Rule: wizard.RuleBody(c, policy)}
			}
			merged, err := config.MergeComponents([]byte(in), add)
			if err != nil {
				t.Fatalf("MergeComponents: %v", err)
			}
			if _, err := config.Parse(merged); err != nil {
				t.Fatalf("merged config does not parse: %v\n%s", err, merged)
			}
			if name == "groups" {
				// "util" is taken by a group: the pkg/util proposal must have
				// been renamed, or Parse above would have rejected the merge.
				for _, c := range proposed {
					if c.Name == "util" {
						t.Errorf("proposal %q collides with the group of the same name", c.Name)
					}
				}
			}
		})
	}
}
