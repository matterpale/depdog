package config

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SetComponentRule edits one component's allow/deny lists in an existing
// depdog.yaml so target lands at the requested verdict, returning the new file
// bytes. Only the component's own line is rewritten; every other line —
// comments, blank lines, alignment — is preserved verbatim, and the result is
// validated with Parse before it is returned.
//
// verdict is "allow", "deny", or "default": allow adds target to the
// component's allow list and drops it from deny, deny is the mirror, and default
// removes target from both (falling back to the component's stance). An
// allow/deny list that becomes empty is dropped; a missing one is created. A
// no-op edit returns the input unchanged.
//
// It refuses, naming the fix, on files a precise splice can't safely edit:
// anchors/aliases, a flow-style components mapping, an unknown component, or a
// component whose value spans more than one line.
func SetComponentRule(data []byte, component, target, verdict string) ([]byte, error) {
	return editComponentLine(data, component, func(val *yaml.Node) bool {
		return applyVerdict(val, target, verdict)
	})
}

// SetComponentPath rewrites a component's `path` to the given pattern(s) — a
// scalar for one, a flow sequence for several — with the same single-line,
// comment-preserving, Parse-validated splice SetComponentRule uses.
func SetComponentPath(data []byte, component string, patterns []string) ([]byte, error) {
	if len(patterns) == 0 {
		return nil, errors.New("a component needs at least one path pattern")
	}
	return editComponentLine(data, component, func(val *yaml.Node) bool {
		return setPath(val, patterns)
	})
}

// editComponentLine locates the single-line flow mapping for component, applies
// mutate to it, and splices the re-encoded line back — preserving every other
// line and the component's trailing comment — validating the result with Parse.
// mutate reports whether it changed anything (a no-op returns the input as-is).
// It refuses files a precise splice can't safely edit: anchors/aliases, a
// flow-style components mapping, an unknown component, or a multi-line value.
func editComponentLine(data []byte, component string, mutate func(val *yaml.Node) bool) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the config is not a YAML mapping — edit it by hand")
	}
	if hasAnchor(&doc) {
		return nil, errors.New("the config uses YAML anchors or aliases, which an edit could corrupt — edit it by hand")
	}
	root := doc.Content[0]

	_, comps := mappingPair(root, "components")
	if comps == nil || comps.Kind != yaml.MappingNode || comps.Style&yaml.FlowStyle != 0 {
		return nil, errors.New(`the "components" mapping is missing or in flow style — edit it by hand`)
	}
	key, val := mappingPair(comps, component)
	if key == nil {
		return nil, fmt.Errorf("component %q is not in the config", component)
	}
	if val.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("component %q has an unexpected shape — edit it by hand", component)
	}
	if val.Line != key.Line || endLine(val) != key.Line {
		return nil, fmt.Errorf("component %q spans multiple lines — edit it by hand", component)
	}

	if !mutate(val) {
		return data, nil // nothing to change
	}

	clearComments(val) // the trailing comment is preserved textually, below
	body, err := encodeFlowMapping(val)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	li := key.Line - 1
	if li < 0 || li >= len(lines) {
		return nil, fmt.Errorf("component %q line %d is out of range", component, key.Line)
	}
	orig := lines[li]
	col := byteOffset(orig, val.Column) // yaml columns count characters, not bytes
	if col >= len(orig) {
		return nil, fmt.Errorf("cannot locate component %q value on its line", component)
	}
	prefix := orig[:col] // "  name:" plus any alignment padding
	comment := trailingComment(orig[col:])
	lines[li] = prefix + body + comment

	out := []byte(strings.Join(lines, "\n"))
	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("the edit produced an invalid config: %w", err)
	}
	return out, nil
}

// applyVerdict mutates the component value mapping so target sits at verdict.
// Returns whether anything actually changed.
func applyVerdict(val *yaml.Node, target, verdict string) bool {
	switch verdict {
	case "allow":
		removed := seqRemove(val, "deny", target)
		added := seqAdd(val, "allow", target)
		return removed || added
	case "deny":
		removed := seqRemove(val, "allow", target)
		added := seqAdd(val, "deny", target)
		return removed || added
	default: // "default"
		a := seqRemove(val, "allow", target)
		d := seqRemove(val, "deny", target)
		return a || d
	}
}

