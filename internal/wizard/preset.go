// Package wizard turns a repository's real layout and a chosen architecture
// preset into a depdog.yaml. It is deliberately pure: the only I/O it performs
// is reading the directory tree in ScanModule, so every preset and heuristic
// here is unit-testable without a terminal. The cli layer drives the
// interactive prompts (charmbracelet/huh) and writes the generated file.
package wizard

import (
	"fmt"
	"sort"
	"strings"
)

// Policy stances, mirroring depdog.yaml's policy field. Both are first-class:
// PolicyDeny is the whitelist stance (only allowed imports pass), PolicyAllow
// the blacklist stance (only denied imports fail).
const (
	PolicyDeny  = "deny"
	PolicyAllow = "allow"
)

// Component is one named layer of an architecture: a set of package-dir
// patterns plus the dependency refs it may import (Allow, rendered under
// policy: deny) or must not import (Deny, rendered under policy: allow).
// Refs are component names or the specials std, external, unassigned and "*".
type Component struct {
	Name     string
	Patterns []string
	Allow    []string
	Deny     []string
	Comment  string // optional one-line note rendered above the component
}

// Preset is a named starting architecture whose components carry patterns for
// the conventional layout of that style. ScanModule + Suggest reconcile these
// ideal patterns with a repository's real directories.
type Preset struct {
	Name        string
	Description string
	Components  []Component
}

// Presets returns the built-in library in stable order. init offers exactly
// these to --preset and to the interactive selector.
func Presets() []Preset {
	return []Preset{ddd(), hexagonal(), layered(), flat()}
}

// PresetNames lists the preset names in offer order, for help text and errors.
func PresetNames() []string {
	ps := Presets()
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name
	}
	return names
}

// PresetByName resolves a preset by name with a human-actionable error that
// lists the valid choices.
func PresetByName(name string) (Preset, error) {
	for _, p := range Presets() {
		if p.Name == name {
			return p, nil
		}
	}
	return Preset{}, fmt.Errorf("unknown preset %q — choose one of: %s", name, strings.Join(PresetNames(), ", "))
}

// clone returns a deep copy so callers (Suggest) can mutate freely without
// disturbing the shared preset literals.
func (p Preset) clone() Preset {
	out := p
	out.Components = make([]Component, len(p.Components))
	for i, c := range p.Components {
		cc := c
		cc.Patterns = append([]string(nil), c.Patterns...)
		cc.Allow = append([]string(nil), c.Allow...)
		cc.Deny = append([]string(nil), c.Deny...)
		out.Components[i] = cc
	}
	return out
}

// ddd is the layout from PLAN.md §3: a std-lib-only domain with handler,
// service and repository layers fanning in on it.
func ddd() Preset {
	return Preset{
		Name:        "ddd",
		Description: "Domain-driven: a std-lib-only domain, with handler/service/repository around it",
		Components: []Component{
			{Name: "main", Patterns: []string{"cmd/**"}, Allow: []string{"*"}, Comment: "wires everything together"},
			{Name: "domain", Patterns: []string{"internal/domain/**"}, Allow: []string{"std"}, Deny: []string{"handler", "service", "repository", "external", "unassigned"}, Comment: "the pure core: standard library only"},
			{Name: "handler", Patterns: []string{"internal/handler/**"}, Allow: []string{"domain", "std", "external"}, Deny: []string{"service", "repository"}},
			{Name: "service", Patterns: []string{"internal/service/**"}, Allow: []string{"domain", "std", "external"}, Deny: []string{"handler", "repository"}},
			{Name: "repository", Patterns: []string{"internal/repository/**"}, Allow: []string{"domain", "std", "external"}, Deny: []string{"handler", "service"}},
		},
	}
}

// hexagonal is ports-and-adapters: a domain core, ports defining interfaces,
// adapters implementing them at the edges.
func hexagonal() Preset {
	return Preset{
		Name:        "hexagonal",
		Description: "Ports & adapters: a domain core, ports (interfaces), and adapters at the edges",
		Components: []Component{
			{Name: "main", Patterns: []string{"cmd/**"}, Allow: []string{"*"}, Comment: "wires the adapters to the core"},
			{Name: "core", Patterns: []string{"internal/core/**"}, Allow: []string{"std"}, Deny: []string{"ports", "adapters", "external", "unassigned"}, Comment: "domain core: standard library only"},
			{Name: "ports", Patterns: []string{"internal/ports/**"}, Allow: []string{"core", "std"}, Deny: []string{"adapters", "external", "unassigned"}, Comment: "interfaces the core needs; no third-party types"},
			{Name: "adapters", Patterns: []string{"internal/adapters/**"}, Allow: []string{"core", "ports", "std", "external"}, Comment: "drivers/driven: databases, HTTP, queues"},
		},
	}
}

// layered is the classic ui -> app -> domain <- infra stack.
func layered() Preset {
	return Preset{
		Name:        "layered",
		Description: "Layered: ui -> app -> domain, with infra depending inward on domain",
		Components: []Component{
			{Name: "main", Patterns: []string{"cmd/**"}, Allow: []string{"*"}, Comment: "composition root"},
			{Name: "ui", Patterns: []string{"internal/ui/**"}, Allow: []string{"app", "domain", "std", "external"}, Deny: []string{"infra"}, Comment: "presentation; talks to app, not infra"},
			{Name: "app", Patterns: []string{"internal/app/**"}, Allow: []string{"domain", "std", "external"}, Deny: []string{"ui", "infra"}, Comment: "use cases; orchestrates the domain"},
			{Name: "domain", Patterns: []string{"internal/domain/**"}, Allow: []string{"std"}, Deny: []string{"ui", "app", "infra", "external", "unassigned"}, Comment: "the pure core: standard library only"},
			{Name: "infra", Patterns: []string{"internal/infra/**"}, Allow: []string{"domain", "std", "external"}, Deny: []string{"ui", "app"}, Comment: "implements domain interfaces against the outside world"},
		},
	}
}

// flat carries no opinionated components: Suggest maps every top-level package
// group to its own component, or emits a single commented starter when the
// module is empty. See Suggest and starterComponent.
func flat() Preset {
	return Preset{
		Name:        "flat",
		Description: "Flat: one component per top-level package directory, no layering assumptions",
		Components:  nil,
	}
}

// sortComponents orders components deterministically: preset components keep
// their authored order (recorded in order), proposed ones follow sorted by
// name. This keeps generated output stable for golden tests.
func sortComponents(cs []Component, order map[string]int) {
	sort.SliceStable(cs, func(i, j int) bool {
		oi, iok := order[cs[i].Name]
		oj, jok := order[cs[j].Name]
		switch {
		case iok && jok:
			return oi < oj
		case iok != jok:
			return iok // authored components sort before proposed ones
		default:
			return cs[i].Name < cs[j].Name
		}
	})
}
