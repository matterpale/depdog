package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

func TestRuleSetDump(t *testing.T) {
	rs := &core.RuleSet{
		Components: []core.Component{
			{Name: "domain", Patterns: []string{"internal/domain/**"}},
			{Name: "handler", Patterns: []string{"internal/handler/**"}},
		},
		Rules: map[string]core.Rule{
			"domain":  {Allow: []core.Ref{{Kind: core.RefStd}}},
			"handler": {Deny: []core.Ref{{Kind: core.RefComponent, Name: "domain"}}},
		},
		Policy:    core.PolicyDeny,
		TestFiles: core.TestRelaxed,
		Skip:      []string{"internal/legacy/**"},
	}
	var buf bytes.Buffer
	if err := RuleSet(&buf, rs, "never"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"default", "deny", "rule-less components import nothing",
		"test_files", "relaxed", "skip", "internal/legacy/**",
		"domain", "whitelist", "internal/domain/**", "allow  std",
		"handler", "blacklist", "deny   domain",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rule-set dump missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("--color never must not emit ANSI escapes:\n%q", out)
	}
}
