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
	Version    int                   `yaml:"version"`
	Module     string                `yaml:"module"`
	Components map[string]stringList `yaml:"components"`
	Policy     string                `yaml:"policy"`
	Rules      map[string]ruleYAML   `yaml:"rules"`
	Options    optionsYAML           `yaml:"options"`
}

type ruleYAML struct {
	Allow []string `yaml:"allow"`
	Deny  []string `yaml:"deny"`
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
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f file
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("config file is empty")
		}
		return nil, err
	}

	if f.Version != 1 {
		return nil, fmt.Errorf("unsupported config version %d (this depdog understands version 1)", f.Version)
	}
	if len(f.Components) == 0 {
		return nil, errors.New(`no "components" defined — map at least one name to package patterns`)
	}

	rs := &core.RuleSet{Rules: make(map[string]core.Rule, len(f.Rules))}

	names := make([]string, 0, len(f.Components))
	for name := range f.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if reserved[name] {
			return nil, fmt.Errorf("component name %q is reserved (std, external, unassigned and * have special meaning in rules)", name)
		}
		patterns := f.Components[name]
		if len(patterns) == 0 {
			return nil, fmt.Errorf("component %q has no patterns", name)
		}
		for _, p := range patterns {
			if err := core.ValidatePattern(p); err != nil {
				return nil, fmt.Errorf("component %q: %w", name, err)
			}
		}
		rs.Components = append(rs.Components, core.Component{Name: name, Patterns: patterns})
	}

	switch f.Policy {
	case "deny":
		rs.Policy = core.PolicyDeny
	case "allow":
		rs.Policy = core.PolicyAllow
	case "":
		return nil, errors.New(`missing "policy": set "deny" (whitelist style: only allowed imports pass) or "allow" (blacklist style: only denied imports fail)`)
	default:
		return nil, fmt.Errorf("policy must be %q or %q, not %q", "deny", "allow", f.Policy)
	}

	known := make(map[string]bool, len(f.Components))
	for name := range f.Components {
		known[name] = true
	}
	ruleNames := make([]string, 0, len(f.Rules))
	for name := range f.Rules {
		ruleNames = append(ruleNames, name)
	}
	sort.Strings(ruleNames)
	for _, name := range ruleNames {
		if !known[name] {
			return nil, fmt.Errorf("rule for unknown component %q (known: %s)", name, strings.Join(names, ", "))
		}
		r := f.Rules[name]
		allow, err := parseRefs(name, r.Allow, known)
		if err != nil {
			return nil, err
		}
		deny, err := parseRefs(name, r.Deny, known)
		if err != nil {
			return nil, err
		}
		rs.Rules[name] = core.Rule{Allow: allow, Deny: deny}
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

func parseRefs(rule string, entries []string, known map[string]bool) ([]core.Ref, error) {
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
			if !known[e] {
				return nil, fmt.Errorf("rule %q refers to unknown component %q", rule, e)
			}
			refs = append(refs, core.Ref{Kind: core.RefComponent, Name: e})
		}
	}
	return refs, nil
}
