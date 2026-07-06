package core

import "testing"

// FuzzMatchPattern checks that MatchPattern never panics, and that any pattern
// ValidatePattern accepts matches without error — the invariant that lets the
// engine validate patterns once up front and match freely thereafter.
func FuzzMatchPattern(f *testing.F) {
	seeds := []struct{ pat, dir string }{
		{"internal/**", "internal/domain"},
		{"cmd/*", "cmd/app"},
		{"**", ""},
		{"a/b/c", "a/b/c"},
		{"internal/domain/**", "internal/domain/order/sub"},
		{"[", "x"},
		{"*", "top"},
		{"", ""},
	}
	for _, s := range seeds {
		f.Add(s.pat, s.dir)
	}
	f.Fuzz(func(t *testing.T, pat, dir string) {
		_, err := MatchPattern(pat, dir)
		if ValidatePattern(pat) == nil && err != nil {
			t.Fatalf("valid pattern %q errored matching dir %q: %v", pat, dir, err)
		}
	})
}
