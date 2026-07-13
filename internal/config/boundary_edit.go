package config

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// AddBoundaryMember adds member to a boundary's member list; RemoveBoundaryMember
// drops it. Both rewrite only the boundary's member `[...]` on its own line
// (shorthand `name: [...]` or expanded `members: [...]`), preserving every other
// byte and validating the result with Parse. A no-op returns the input as-is.
func AddBoundaryMember(data []byte, boundary, member string) ([]byte, error) {
	return editBoundaryMembers(data, boundary, func(seq *yaml.Node) bool {
		return addMember(seq, member)
	})
}

func RemoveBoundaryMember(data []byte, boundary, member string) ([]byte, error) {
	return editBoundaryMembers(data, boundary, func(seq *yaml.Node) bool {
		return removeMember(seq, member)
	})
}

// findBoundary parses the config and returns the named boundary's value node
// (the shorthand sequence or the expanded mapping). It refuses anchors, a
// non-mapping config, a missing boundaries block, or an unknown boundary — the
// shared prologue of every boundary splice.
func findBoundary(data []byte, boundary string) (*yaml.Node, error) {
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

	_, bounds := mappingPair(root, "boundaries")
	if bounds == nil || bounds.Kind != yaml.MappingNode {
		return nil, errors.New(`the config has no "boundaries" mapping — edit it by hand`)
	}
	bkey, bval := mappingPair(bounds, boundary)
	if bkey == nil {
		return nil, fmt.Errorf("boundary %q is not in the config", boundary)
	}
	return bval, nil
}

// editBoundaryMembers locates a boundary's single-line flow member sequence,
// applies mutate, and splices the re-encoded `[...]` back into its line. It
// refuses anchors, an unknown boundary, a non-flow/multi-line member list, or a
// boundary shape it doesn't recognise.
func editBoundaryMembers(data []byte, boundary string, mutate func(seq *yaml.Node) bool) ([]byte, error) {
	bval, err := findBoundary(data, boundary)
	if err != nil {
		return nil, err
	}

	var seq *yaml.Node
	switch bval.Kind {
	case yaml.SequenceNode: // shorthand: name: [a, b, c]
		seq = bval
	case yaml.MappingNode: // expanded: name: { members: [...], sealed: … }
		if _, seq = mappingPair(bval, "members"); seq == nil {
			return nil, fmt.Errorf("boundary %q has no members list — edit it by hand", boundary)
		}
	default:
		return nil, fmt.Errorf("boundary %q has an unexpected shape — edit it by hand", boundary)
	}
	if seq.Kind != yaml.SequenceNode || endLine(seq) != seq.Line {
		return nil, fmt.Errorf("boundary %q members are not a single-line list — edit it by hand", boundary)
	}

	if !mutate(seq) {
		return data, nil
	}

	clearComments(seq)
	body, err := encodeFlowSeq(seq)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	li := seq.Line - 1
	if li < 0 || li >= len(lines) {
		return nil, fmt.Errorf("boundary %q line %d is out of range", boundary, seq.Line)
	}
	orig := lines[li]
	col := byteOffset(orig, seq.Column) // yaml columns count characters, not bytes
	if col >= len(orig) || orig[col] != '[' {
		return nil, fmt.Errorf("cannot locate boundary %q members on its line", boundary)
	}
	end, ok := matchBracket(orig, col)
	if !ok {
		return nil, fmt.Errorf("boundary %q member list is malformed", boundary)
	}
	lines[li] = orig[:col] + body + orig[end:]

	out := []byte(strings.Join(lines, "\n"))
	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("the edit produced an invalid config: %w", err)
	}
	return out, nil
}

