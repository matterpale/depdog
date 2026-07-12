package config

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// RenameComponent renames a component and every reference to it — its own key,
// allow/deny refs in any component, group entries, and boundary members — in an
// existing depdog.yaml, returning the new bytes. Only the exact name tokens
// change; every other byte (comments, alignment, blank lines) is preserved, and
// the result is validated with Parse. Path globs are deliberately left alone, so
// a path that happens to equal the name is not touched.
//
// It refuses, naming the fix, when: newName already names a component or group;
// oldName is unknown; the file uses anchors/aliases; or a reference token is
// quoted or shifted (a positional splice can't safely rewrite it).
func RenameComponent(data []byte, oldName, newName string) ([]byte, error) {
	if oldName == newName {
		return data, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the config is not a YAML mapping — edit it by hand")
	}
	if hasAnchor(&doc) {
		return nil, errors.New("the config uses YAML anchors or aliases, which a rename could corrupt — edit it by hand")
	}
	root := doc.Content[0]

	_, comps := mappingPair(root, "components")
	if comps == nil || comps.Kind != yaml.MappingNode {
		return nil, errors.New(`the config has no "components" mapping — edit it by hand`)
	}
	if k, _ := mappingPair(comps, oldName); k == nil {
		return nil, fmt.Errorf("component %q is not in the config", oldName)
	}
	if names, err := DeclaredNames(data); err == nil && slices.Contains(names, newName) {
		return nil, fmt.Errorf("%q already names a component or group — pick another name", newName)
	}

	// Group the reference positions by line, then rewrite each line right-to-left
	// so replacing a token never invalidates an earlier token's column.
	byLine := map[int][]int{}
	for _, p := range renameRefs(root, oldName) {
		byLine[p.line] = append(byLine[p.line], p.col)
	}
	lines := strings.Split(string(data), "\n")
	for ln, cols := range byLine {
		li := ln - 1
		if li < 0 || li >= len(lines) {
			return nil, fmt.Errorf("a reference on line %d is out of range", ln)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(cols)))
		s := lines[li]
		for _, c := range cols {
			// Rewrite right-to-left, so each byte offset (yaml columns count
			// characters, not bytes) is measured against an unchanged prefix.
			start := byteOffset(s, c)
			end := start + len(oldName)
			if end > len(s) || s[start:end] != oldName {
				return nil, fmt.Errorf("a reference to %q on line %d is quoted or shifted — rename it by hand", oldName, ln)
			}
			s = s[:start] + newName + s[end:]
		}
		lines[li] = s
	}

	out := []byte(strings.Join(lines, "\n"))
	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("the rename produced an invalid config: %w", err)
	}
	return out, nil
}

type refPos struct{ line, col int }

// renameRefs collects the position of every scalar equal to name that is a
// component reference: the component's own key, allow/deny refs (in any
// component), group entries, and boundary members (shorthand list or expanded
// `members`). It never visits `path` values or other scalars, so a glob equal to
// the name is not collected.
func renameRefs(root *yaml.Node, name string) []refPos {
	var out []refPos
	add := func(n *yaml.Node) {
		if n.Kind == yaml.ScalarNode && n.Value == name {
			out = append(out, refPos{n.Line, n.Column})
		}
	}
	seq := func(n *yaml.Node) {
		if n == nil || n.Kind != yaml.SequenceNode {
			return
		}
		for _, c := range n.Content {
			add(c)
		}
	}

	if _, comps := mappingPair(root, "components"); comps != nil {
		for i := 0; i+1 < len(comps.Content); i += 2 {
			key, val := comps.Content[i], comps.Content[i+1]
			add(key)
			if val.Kind == yaml.MappingNode {
				_, a := mappingPair(val, "allow")
				_, d := mappingPair(val, "deny")
				seq(a)
				seq(d)
			}
		}
	}
	if _, groups := mappingPair(root, "groups"); groups != nil {
		for i := 1; i < len(groups.Content); i += 2 {
			seq(groups.Content[i])
		}
	}
	if _, bounds := mappingPair(root, "boundaries"); bounds != nil {
		for i := 1; i < len(bounds.Content); i += 2 {
			v := bounds.Content[i]
			switch v.Kind {
			case yaml.SequenceNode:
				seq(v)
			case yaml.MappingNode:
				_, members := mappingPair(v, "members")
				seq(members)
			}
		}
	}
	return out
}
