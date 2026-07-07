package cli

import (
	"testing"

	"github.com/matterpale/depdog/internal/config"
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
