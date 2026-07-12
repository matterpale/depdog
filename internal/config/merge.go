package config

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// MergeComponent is one component `depdog init --merge` adds to an existing
// depdog.yaml.
type MergeComponent struct {
	Name     string
	Patterns []string
	Comment  string // optional trailing comment on the component line
	Rule     string // optional inline rule, e.g. "allow: [std, external]" (no braces)
}

// bareKey is the charset a component name may use to stay an unquoted YAML key
// (mirrors the wizard's name validation).
var bareKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// MergeComponents inserts the given components into an existing depdog.yaml
// without disturbing anything else. The original bytes are kept verbatim: the
// parsed yaml.Node tree only locates the end of the `components:` block mapping,
// and new `name: { path: [...], <rule> }` lines are spliced in after it, so
// every existing comment, blank line and alignment survives. New entries are
// sorted by name and aligned to the existing value column when the block is
// consistently aligned.
//
// It refuses, with an error naming the fix, when a splice could corrupt the
// file: anchors or aliases anywhere, or a flow-style ({...}) components mapping.
// Callers must validate the result with Parse before writing it.
func MergeComponents(data []byte, add []MergeComponent) ([]byte, error) {
	if len(add) == 0 {
		return data, nil
	}
	add = append([]MergeComponent(nil), add...)
	sort.Slice(add, func(i, j int) bool { return add[i].Name < add[j].Name })

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the config is not a YAML mapping — fix the file before merging")
	}
	if hasAnchor(&doc) {
		return nil, errors.New("the config uses YAML anchors or aliases, which a merge could corrupt — add the new components by hand")
	}
	root := doc.Content[0]

	_, comps := mappingPair(root, "components")
	if comps == nil {
		return nil, errors.New(`the config has no "components" mapping — fix the file before merging`)
	}
	if comps.Kind != yaml.MappingNode || comps.Style&yaml.FlowStyle != 0 || len(comps.Content) == 0 {
		return nil, errors.New(`the "components" mapping is in flow style ({...}) or empty — rewrite it in block form (one "name: { path: ... }" line per component), then rerun the merge`)
	}
	for _, c := range add {
		if k, _ := mappingPair(comps, c.Name); k != nil {
			return nil, fmt.Errorf("component %q already exists in the config — merge only adds new components", c.Name)
		}
	}

	lines := strings.Split(string(data), "\n")
	indent := strings.Repeat(" ", indentOf(comps))
	col := valueColumn(comps)
	newLines := make([]string, len(add))
	for i, c := range add {
		newLines[i] = renderEntry(indent, col, c.Name, mergeEntryBody(c), c.Comment)
	}

	at := endLine(comps)
	if at > len(lines) {
		at = len(lines)
	}
	lines = append(lines[:at], append(append([]string(nil), newLines...), lines[at:]...)...)
	return []byte(strings.Join(lines, "\n")), nil
}

// mergeEntryBody renders a merged component's `{ path: <patterns>, <rule> }`
// value, or `{ path: <patterns> }` when the component carries no rule.
func mergeEntryBody(c MergeComponent) string {
	body := "{ path: " + renderPathValue(c.Patterns)
	if c.Rule != "" {
		body += ", " + c.Rule
	}
	return body + " }"
}

// DeclaredNames lists every name the raw config's components and groups
// mappings declare, sorted. The merge uses it to pick collision-free names for
// new components without compiling the whole file.
func DeclaredNames(data []byte) ([]string, error) {
	var f struct {
		Components map[string]yaml.Node `yaml:"components"`
		Groups     map[string]yaml.Node `yaml:"groups"`
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(f.Components)+len(f.Groups))
	for n := range f.Components {
		names = append(names, n)
	}
	for n := range f.Groups {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// mappingPair finds a key by name in a block or flow mapping, returning its key
// and value nodes, or nil, nil.
func mappingPair(m *yaml.Node, key string) (k, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Kind == yaml.ScalarNode && m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

// hasAnchor reports whether any node in the tree declares an anchor or is an
// alias — features a textual splice must not touch.
func hasAnchor(n *yaml.Node) bool {
	if n.Anchor != "" || n.Kind == yaml.AliasNode {
		return true
	}
	for _, c := range n.Content {
		if hasAnchor(c) {
			return true
		}
	}
	return false
}

// endLine is the last physical line the node's subtree occupies. Literal and
// folded scalars span their content lines even though only the marker line is
// recorded on the node.
func endLine(n *yaml.Node) int {
	last := n.Line
	if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		if v := strings.TrimRight(n.Value, "\n"); v != "" {
			last += strings.Count(v, "\n") + 1
		}
	}
	for _, c := range n.Content {
		if l := endLine(c); l > last {
			last = l
		}
	}
	return last
}

// byteOffset converts a 1-based character column — the unit yaml.v3 reports in
// Node.Column — to a 0-based byte index into line. yaml counts columns in
// characters, so a line with multibyte runes before the column would misplace a
// raw byte splice without this translation. A column past the line's end clamps
// to len(line).
func byteOffset(line string, col int) int {
	i := 0
	for c := 1; c < col; c++ {
		if i >= len(line) {
			return len(line)
		}
		_, size := utf8.DecodeRuneInString(line[i:])
		i += size
	}
	return i
}

// indentOf is the indentation (in spaces) of a block mapping's keys.
func indentOf(m *yaml.Node) int {
	if len(m.Content) == 0 || m.Content[0].Column < 2 {
		return 2
	}
	return m.Content[0].Column - 1
}

// valueColumn reports the 1-based column all of a block mapping's values start
// at, or 0 when they are not consistently aligned (or not on their key's
// line). New entries pad to it so an aligned block stays aligned.
func valueColumn(m *yaml.Node) int {
	col := 0
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if v.Line != k.Line {
			return 0
		}
		if col == 0 {
			col = v.Column
		} else if v.Column != col {
			return 0
		}
	}
	return col
}

// renderEntry renders one "name: body" mapping line, padding the value to
// valueCol when the name fits, with an optional trailing comment.
func renderEntry(indent string, valueCol int, name, body, comment string) string {
	line := indent + yamlKey(name) + ":"
	if pad := valueCol - 1 - len(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	} else {
		line += " "
	}
	line += body
	if comment != "" {
		line += " # " + comment
	}
	return line
}

// yamlKey renders a component name as a YAML key, quoting it when it is not
// safe as a bare scalar.
func yamlKey(name string) string {
	if bareKey.MatchString(name) {
		return name
	}
	return strconv.Quote(name)
}

// renderPatterns renders patterns as a double-quoted flow sequence, matching
// the style `depdog init` generates.
func renderPatterns(patterns []string) string {
	out := make([]string, len(patterns))
	for i, p := range patterns {
		out[i] = strconv.Quote(p)
	}
	return "[" + strings.Join(out, ", ") + "]"
}
