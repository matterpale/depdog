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
	Lang       string                   `yaml:"lang"`
	Components map[string]componentYAML `yaml:"components"`
	Aliases    map[string]stringList    `yaml:"aliases"`
	// Groups is the deprecated pre-1.1 spelling of Aliases. It shipped in v1.0.0,
	// so it stays accepted through the 1.x line (docs/compatibility.md); Parse
	// treats it as a synonym of Aliases and emits a deprecation advisory. Setting
	// both keys is an error.
	Groups     map[string]stringList   `yaml:"groups"`
	Boundaries map[string]boundaryYAML `yaml:"boundaries"`
	Default    string                  `yaml:"default"`
	// Deny is the module-wide deny list: refs (typically external-module prefixes)
	// that no package anywhere may import, regardless of its component. It is a
	// hard, global ban that wins over any component allow — for security/license
	// bans that must hold across the whole project, not one layer.
	Deny    []string    `yaml:"deny"`
	Options optionsYAML `yaml:"options"`
}

// componentYAML is one entry of the merged components block: the patterns a
// component claims plus, inline, the rule saying who it may (allow) or must
// not (deny) import.
type componentYAML struct {
	Path     stringList `yaml:"path"`
	Allow    []string   `yaml:"allow"`
	Deny     []string   `yaml:"deny"`
	Severity string     `yaml:"severity"` // "" (default) / "error" / "warn"
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

// boundaryYAML accepts both boundary forms: a bare list of members (shorthand,
// symmetric and not sealed) or an expanded mapping {members, sealed}.
type boundaryYAML struct {
	Members  stringList `yaml:"members"`
	Sealed   bool       `yaml:"sealed"`
	Severity string     `yaml:"severity"` // "" (default) / "error" / "warn"
}

func (b *boundaryYAML) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.SequenceNode:
		// Shorthand: a list of members, symmetric and unsealed.
		var members []string
		if err := n.Decode(&members); err != nil {
			return err
		}
		b.Members = stringList(members)
		b.Sealed = false
		return nil
	case yaml.MappingNode:
		// Expanded form. A custom UnmarshalYAML bypasses the top-level decoder's
		// KnownFields(true), so reject unknown sub-keys here to keep the
		// actionable-error-on-typo convention (e.g. `seald: true`).
		var aux struct {
			Members  stringList `yaml:"members"`
			Sealed   bool       `yaml:"sealed"`
			Severity string     `yaml:"severity"`
		}
		if err := decodeKnownFields(n, &aux); err != nil {
			return err
		}
		b.Members = aux.Members
		b.Sealed = aux.Sealed
		b.Severity = aux.Severity
		return nil
	default:
		return fmt.Errorf("line %d: expected a list of members or {members, sealed}", n.Line)
	}
}

