package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// RuleSet prints the compiled configuration for debugging: the active config
// path, the default stance and options, then each component with its patterns,
// inferred stance and rule, and any boundaries. It mirrors the TUI's Config
// tab — the path title, a key/value header, a per-component stance, and
// colored allow/deny refs — and is color-aware (color is "auto", "always" or
// "never"): piped output and NO_COLOR stay plain, a real terminal gets color.
// path is the module-relative config location ("" hides the title, keeping
// callers without one unchanged). Components and boundaries are already
// sorted, so output is deterministic.
func RuleSet(w io.Writer, rs *core.RuleSet, path, color string) error {
	st := newStyles(w, color)
	var b strings.Builder

	if path != "" {
		fmt.Fprintf(&b, "%s\n\n", st.rule.Render(path))
	}

	kv := func(k, v string) {
		fmt.Fprintf(&b, "%s  %s\n", st.pos.Render(fmt.Sprintf("%-10s", k)), v)
	}
	defVal, defHint := st.warn.Render("deny"), "rule-less components import nothing"
	if rs.Policy == core.PolicyAllow {
		defVal, defHint = st.good.Render("allow"), "rule-less components import anything"
	}
	kv("default", defVal+"  "+st.pos.Render("("+defHint+")"))
	kv("test_files", testFilesName(rs.TestFiles))
	if len(rs.Skip) > 0 {
		kv("skip", st.pos.Render(strings.Join(rs.Skip, ", ")))
	}

	fmt.Fprintf(&b, "\n%s\n", st.pos.Render("components"))
	nameW := 0
	for _, c := range rs.Components {
		if len(c.Name) > nameW {
			nameW = len(c.Name)
		}
	}
	for _, c := range rs.Components {
		fmt.Fprintf(&b, "  %s   %s\n",
			st.rule.Render(fmt.Sprintf("%-*s", nameW, c.Name)),
			st.pos.Render(stanceShort(rs.Stance(c.Name))))
		fmt.Fprintf(&b, "      %s\n", st.pos.Render(strings.Join(c.Patterns, ", ")))
		if r, ok := rs.Rules[c.Name]; ok {
			if len(r.Allow) > 0 {
				fmt.Fprintf(&b, "      %s  %s\n", st.good.Render("allow"), refsInline(r.Allow))
			}
			if len(r.Deny) > 0 {
				fmt.Fprintf(&b, "      %s   %s\n", st.bad.Render("deny"), refsInline(r.Deny))
			}
		}
	}

	if len(rs.Boundaries) > 0 {
		fmt.Fprintf(&b, "\n%s\n", st.pos.Render("boundaries"))
		for _, bd := range rs.Boundaries {
			head := "  " + st.rule.Render(bd.Name)
			if bd.Sealed {
				head += "  " + st.warn.Render("sealed")
			}
			labels := make([]string, len(bd.Members))
			for i, m := range bd.Members {
				labels[i] = m.Label
			}
			fmt.Fprintf(&b, "%s\n      %s\n", head, st.pos.Render(strings.Join(labels, ", ")))
		}
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// stanceShort names a component's inferred stance in one word: whitelist (only
// allow refs pass) or blacklist (everything passes except deny refs).
func stanceShort(p core.Policy) string {
	if p == core.PolicyAllow {
		return "blacklist"
	}
	return "whitelist"
}

// refsInline renders refs comma-separated without brackets, matching the TUI's
// Config tab (refList keeps the brackets for explain/JSON).
func refsInline(refs []core.Ref) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = r.String()
	}
	return strings.Join(parts, ", ")
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
