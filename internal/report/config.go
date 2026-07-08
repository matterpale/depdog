package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// RuleSet prints the compiled configuration for debugging: the default stance
// and options, then each component with its patterns, inferred stance and rule.
// Output is plain text and deterministic (components are already sorted).
func RuleSet(w io.Writer, rs *core.RuleSet) error {
	var b strings.Builder
	fmt.Fprintf(&b, "default:    %s\n", policyName(rs.Policy))
	fmt.Fprintf(&b, "test_files: %s\n", testFilesName(rs.TestFiles))
	if len(rs.Skip) > 0 {
		fmt.Fprintf(&b, "skip:       %s\n", strings.Join(rs.Skip, ", "))
	}

	b.WriteString("\ncomponents:\n")
	nameW := 0
	for _, c := range rs.Components {
		if len(c.Name) > nameW {
			nameW = len(c.Name)
		}
	}
	for _, c := range rs.Components {
		fmt.Fprintf(&b, "  %-*s  %s\n", nameW, c.Name, strings.Join(c.Patterns, ", "))
		fmt.Fprintf(&b, "  %-*s  stance: %s\n", nameW, "", stanceName(rs.Stance(c.Name)))
		if r, ok := rs.Rules[c.Name]; ok {
			if len(r.Allow) > 0 {
				fmt.Fprintf(&b, "  %-*s  allow:  %s\n", nameW, "", refList(r.Allow))
			}
			if len(r.Deny) > 0 {
				fmt.Fprintf(&b, "  %-*s  deny:   %s\n", nameW, "", refList(r.Deny))
			}
		}
	}

	if len(rs.Boundaries) > 0 {
		b.WriteString("\nboundaries:\n")
		// Label width covers the boundary name plus its " (sealed)" suffix so
		// the member lists align. Boundaries are already sorted by name.
		labelW := 0
		for _, bd := range rs.Boundaries {
			if w := len(boundaryLabel(bd)); w > labelW {
				labelW = w
			}
		}
		for _, bd := range rs.Boundaries {
			labels := make([]string, len(bd.Members))
			for i, m := range bd.Members {
				labels[i] = m.Label
			}
			fmt.Fprintf(&b, "  %-*s  %s\n", labelW, boundaryLabel(bd), strings.Join(labels, ", "))
		}
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// boundaryLabel renders a boundary's name with a " (sealed)" suffix when the
// one-way wall is on, matching the (sealed) marker used in explain and text.
func boundaryLabel(b core.Boundary) string {
	if b.Sealed {
		return b.Name + " (sealed)"
	}
	return b.Name
}

func testFilesName(m core.TestFileMode) string {
	switch m {
	case core.TestSameRules:
		return "same-rules"
	case core.TestRelaxed:
		return "relaxed"
	default:
		return "hybrid"
	}
}