// SetBoundarySealed sets a boundary's sealed flag with the smallest edit that
// keeps the file's shape: an existing `sealed:` scalar is rewritten in place;
// a shorthand `name: [a, b]` being sealed becomes the expanded single-line
// `name: { members: [a, b], sealed: true }` (the member text is kept
// verbatim); a single-line flow mapping gains `, sealed: true` before its
// closing brace; a block mapping gains a `sealed: true` line after its last
// key. Unsealing a boundary with no `sealed:` key is a no-op (absent means
// false), as is setting the flag it already has. The result is validated with
// Parse, like every other splice.
func SetBoundarySealed(data []byte, boundary string, sealed bool) ([]byte, error) {
	bval, err := findBoundary(data, boundary)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	spliceLine := func(li int, edit func(orig string) (string, error)) ([]byte, error) {
		if li < 0 || li >= len(lines) {
			return nil, fmt.Errorf("boundary %q line %d is out of range", boundary, li+1)
		}
		next, err := edit(lines[li])
		if err != nil {
			return nil, err
		}
		lines[li] = next
		out := []byte(strings.Join(lines, "\n"))
		if _, err := Parse(out); err != nil {
			return nil, fmt.Errorf("the edit produced an invalid config: %w", err)
		}
		return out, nil
	}

	switch bval.Kind {
	case yaml.SequenceNode: // shorthand: name: [a, b] — sealed is implicitly false
		if !sealed {
			return data, nil
		}
		if endLine(bval) != bval.Line {
			return nil, fmt.Errorf("boundary %q members are not a single-line list — edit it by hand", boundary)
		}
		return spliceLine(bval.Line-1, func(orig string) (string, error) {
			col := byteOffset(orig, bval.Column)
			if col >= len(orig) || orig[col] != '[' {
				return "", fmt.Errorf("cannot locate boundary %q members on its line", boundary)
			}
			end, ok := matchBracket(orig, col)
			if !ok {
				return "", fmt.Errorf("boundary %q member list is malformed", boundary)
			}
			return orig[:col] + "{ members: " + orig[col:end] + ", sealed: true }" + orig[end:], nil
		})

	case yaml.MappingNode:
		_, sval := mappingPair(bval, "sealed")
		if sval != nil {
			if sval.Kind != yaml.ScalarNode || sval.Style != 0 {
				return nil, fmt.Errorf("boundary %q has a sealed value this edit cannot rewrite — edit it by hand", boundary)
			}
			if parseYAMLBool(sval.Value) == sealed {
				return data, nil
			}
			want := "false"
			if sealed {
				want = "true"
			}
			return spliceLine(sval.Line-1, func(orig string) (string, error) {
				col := byteOffset(orig, sval.Column)
				if col+len(sval.Value) > len(orig) || orig[col:col+len(sval.Value)] != sval.Value {
					return "", fmt.Errorf("cannot locate boundary %q sealed value on its line", boundary)
				}
				return orig[:col] + want + orig[col+len(sval.Value):], nil
			})
		}
		if !sealed {
			return data, nil // no sealed key means false already
		}
		if bval.Style&yaml.FlowStyle != 0 { // flow mapping: name: { members: [...] }
			if endLine(bval) != bval.Line {
				return nil, fmt.Errorf("boundary %q spans multiple lines in flow style — edit it by hand", boundary)
			}
			return spliceLine(bval.Line-1, func(orig string) (string, error) {
				col := byteOffset(orig, bval.Column)
				if col >= len(orig) || orig[col] != '{' {
					return "", fmt.Errorf("cannot locate boundary %q mapping on its line", boundary)
				}
				end, ok := matchDelim(orig, col, '{', '}')
				if !ok {
					return "", fmt.Errorf("boundary %q mapping is malformed", boundary)
				}
				head := strings.TrimRight(orig[col:end-1], " ")
				return orig[:col] + head + ", sealed: true }" + orig[end:], nil
			})
		}
		// Block mapping: insert a sealed line after the last line the mapping
		// occupies, indented like its first key.
		if len(bval.Content) == 0 {
			return nil, fmt.Errorf("boundary %q has an unexpected shape — edit it by hand", boundary)
		}
		k0 := bval.Content[0]
		if k0.Line-1 < 0 || k0.Line-1 >= len(lines) {
			return nil, fmt.Errorf("boundary %q line %d is out of range", boundary, k0.Line)
		}
		indent := lines[k0.Line-1][:byteOffset(lines[k0.Line-1], k0.Column)]
		if strings.TrimSpace(indent) != "" {
			return nil, fmt.Errorf("boundary %q has an unexpected layout — edit it by hand", boundary)
		}
		at := endLine(bval) // insert after the mapping's last line (1-based → index)
		if at > len(lines) {
			return nil, fmt.Errorf("boundary %q line %d is out of range", boundary, at)
		}
		lines = append(lines[:at], append([]string{indent + "sealed: true"}, lines[at:]...)...)
		out := []byte(strings.Join(lines, "\n"))
		if _, err := Parse(out); err != nil {
			return nil, fmt.Errorf("the edit produced an invalid config: %w", err)
		}
		return out, nil

	default:
		return nil, fmt.Errorf("boundary %q has an unexpected shape — edit it by hand", boundary)
	}
}

// parseYAMLBool reads the spellings yaml.v3 resolves to a bool in a config
// that already passed Parse.
func parseYAMLBool(s string) bool {
	switch s {
	case "true", "True", "TRUE":
		return true
	default:
		return false
	}
}

func addMember(seq *yaml.Node, member string) bool {
	for _, n := range seq.Content {
		if n.Value == member {
			return false
		}
	}
	seq.Content = append(seq.Content, scalarNode(member))
	return true
}

func removeMember(seq *yaml.Node, member string) bool {
	kept := seq.Content[:0:0]
	removed := false
	for _, n := range seq.Content {
		if n.Value == member {
			removed = true
			continue
		}
		kept = append(kept, n)
	}
	if removed {
		seq.Content = kept
	}
	return removed
}

// encodeFlowSeq renders a sequence node as a single flow-style line `[a, b, c]`.
func encodeFlowSeq(n *yaml.Node) (string, error) {
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

// matchBracket returns the index just past the `]` matching the `[` at open,
// honoring quotes, and whether it was found.
func matchBracket(s string, open int) (int, bool) {
	return matchDelim(s, open, '[', ']')
}

// matchDelim returns the index just past the closer matching the opener at
// open, honoring quotes, and whether it was found.
func matchDelim(s string, open int, oc, cc byte) (int, bool) {
	depth := 0
	inSingle, inDouble := false, false
	for i := open; i < len(s); i++ {
		c := s[i]
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
		case c == oc:
			depth++
		case c == cc:
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}
