package wizard

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// Config is a fully resolved depdog.yaml, ready to Marshal. Suggest produces
// it; the interactive flow may drop components from it before writing.
type Config struct {
	Preset     string      // preset name, recorded in the header comment
	Policy     string      // PolicyDeny or PolicyAllow
	Components []Component // in render order
}

// Suggest reconciles a preset with what ScanModule actually found and the
// chosen policy stance, producing a Config that always round-trips through
// config.Parse:
//
//   - Preset components whose patterns match a real directory are kept; ones
//     with no directory behind them are dropped (unless the module has no
//     package dirs at all, in which case the whole preset is kept as a
//     scaffold).
//   - Every remaining directory that no kept component claims is grouped into a
//     proposed component named after its directory.
//   - Refs to dropped components are scrubbed from the surviving rules so the
//     result never references an unknown component.
func Suggest(p Preset, s Scan, policy string) Config {
	if policy == "" {
		policy = PolicyDeny
	}
	pc := p.clone()

	order := make(map[string]int, len(pc.Components))
	for i, c := range pc.Components {
		order[c.Name] = i
	}

	var kept []Component
	if len(s.Dirs) == 0 {
		// An empty module: hand back the preset untouched as a scaffold.
		kept = pc.Components
	} else {
		for _, c := range pc.Components {
			if componentClaims(c, s.Dirs) {
				kept = append(kept, c)
			}
		}
	}

	used := make(map[string]bool, len(kept))
	for _, c := range kept {
		used[c.Name] = true
	}

	all := append(kept, proposeComponents(kept, s, policy, used)...)
	if len(all) == 0 {
		all = []Component{starterComponent()}
	}

	// Nothing may reference a component we did not keep, or config.Parse
	// rejects the file.
	names := make(map[string]bool, len(all))
	for _, c := range all {
		names[c.Name] = true
	}
	for i := range all {
		all[i].Allow = scrubRefs(all[i].Allow, names)
		all[i].Deny = scrubRefs(all[i].Deny, names)
	}

	sortComponents(all, order)
	return Config{Preset: p.Name, Policy: policy, Components: all}
}

// Keep narrows a Config to the named components in their existing order,
// scrubbing any rule refs to dropped components so the result still
// round-trips. The interactive review uses it after the user unchecks
// components.
func (c Config) Keep(names []string) Config {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	var kept []Component
	for _, comp := range c.Components {
		if want[comp.Name] {
			kept = append(kept, comp)
		}
	}
	survive := make(map[string]bool, len(kept))
	for _, comp := range kept {
		survive[comp.Name] = true
	}
	for i := range kept {
		kept[i].Allow = scrubRefs(kept[i].Allow, survive)
		kept[i].Deny = scrubRefs(kept[i].Deny, survive)
	}
	out := c
	out.Components = kept
	return out
}

// proposeComponents turns every scanned directory that no kept component
// claims into a component, grouped by top-level package area.
func proposeComponents(kept []Component, s Scan, policy string, used map[string]bool) []Component {
	members := map[string][]string{}
	var groups []string
	for _, dir := range s.Dirs {
		if dirClaimed(kept, dir) {
			continue
		}
		gk := groupKey(dir)
		if _, ok := members[gk]; !ok {
			groups = append(groups, gk)
		}
		members[gk] = append(members[gk], dir)
	}
	sort.Strings(groups)

	out := make([]Component, 0, len(groups))
	for _, gk := range groups {
		name := proposedName(gk, used)
		used[name] = true
		c := Component{
			Name:     name,
			Patterns: []string{gk + "/**"},
			Comment:  "proposed from directory scan — review this rule",
		}
		if policy == PolicyDeny {
			if gk == "cmd" {
				c.Allow = []string{"*"} // an entrypoint wires everything
			} else {
				c.Allow = []string{"std", "external"}
			}
		}
		out = append(out, c)
	}
	return out
}

// starterComponent is the single permissive component emitted for the flat
// preset on an empty module, so even an empty scan yields a valid, editable
// file.
func starterComponent() Component {
	return Component{
		Name:     "app",
		Patterns: []string{"internal/**"},
		Allow:    []string{"std", "external"},
		Comment:  "starter component — rename and split as your architecture emerges",
	}
}

// groupKey collapses a package dir to the area that becomes one component:
// the first two segments under internal/ or pkg/, otherwise the first segment.
func groupKey(dir string) string {
	segs := strings.Split(dir, "/")
	if (segs[0] == "internal" || segs[0] == "pkg") && len(segs) >= 2 {
		return segs[0] + "/" + segs[1]
	}
	return segs[0]
}

// proposedName derives a component name from a group key, avoiding collisions
// with names already in use and with the reserved rule keywords.
func proposedName(groupKey string, used map[string]bool) string {
	base := groupKey
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	name := base
	if used[name] || isSpecial(name) {
		name = strings.ReplaceAll(groupKey, "/", "-")
	}
	for i := 2; used[name] || isSpecial(name); i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	return name
}

// componentClaims reports whether any of the component's patterns match at
// least one scanned directory.
func componentClaims(c Component, dirs []string) bool {
	for _, dir := range dirs {
		if dirClaimed([]Component{c}, dir) {
			return true
		}
	}
	return false
}

// dirClaimed reports whether any component's pattern matches dir.
func dirClaimed(cs []Component, dir string) bool {
	for _, c := range cs {
		for _, pat := range c.Patterns {
			if ok, err := core.MatchPattern(pat, dir); err == nil && ok {
				return true
			}
		}
	}
	return false
}

// scrubRefs keeps only refs that are specials or names of surviving
// components.
func scrubRefs(refs []string, names map[string]bool) []string {
	if len(refs) == 0 {
		return refs
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if isSpecial(r) || names[r] {
			out = append(out, r)
		}
	}
	return out
}

func isSpecial(ref string) bool {
	switch ref {
	case "*", "std", "external", "unassigned":
		return true
	default:
		return false
	}
}
