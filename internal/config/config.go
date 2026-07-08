// Package config loads depdog.yaml and compiles it into a core.RuleSet.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/matterpale/depdog/internal/core"
)

// DefaultName is the config file expected next to go.mod.
const DefaultName = "depdog.yaml"

// reserved are names components may not use because rules give them special
// meaning.
var reserved = map[string]bool{"std": true, "external": true, "unassigned": true, "*": true}

type file struct {
	Version    int                      `yaml:"version"`
	Module     string                   `yaml:"module"`
	Components map[string]componentYAML `yaml:"components"`
	Groups     map[string]stringList    `yaml:"groups"`
	Default    string                   `yaml:"default"`
	Options    optionsYAML              `yaml:"options"`
}

// componentYAML is one entry of the merged components block: the patterns a
// component claims plus, inline, the rule saying who it may (allow) or must
// not (deny) import.
type componentYAML struct {
	Path  stringList `yaml:"path"`
	Allow []string   `yaml:"allow"`
	Deny  []string   `yaml:"deny"`
}

type optionsYAML struct {
	TestFiles string   `yaml:"test_files"`
	Skip      []string `yaml:"skip"`
}

// stringList accepts both a single scalar and a sequence in YAML.
type stringList []string

func (s *stringList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		var v string
		if err := n.Decode(&v); err != nil {
			return err
		}
		*s = stringList{v}
		return nil
	case yaml.SequenceNode:
		var v []string
		if err := n.Decode(&v); err != nil {
			return err
		}
		*s = stringList(v)
		return nil
	default:
		return fmt.Errorf("line %d: expected a pattern or a list of patterns", n.Line)
	}
}

// Load reads and compiles the config file at path.
func Load(path string) (*core.RuleSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rs, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rs, nil
}

// Parse compiles raw YAML into a validated rule set.
func Parse(data []byte) (*core.RuleSet, error) {
	// The v1 layout (separate components: and rules: blocks) gets a migration
	// error built from the user's own config, not a generic decode failure.
	if err := legacyError(data); err != nil {
		return nil, err
	}
	// The stance field was renamed policy -> default (and its default flipped).
	if err := renamedFieldError(data); err != nil {
		return nil, err
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f file
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("config file is empty")
		}
		return nil, err
	}

	if f.Version != 2 {
		return nil, fmt.Errorf("unsupported config version %d (this depdog understands version 2)", f.Version)
	}
	if len(f.Components) == 0 {
		return nil, errors.New(`no "components" defined — map at least one name to package patterns`)
	}

	rs := &core.RuleSet{Rules: make(map[string]core.Rule, len(f.Components))}

	names := make([]string, 0, len(f.Components))
	for name := range f.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if reserved[name] {
			return nil, fmt.Errorf("component name %q is reserved (std, external, unassigned and * have special meaning in rules)", name)
		}
		patterns := f.Components[name].Path
		if len(patterns) == 0 {
			return nil, fmt.Errorf("component %q has no patterns — set path to a glob or a list of globs", name)
		}
		for _, p := range patterns {
			if err := core.ValidatePattern(p); err != nil {
				return nil, fmt.Errorf("component %q: %w", name, err)
			}
		}
		rs.Components = append(rs.Components, core.Component{Name: name, Patterns: patterns})
	}

	switch f.Default {
	case "allow", "":
		// Optional; absent it defaults to the open (blacklist) stance: a
		// component with no allow/deny rule may import anything. Components
		// still infer their own stance from allow vs deny.
		rs.Policy = core.PolicyAllow
	case "deny":
		rs.Policy = core.PolicyDeny
	default:
		return nil, fmt.Errorf("default must be %q or %q, not %q", "allow", "deny", f.Default)
	}

	known := make(map[string]bool, len(f.Components))
	for name := range f.Components {
		known[name] = true
	}

	groups, err := parseGroups(f.Groups, known, names)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		c := f.Components[name]
		allow, err := parseRefs(name, c.Allow, known, groups)
		if err != nil {
			return nil, err
		}
		deny, err := parseRefs(name, c.Deny, known, groups)
		if err != nil {
			return nil, err
		}
		if len(allow) > 0 || len(deny) > 0 {
			rs.Rules[name] = core.Rule{Allow: allow, Deny: deny}
		}
	}

	switch f.Options.TestFiles {
	case "", "hybrid":
		rs.TestFiles = core.TestHybrid
	case "same-rules":
		rs.TestFiles = core.TestSameRules
	case "relaxed":
		rs.TestFiles = core.TestRelaxed
	default:
		return nil, fmt.Errorf("options.test_files must be hybrid, same-rules or relaxed, not %q", f.Options.TestFiles)
	}
	for _, p := range f.Options.Skip {
		if err := core.ValidatePattern(p); err != nil {
			return nil, fmt.Errorf("options.skip: %w", err)
		}
	}
	rs.Skip = f.Options.Skip

	return rs, nil
}

// parseGroups validates the optional `groups` map (each a named set of
// components) and returns name -> member components. A group may not use a
// reserved name or collide with a component, and every member must be a known
// component.
func parseGroups(raw map[string]stringList, known map[string]bool, componentNames []string) (map[string][]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	groups := make(map[string][]string, len(raw))
	gnames := make([]string, 0, len(raw))
	for name := range raw {
		gnames = append(gnames, name)
	}
	sort.Strings(gnames)
	for _, name := range gnames {
		if reserved[name] {
			return nil, fmt.Errorf("group name %q is reserved", name)
		}
		if known[name] {
			return nil, fmt.Errorf("group %q collides with a component of the same name", name)
		}
		members := raw[name]
		if len(members) == 0 {
			return nil, fmt.Errorf("group %q has no members", name)
		}
		for _, m := range members {
			if !known[m] {
				return nil, fmt.Errorf("group %q member %q is not a known component (known: %s)", name, m, strings.Join(componentNames, ", "))
			}
		}
		groups[name] = members
	}
	return groups, nil
}

func parseRefs(comp string, entries []string, known map[string]bool, groups map[string][]string) ([]core.Ref, error) {
	refs := make([]core.Ref, 0, len(entries))
	for _, e := range entries {
		switch e {
		case "*":
			refs = append(refs, core.Ref{Kind: core.RefAny})
		case "std":
			refs = append(refs, core.Ref{Kind: core.RefStd})
		case "external":
			refs = append(refs, core.Ref{Kind: core.RefExternal})
		case "unassigned":
			refs = append(refs, core.Ref{Kind: core.RefUnassigned})
		default:
			if members, ok := groups[e]; ok {
				for _, m := range members {
					refs = append(refs, core.Ref{Kind: core.RefComponent, Name: m})
				}
				continue
			}
			if known[e] {
				refs = append(refs, core.Ref{Kind: core.RefComponent, Name: e})
				continue
			}
			// A ref that is neither a component nor a group, but looks like an
			// import path, restricts a specific external module (depguard-style).
			if strings.ContainsAny(e, "/.") {
				refs = append(refs, core.Ref{Kind: core.RefExternalModule, Name: e})
				continue
			}
			return nil, fmt.Errorf("component %q refers to unknown component or group %q", comp, e)
		}
	}
	return refs, nil
}
