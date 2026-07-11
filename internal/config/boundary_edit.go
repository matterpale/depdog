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

// editBoundaryMembers locates a boundary's single-line flow member sequence,
// applies mutate, and splices the re-encoded `[...]` back into its line. It
// refuses anchors, an unknown boundary, a non-flow/multi-line member list, or a
// boundary shape it doesn't recognise.
func editBoundaryMembers(data []byte, boundary string, mutate func(seq *yaml.Node) bool) ([]byte, error) {
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
	col := seq.Column - 1
	if col < 0 || col >= len(orig) || orig[col] != '[' {
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
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}