// decodeKnownFields decodes a mapping node into v, rejecting keys v does not
// declare — the strict decode a custom UnmarshalYAML must do itself, since it
// bypasses the parent decoder's KnownFields setting.
func decodeKnownFields(n *yaml.Node, v any) error {
	// yaml.Node.Decode does not honour KnownFields; round-trip through a decoder
	// that does, over the node's own serialization.
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	if err := enc.Encode(n); err != nil {
		return err
	}
	enc.Close()
	dec := yaml.NewDecoder(&buf)
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

// parseSeverity maps a config `severity:` string to a core.Severity. Empty is
// the default (error, so the field is purely additive); an unknown value is an
// actionable config error, in the style of the `default`/`test_files` checks.
func parseSeverity(where, s string) (core.Severity, error) {
	switch s {
	case "", "error":
		return core.SeverityError, nil
	case "warn":
		return core.SeverityWarn, nil
	default:
		return core.SeverityError, fmt.Errorf("%s severity %q must be \"warn\" or \"error\"", where, s)
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

// PeekLang reads only the optional `lang:` key from the config at path, without
// validating the rest of the file. It exists so the CLI can resolve a unit's
// adapter (which it needs to find the project root) before the full Load — which
// would otherwise be a chicken-and-egg between adapter selection and config
// parsing. A missing file, a non-scalar `lang:`, or any other decode problem
// yields "" and no error; the authoritative Load reports real config errors.
func PeekLang(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var peek struct {
		Lang string `yaml:"lang"`
	}
	if err := yaml.Unmarshal(data, &peek); err != nil {
		return ""
	}
	return peek.Lang
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
	// lang pins the language adapter for this unit. config carries it opaquely;
	// the CLI validates it against the adapter registry (config must not import
	// the registry). Empty means auto-detect.
	rs.Lang = f.Lang

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
		sev, err := parseSeverity(fmt.Sprintf("component %q", name), f.Components[name].Severity)
		if err != nil {
			return nil, err
		}
		rs.Components = append(rs.Components, core.Component{Name: name, Patterns: patterns, Severity: sev})
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

	// `aliases` is the current key; `groups` is its deprecated pre-1.1 synonym.
	// Accept either (not both). `groups` is frozen at its 1.0 behaviour —
	// components only — so its accepted input never changes under the 1.x line;
	// naming an external-module prefix requires the wider `aliases` key.
	rawAliases, noun, allowExternal := f.Aliases, "alias", true
	if len(f.Groups) > 0 {
		if len(f.Aliases) > 0 {
			return nil, errors.New("set `aliases:` or the deprecated `groups:`, not both — move every entry under `aliases:`")
		}
		rawAliases, noun, allowExternal = f.Groups, "group", false
		rs.Deprecations = append(rs.Deprecations,
			"`groups:` was renamed to `aliases:` (which additionally accepts external-module prefixes); "+
				"it still works but is deprecated and will be removed in the next major release — rename the block to `aliases:`")
	}
	aliases, err := parseAliases(rawAliases, known, names, noun, allowExternal)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		c := f.Components[name]
		subject := fmt.Sprintf("component %q", name)
		allow, err := parseRefs(subject, c.Allow, known, aliases)
		if err != nil {
			return nil, err
		}
		deny, err := parseRefs(subject, c.Deny, known, aliases)
		if err != nil {
			return nil, err
		}
		if len(allow) > 0 || len(deny) > 0 {
			rs.Rules[name] = core.Rule{Allow: allow, Deny: deny}
		}
	}

	// The top-level deny is the module-wide ban. It uses the same ref vocabulary
	// as a component rule but belongs to no component, so it is parsed once here.
	globalDeny, err := parseRefs("top-level deny", f.Deny, known, aliases)
	if err != nil {
		return nil, err
	}
	rs.GlobalDeny = globalDeny

	boundaries, err := parseBoundaries(f.Boundaries, known, names, rs.Components)
	if err != nil {
		return nil, err
	}
	rs.Boundaries = boundaries

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

// parseAliases validates an alias map (the `aliases` key, or its deprecated
// `groups` synonym) and returns name -> the refs it expands to. A member is a
// component or, when allowExternal is set, an external-module prefix — told apart
// by the same heuristic parseRefs uses inline: a bare word is a component,
// anything with a "/" or "." is an external-module prefix. noun ("alias" or
// "group") names the entity in error messages. The deprecated `groups` key is
// frozen at its 1.0 behaviour (components only, allowExternal=false), so a prefix
// member there is an actionable error pointing at `aliases`. An entry may not use
// a reserved name or collide with a component. Members are resolved to refs here,
// once, so parseRefs can splice them in by name.
func parseAliases(raw map[string]stringList, known map[string]bool, componentNames []string, noun string, allowExternal bool) (map[string][]core.Ref, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	aliases := make(map[string][]core.Ref, len(raw))
	anames := make([]string, 0, len(raw))
	for name := range raw {
		anames = append(anames, name)
	}
	sort.Strings(anames)
	for _, name := range anames {
		if reserved[name] {
			return nil, fmt.Errorf("%s name %q is reserved", noun, name)
		}
		if known[name] {
			return nil, fmt.Errorf("%s %q collides with a component of the same name", noun, name)
		}
		members := raw[name]
		if len(members) == 0 {
			return nil, fmt.Errorf("%s %q has no members", noun, name)
		}
		refs := make([]core.Ref, 0, len(members))
		for _, m := range members {
			switch {
			case known[m]:
				refs = append(refs, core.Ref{Kind: core.RefComponent, Name: m})
			case strings.ContainsAny(m, "/."):
				if !allowExternal {
					// `groups` is components-only; external prefixes live under `aliases`.
					return nil, fmt.Errorf("%s %q member %q looks like an external-module prefix, which the deprecated `groups:` key does not accept — move this entry to the `aliases:` key, which names external modules as well as components", noun, name, m)
				}
				refs = append(refs, core.Ref{Kind: core.RefExternalModule, Name: m})
			case allowExternal:
				return nil, fmt.Errorf("%s %q member %q is not a known component or an external-module prefix (a prefix needs a %q or %q, e.g. github.com/pkg/errors) (known components: %s)", noun, name, m, "/", ".", strings.Join(componentNames, ", "))
			default:
				return nil, fmt.Errorf("%s %q member %q is not a known component (known: %s)", noun, name, m, strings.Join(componentNames, ", "))
			}
		}
		aliases[name] = refs
	}
	return aliases, nil
}

// isGlobMember reports whether a boundary member string is a path glob rather
// than a bare component name. It extends the allow/deny ref heuristic
// (ContainsAny "/.") with glob metacharacters so a single-segment glob like
// "cmd*" is read as a glob, not mis-read as an unknown component.
func isGlobMember(m string) bool {
	return strings.ContainsAny(m, "/.*?[")
}

// parseBoundaries validates the optional `boundaries` map (named
// mutual-exclusion groups) and returns the compiled boundaries, sorted by name.
// A member is a known component name or a path glob, told apart by the same
// heuristic as allow/deny refs (extended so glob metacharacters mean "glob").
// Members disjoint-within-a-boundary is enforced authoritatively at runtime by
// the membership index; here only a cheap duplicate/identical-pattern check
// catches obvious authoring mistakes.
func parseBoundaries(raw map[string]boundaryYAML, known map[string]bool, componentNames []string, components []core.Component) ([]core.Boundary, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	// Component name → its patterns, for expanding component members.
	patternsOf := make(map[string][]string, len(components))
	for _, c := range components {
		patternsOf[c.Name] = c.Patterns
	}

	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)

	boundaries := make([]core.Boundary, 0, len(raw))
	for _, name := range names {
		if reserved[name] {
			return nil, fmt.Errorf("boundary name %q is reserved", name)
		}
		by := raw[name]
		if len(by.Members) == 0 {
			return nil, fmt.Errorf("boundary %q has no members — list at least two components or globs", name)
		}
		members := make([]core.BoundaryMember, 0, len(by.Members))
		seenComponent := make(map[string]bool, len(by.Members))
		seenPattern := make(map[string]string, len(by.Members)) // pattern → member label that owns it
		for _, m := range by.Members {
			switch {
			case known[m]:
				if seenComponent[m] {
					return nil, fmt.Errorf("boundary %q lists component %q twice", name, m)
				}
				seenComponent[m] = true
				pats := patternsOf[m]
				for _, pat := range pats {
					if owner, dup := seenPattern[pat]; dup {
						return nil, fmt.Errorf("boundary %q members %q and %q overlap with equal specificity — make one more specific", name, owner, m)
					}
					seenPattern[pat] = m
				}
				members = append(members, core.BoundaryMember{Component: m, Patterns: pats, Label: m})
			case isGlobMember(m):
				if err := core.ValidatePattern(m); err != nil {
					return nil, fmt.Errorf("boundary %q member %q: %w", name, m, err)
				}
				if owner, dup := seenPattern[m]; dup {
					return nil, fmt.Errorf("boundary %q members %q and %q overlap with equal specificity — make one more specific", name, owner, m)
				}
				seenPattern[m] = m
				members = append(members, core.BoundaryMember{Patterns: []string{m}, Label: m})
			default:
				return nil, fmt.Errorf("boundary %q member %q is not a known component or a path glob (known components: %s)", name, m, strings.Join(componentNames, ", "))
			}
		}
		sort.Slice(members, func(i, j int) bool { return members[i].Label < members[j].Label })
		sev, err := parseSeverity(fmt.Sprintf("boundary %q", name), by.Severity)
		if err != nil {
			return nil, err
		}
		boundaries = append(boundaries, core.Boundary{Name: name, Members: members, Sealed: by.Sealed, Severity: sev})
	}
	return boundaries, nil
}

// parseRefs compiles a list of allow/deny entries into refs. subject is the
// already-formatted owner named in error messages (e.g. `component "api"` or
// `top-level deny`), so the same routine serves both component rules and the
// module-wide deny list. aliases maps each alias name to the refs it expands to
// (resolved once by parseAliases).
func parseRefs(subject string, entries []string, known map[string]bool, aliases map[string][]core.Ref) ([]core.Ref, error) {
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
			// An alias expands to the components and/or external-module prefixes
			// it names, each already resolved to a ref by parseAliases.
			if expanded, ok := aliases[e]; ok {
				refs = append(refs, expanded...)
				continue
			}
			if known[e] {
				refs = append(refs, core.Ref{Kind: core.RefComponent, Name: e})
				continue
			}
			// A ref that is neither a component nor an alias, but looks like an
			// import path, restricts a specific external module (depguard-style).
			if strings.ContainsAny(e, "/.") {
				refs = append(refs, core.Ref{Kind: core.RefExternalModule, Name: e})
				continue
			}
			return nil, fmt.Errorf("%s refers to unknown component or alias %q", subject, e)
		}
	}
	return refs, nil
}
