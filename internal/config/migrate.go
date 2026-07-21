package config

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// legacyError detects a version-1 config (separate `components:` and `rules:`
// blocks, or components whose value is a bare pattern list) and returns a
// migration error written from the user's own config, showing the exact
// rewrite. It returns nil for a version-2 config or anything it can't confidently
// classify, letting the normal decoder report the real problem.
func legacyError(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil // a genuine parse error; the main decoder reports it.
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	root := doc.Content[0]

	var versionNode, componentsNode, rulesNode *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "version":
			versionNode = root.Content[i+1]
		case "components":
			componentsNode = root.Content[i+1]
		case "rules":
			rulesNode = root.Content[i+1]
		}
	}

	// Signals of a v1 config: an explicit version: 1, a separate rules block
	// (v2 has none), or a component whose value is a bare pattern sequence.
	legacy := (versionNode != nil && versionNode.Value == "1") || rulesNode != nil
	if !legacy && componentsNode != nil && componentsNode.Kind == yaml.MappingNode {
		for i := 1; i < len(componentsNode.Content); i += 2 {
			if componentsNode.Content[i].Kind == yaml.SequenceNode {
				legacy = true // old `name: [patterns]` shape.
				break
			}
		}
	}
	if !legacy {
		return nil
	}

	name, path := firstComponent(componentsNode)
	rule := ruleFor(rulesNode, name)
	if name == "" {
		name, path, rule = "domain", `"internal/domain/**"`, "allow: [std]"
	}
	body := "{ path: " + path
	if rule != "" {
		body += ", " + rule
	}
	body += " }"

	return fmt.Errorf("this is a version 1 config (separate `components` and `rules` blocks). "+
		"depdog now uses one `components` block with each rule inline: move every component's "+
		"allow/deny onto its entry beside `path`, delete the `rules` block, and set `version: 2` — "+
		"e.g. `%s: %s`", yamlKey(name), body)
}

// renamedFieldError catches a config that still uses the old top-level `policy`
// key (renamed to `default` in v2, with its default flipped from deny to allow),
// pointing at the rename rather than letting KnownFields emit "field policy not
// found". Returns nil when there is no such key.
//
// The `groups` -> `aliases` rename is deliberately NOT handled here: `groups`
// shipped in v1.0.0, so per docs/compatibility.md it must keep working within
// the 1.x line. Parse accepts it as a deprecated synonym of `aliases` and emits
// an advisory instead of an error; the hard removal waits for a major bump.
func renamedFieldError(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil // a real parse error; the main decoder reports it.
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	root := doc.Content[0]
	var policy *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "policy" {
			policy = root.Content[i+1]
		}
	}
	if policy == nil {
		return nil
	}
	stance := policy.Value
	if stance != "allow" && stance != "deny" {
		stance = "deny"
	}
	return fmt.Errorf("the `policy` field was renamed to `default` in config v2 — write `default: %s`. "+
		"Note the default also flipped: a component with no allow/deny rule now imports anything (was: nothing); "+
		"omit `default` for that open behavior, or set `default: deny` for the strict whitelist stance", stance)
}

// firstComponent returns the first component's name and its rendered path value
// (a quoted scalar for a single pattern, a flow list for several).
func firstComponent(components *yaml.Node) (name, path string) {
	if components == nil || components.Kind != yaml.MappingNode || len(components.Content) < 2 {
		return "", ""
	}
	name = components.Content[0].Value
	val := components.Content[1]
	var patterns []string
	switch val.Kind {
	case yaml.ScalarNode:
		patterns = []string{val.Value}
	case yaml.SequenceNode:
		for _, p := range val.Content {
			patterns = append(patterns, p.Value)
		}
	case yaml.MappingNode:
		// Already a v2-shaped entry; pull its path for the example.
		for i := 0; i+1 < len(val.Content); i += 2 {
			if val.Content[i].Value == "path" {
				pv := val.Content[i+1]
				if pv.Kind == yaml.SequenceNode {
					for _, p := range pv.Content {
						patterns = append(patterns, p.Value)
					}
				} else {
					patterns = []string{pv.Value}
				}
			}
		}
	}
	return name, renderPathValue(patterns)
}

// ruleFor renders the flow rule body for name from a legacy rules block, e.g.
// `allow: [std, external]`, or "" when there is none.
func ruleFor(rules *yaml.Node, name string) string {
	if rules == nil || rules.Kind != yaml.MappingNode || name == "" {
		return ""
	}
	for i := 0; i+1 < len(rules.Content); i += 2 {
		if rules.Content[i].Value != name {
			continue
		}
		body := rules.Content[i+1]
		if body.Kind != yaml.MappingNode {
			return ""
		}
		var parts []string
		for j := 0; j+1 < len(body.Content); j += 2 {
			key := body.Content[j].Value
			var refs []string
			for _, r := range body.Content[j+1].Content {
				refs = append(refs, r.Value)
			}
			parts = append(parts, key+": "+renderRefList(refs))
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

// renderPathValue renders one path field: a quoted scalar for a single pattern,
// a flow list for several.
func renderPathValue(patterns []string) string {
	switch len(patterns) {
	case 0:
		return `""`
	case 1:
		return strconv.Quote(patterns[0])
	default:
		return renderPatterns(patterns)
	}
}

// renderRefList renders allow/deny refs as a flow list; "*" is quoted so it
// stays a string.
func renderRefList(refs []string) string {
	out := make([]string, len(refs))
	for i, r := range refs {
		if r == "*" {
			out[i] = `"*"`
		} else {
			out[i] = r
		}
	}
	return "[" + strings.Join(out, ", ") + "]"
}
