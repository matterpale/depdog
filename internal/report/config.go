package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// RuleSet prints the compiled configuration for debugging: the policy and
// options, then each component with its patterns, inferred stance and rule.
// Output is plain text and deterministic (components are already sorted).
func RuleSet(w io.Writer, rs *core.RuleSet) error {
	var b strings.Builder
	fmt.Fprintf(&b, "policy:     %s\n", policyName(rs.Policy))
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
	_, err := io.WriteString(w, b.String())
	return err
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
