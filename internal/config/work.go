package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/matterpale/depdog/internal/core"
)

// WorkFileName is the cross-unit governance file expected at the repo root.
const WorkFileName = "depdog.work.yaml"

// workFile mirrors depdog.work.yaml: named units (root-relative subtrees) and
// the cross-unit rules/boundaries/surfaces between them. It is versioned
// independently of depdog.yaml.
type workFile struct {
	Version    int                     `yaml:"version"`
	Units      map[string]workUnit     `yaml:"units"`
	Default    string                  `yaml:"default"`
	Rules      map[string]workRule     `yaml:"rules"`
	Boundaries map[string]boundaryYAML `yaml:"boundaries"`
	Surfaces   map[string]surfaceYAML  `yaml:"surfaces"`
}

// workUnit accepts both unit forms: a bare scalar (shorthand for the dir) or
// an expanded mapping {path, lang, config}.
type workUnit struct {
	Path   string `yaml:"path"`
	Lang   string `yaml:"lang"`
	Config string `yaml:"config"`
}

func (u *workUnit) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		var v string
		if err := n.Decode(&v); err != nil {
			return err
		}
		u.Path = v
		return nil
	case yaml.MappingNode:
		var aux struct {
			Path   string `yaml:"path"`
			Lang   string `yaml:"lang"`
			Config string `yaml:"config"`
		}
		if err := decodeKnownFields(n, &aux); err != nil {
			return err
		}
		u.Path, u.Lang, u.Config = aux.Path, aux.Lang, aux.Config
		return nil
	default:
		return fmt.Errorf("line %d: expected a directory or {path, lang, config}", n.Line)
	}
}

type workRule struct {
	Allow []string `yaml:"allow"`
	Deny  []string `yaml:"deny"`
}

type surfaceYAML struct {
	Exports  stringList `yaml:"exports"`
	Internal stringList `yaml:"internal"`
}

// FindWorkFile reports the work file at dir, if one exists. It looks only at
// dir itself — the work file governs the tree below the directory a check is
// run from, so there is no upward search.
func FindWorkFile(dir string) (string, bool) {
	p := filepath.Join(dir, WorkFileName)
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return "", false
	}
	return p, true
}

// LoadWork reads and compiles the work file at path.
func LoadWork(path string) (*core.WorkRules, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	w, err := ParseWork(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return w, nil
}

// ParseWork compiles raw work-file YAML into validated cross-unit rules. The
// compiled form reuses the component machinery with units as the members: each
// unit becomes a component whose single pattern is its exact directory.
func ParseWork(data []byte) (*core.WorkRules, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f workFile
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("work file is empty")
		}
		return nil, err
	}

	if f.Version != 1 {
		return nil, fmt.Errorf("unsupported work file version %d (this depdog understands version 1)", f.Version)
	}
	if len(f.Units) == 0 {
		return nil, errors.New(`no "units" defined — map at least one name to a root-relative directory`)
	}

	names := make([]string, 0, len(f.Units))
	for name := range f.Units {
		names = append(names, name)
	}
	sort.Strings(names)

	w := &core.WorkRules{
		Rules: &core.RuleSet{Rules: make(map[string]core.Rule, len(f.Rules))},
	}
	dirOwner := make(map[string]string, len(f.Units))
	for _, name := range names {
		if reserved[name] {
			return nil, fmt.Errorf("unit name %q is reserved (std, external, unassigned and * have special meaning in rules)", name)
		}
		u := f.Units[name]
		dir, err := cleanUnitDir(name, u.Path)
		if err != nil {
			return nil, err
		}
		if owner, dup := dirOwner[dir]; dup {
			return nil, fmt.Errorf("units %q and %q declare the same directory %q", owner, name, dir)
		}
		dirOwner[dir] = name
		w.Units = append(w.Units, core.WorkUnit{Name: name, Dir: dir, Lang: u.Lang, Config: u.Config})
		w.Rules.Components = append(w.Rules.Components, core.Component{Name: name, Patterns: []string{dir}})
	}

	switch f.Default {
	case "allow", "":
		w.Rules.Policy = core.PolicyAllow
	case "deny":
		w.Rules.Policy = core.PolicyDeny
	default:
		return nil, fmt.Errorf("default must be %q or %q, not %q", "allow", "deny", f.Default)
	}

	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}

	ruleNames := make([]string, 0, len(f.Rules))
	for name := range f.Rules {
		ruleNames = append(ruleNames, name)
	}
	sort.Strings(ruleNames)
	for _, name := range ruleNames {
		if !known[name] {
			return nil, fmt.Errorf("rules name unknown unit %q (units: %s)", name, strings.Join(names, ", "))
		}
		r := f.Rules[name]
		allow, err := parseUnitRefs(name, r.Allow, known, names)
		if err != nil {
			return nil, err
		}
		deny, err := parseUnitRefs(name, r.Deny, known, names)
		if err != nil {
			return nil, err
		}
		if len(allow) > 0 || len(deny) > 0 {
			w.Rules.Rules[name] = core.Rule{Allow: allow, Deny: deny}
		}
	}

	boundaries, err := parseUnitBoundaries(f.Boundaries, w, names)
	if err != nil {
		return nil, err
	}
	w.Rules.Boundaries = boundaries

	if len(f.Surfaces) > 0 {
		w.Surfaces = make(map[string]core.Surface, len(f.Surfaces))
		surfNames := make([]string, 0, len(f.Surfaces))
		for name := range f.Surfaces {
			surfNames = append(surfNames, name)
		}
		sort.Strings(surfNames)
		for _, name := range surfNames {
			if !known[name] {
				return nil, fmt.Errorf("surfaces name unknown unit %q (units: %s)", name, strings.Join(names, ", "))
			}
			s := f.Surfaces[name]
			if len(s.Exports) == 0 && len(s.Internal) == 0 {
				return nil, fmt.Errorf("surface for unit %q is empty — declare exports and/or internal globs", name)
			}
			for _, g := range append(append([]string{}, s.Exports...), s.Internal...) {
				if err := core.ValidatePattern(g); err != nil {
					return nil, fmt.Errorf("surface for unit %q: %w", name, err)
				}
			}
			w.Surfaces[name] = core.Surface{Exports: s.Exports, Internal: s.Internal}
		}
	}

	return w, nil
}