// seqAdd ensures target is in the component's `key` (allow|deny) flow sequence,
// creating the key when absent. Returns whether it changed anything.
func seqAdd(val *yaml.Node, key, target string) bool {
	if _, seq := mappingPair(val, key); seq != nil {
		for _, n := range seq.Content {
			if n.Value == target {
				return false
			}
		}
		seq.Content = append(seq.Content, scalarNode(target))
		return true
	}
	val.Content = append(val.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle, Content: []*yaml.Node{scalarNode(target)}})
	return true
}

// seqRemove drops target from the component's `key` sequence, removing the key
// entirely when its sequence empties. Returns whether it changed anything.
func seqRemove(val *yaml.Node, key, target string) bool {
	ki := mappingKeyIndex(val, key)
	if ki < 0 {
		return false
	}
	seq := val.Content[ki+1]
	kept := seq.Content[:0:0]
	removed := false
	for _, n := range seq.Content {
		if n.Value == target {
			removed = true
			continue
		}
		kept = append(kept, n)
	}
	if !removed {
		return false
	}
	if len(kept) == 0 {
		val.Content = append(val.Content[:ki], val.Content[ki+2:]...)
	} else {
		seq.Content = kept
	}
	return true
}

// setPath replaces the component's `path` value with patterns (a scalar for one,
// a flow sequence for several), creating the key if it is somehow absent.
// Returns whether the path actually changed.
func setPath(val *yaml.Node, patterns []string) bool {
	ki := mappingKeyIndex(val, "path")
	if ki < 0 {
		val.Content = append([]*yaml.Node{{Kind: yaml.ScalarNode, Value: "path"}, pathNode(patterns)}, val.Content...)
		return true
	}
	if samePath(val.Content[ki+1], patterns) {
		return false
	}
	val.Content[ki+1] = pathNode(patterns)
	return true
}

// pathNode renders path patterns as a quoted scalar (one) or a quoted flow
// sequence (several), matching the style `depdog init` generates.
func pathNode(patterns []string) *yaml.Node {
	if len(patterns) == 1 {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: patterns[0], Style: yaml.DoubleQuotedStyle}
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, p := range patterns {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: p, Style: yaml.DoubleQuotedStyle})
	}
	return seq
}

// samePath reports whether a path value node already holds exactly patterns.
func samePath(node *yaml.Node, patterns []string) bool {
	var cur []string
	if node.Kind == yaml.SequenceNode {
		for _, n := range node.Content {
			cur = append(cur, n.Value)
		}
	} else {
		cur = []string{node.Value}
	}
	if len(cur) != len(patterns) {
		return false
	}
	for i := range cur {
		if cur[i] != patterns[i] {
			return false
		}
	}
	return true
}

func mappingKeyIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Kind == yaml.ScalarNode && m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// scalarNode builds a ref scalar, quoting names that are not safe as bare YAML
// (e.g. "*" or a module path).
func scalarNode(v string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Value: v}
	if !bareKey.MatchString(v) {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// clearComments strips head/line/foot comments from a node subtree so a
// re-encode never duplicates a comment that is preserved textually elsewhere.
func clearComments(n *yaml.Node) {
	n.HeadComment, n.LineComment, n.FootComment = "", "", ""
	for _, c := range n.Content {
		clearComments(c)
	}
}

// encodeFlowMapping renders a mapping node as a single flow-style line.
func encodeFlowMapping(n *yaml.Node) (string, error) {
	n.Style = yaml.FlowStyle
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return "", err
	}
	_ = enc.Close()
	return strings.TrimRight(buf.String(), "\n"), nil
}

// trailingComment returns the comment suffix (leading spaces and all) that
// follows the flow mapping at the start of rest, or "". It walks to the mapping's
// matching close brace, honoring quotes, and returns whatever comes after.
func trailingComment(rest string) string {
	depth := 0
	inSingle, inDouble := false, false
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case inDouble:
			switch c {
			case '\\':
				i++
			case '"':
				inDouble = false
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == '{', c == '[':
			depth++
		case c == '}', c == ']':
			depth--
			if depth == 0 && c == '}' {
				return rest[i+1:]
			}
		}
	}
	return ""
}
