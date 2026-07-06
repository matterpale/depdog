package core

import "testing"

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern, dir string
		want         bool
	}{
		{"internal/domain/**", "internal/domain", true},
		{"internal/domain/**", "internal/domain/order", true},
		{"internal/domain/**", "internal/domain/order/detail", true},
		{"internal/domain/**", "internal/handler", false},
		{"internal/domain/**", "internal", false},
		{"internal/**", "internal/domain/order", true},
		{"cmd/**", "cmd", true},
		{"cmd/*", "cmd/app", true},
		{"cmd/*", "cmd/app/sub", false},
		{"**/repo*", "internal/db/repository", true},
		{"**", "anything/at/all", true},
		{"**", ".", true},
		{".", ".", true},
		{"internal", "internal", true},
		{"internal", "internal/domain", false},
	}
	for _, tt := range tests {
		got, err := MatchPattern(tt.pattern, tt.dir)
		if err != nil {
			t.Fatalf("MatchPattern(%q, %q): %v", tt.pattern, tt.dir, err)
		}
		if got != tt.want {
			t.Errorf("MatchPattern(%q, %q) = %v, want %v", tt.pattern, tt.dir, got, tt.want)
		}
	}
}

func TestValidatePattern(t *testing.T) {
	for _, ok := range []string{"internal/**", "cmd/*", "**", ".", "a/b/c"} {
		if err := ValidatePattern(ok); err != nil {
			t.Errorf("ValidatePattern(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "/abs/path", `win\path`, "a/[unclosed/**"} {
		if err := ValidatePattern(bad); err == nil {
			t.Errorf("ValidatePattern(%q) = nil, want error", bad)
		}
	}
}

func TestAssignComponent(t *testing.T) {
	rs := &RuleSet{Components: []Component{
		{Name: "app", Patterns: []string{"internal/**"}},
		{Name: "domain", Patterns: []string{"internal/domain/**"}},
		{Name: "main", Patterns: []string{"cmd/**"}},
	}}

	tests := []struct {
		dir, want string
	}{
		{"internal/handler", "app"},
		{"internal/domain", "domain"},        // deeper pattern wins over catch-all
		{"internal/domain/order", "domain"},  // carve-out is recursive
		{"cmd/depdog", "main"},
		{"pkg/unrelated", ""}, // unassigned
	}
	for _, tt := range tests {
		got, err := rs.AssignComponent(tt.dir)
		if err != nil {
			t.Fatalf("AssignComponent(%q): %v", tt.dir, err)
		}
		if got != tt.want {
			t.Errorf("AssignComponent(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}

func TestAssignComponentAmbiguous(t *testing.T) {
	rs := &RuleSet{Components: []Component{
		{Name: "a", Patterns: []string{"internal/x/**"}},
		{Name: "b", Patterns: []string{"internal/x/**"}},
	}}
	_, err := rs.AssignComponent("internal/x/pkg")
	if err == nil {
		t.Fatal("want ambiguity error, got nil")
	}
	amb, ok := err.(*AmbiguityError)
	if !ok {
		t.Fatalf("want *AmbiguityError, got %T: %v", err, err)
	}
	if len(amb.Components) != 2 {
		t.Errorf("ambiguity between %v, want 2 components", amb.Components)
	}
}