// cleanUnitDir validates and normalizes a unit's root-relative directory. It
// is a plain directory, not a glob: ownership is subtree-prefix with
// most-specific-wins, so metacharacters are rejected up front.
func cleanUnitDir(name, dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("unit %q has no path — set it to a root-relative directory", name)
	}
	if strings.Contains(dir, `\`) || strings.HasPrefix(dir, "/") {
		return "", fmt.Errorf("unit %q path %q must be relative to the work file and use forward slashes", name, dir)
	}
	if strings.ContainsAny(dir, "*?[") {
		return "", fmt.Errorf("unit %q path %q is a directory, not a glob — drop the metacharacters", name, dir)
	}
	cleaned := path.Clean(dir)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("unit %q path %q escapes the work file's directory", name, dir)
	}
	return cleaned, nil
}

// parseUnitRefs compiles a work rule's allow/deny entries: unit names or "*".
// The std/external/unassigned specials are meaningless between units and get
// an actionable error.
func parseUnitRefs(unit string, entries []string, known map[string]bool, names []string) ([]core.Ref, error) {
	refs := make([]core.Ref, 0, len(entries))
	for _, e := range entries {
		switch {
		case e == "*":
			refs = append(refs, core.Ref{Kind: core.RefAny})
		case known[e]:
			refs = append(refs, core.Ref{Kind: core.RefComponent, Name: e})
		case e == "std" || e == "external" || e == "unassigned":
			return nil, fmt.Errorf("rule for unit %q: %q has no meaning between units — cross-unit rules reference unit names (units: %s)", unit, e, strings.Join(names, ", "))
		default:
			return nil, fmt.Errorf("rule for unit %q refers to unknown unit %q (units: %s)", unit, e, strings.Join(names, ", "))
		}
	}
	return refs, nil
}

// parseUnitBoundaries compiles the work file's boundaries. Members are unit
// names only (a glob cannot name a unit); each expands to the unit's exact
// directory so BoundaryMembership resolves over the super-graph unchanged.
func parseUnitBoundaries(raw map[string]boundaryYAML, w *core.WorkRules, names []string) ([]core.Boundary, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	bnames := make([]string, 0, len(raw))
	for name := range raw {
		bnames = append(bnames, name)
	}
	sort.Strings(bnames)

	boundaries := make([]core.Boundary, 0, len(raw))
	for _, name := range bnames {
		if reserved[name] {
			return nil, fmt.Errorf("boundary name %q is reserved", name)
		}
		by := raw[name]
		if len(by.Members) == 0 {
			return nil, fmt.Errorf("boundary %q has no members — list at least two units", name)
		}
		members := make([]core.BoundaryMember, 0, len(by.Members))
		seen := make(map[string]bool, len(by.Members))
		for _, m := range by.Members {
			u := w.Unit(m)
			if u == nil {
				return nil, fmt.Errorf("boundary %q member %q is not a known unit (units: %s)", name, m, strings.Join(names, ", "))
			}
			if seen[m] {
				return nil, fmt.Errorf("boundary %q lists unit %q twice", name, m)
			}
			seen[m] = true
			members = append(members, core.BoundaryMember{Component: m, Patterns: []string{u.Dir}, Label: m})
		}
		sort.Slice(members, func(i, j int) bool { return members[i].Label < members[j].Label })
		boundaries = append(boundaries, core.Boundary{Name: name, Members: members, Sealed: by.Sealed})
	}
	return boundaries, nil
}
