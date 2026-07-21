package cli

import (
	"strings"
	"testing"
)

func TestSampleEvenly(t *testing.T) {
	all := strings.Fields("a b c d e f g h i j") // 10 elements

	got := sampleEvenly(all, 4)
	if len(got) != 4 {
		t.Fatalf("sampleEvenly(10, 4) len = %d, want 4: %v", len(got), got)
	}
	if got[0] != "a" || got[len(got)-1] != "j" {
		t.Errorf("sampleEvenly must keep the first and last: %v", got)
	}

	// Fewer elements than the cap → all of them, unchanged.
	if got := sampleEvenly([]string{"x", "y"}, 5); len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Errorf("sampleEvenly should return all when len<=max: %v", got)
	}

	// Exactly the cap → all.
	if got := sampleEvenly(all, 10); len(got) != 10 {
		t.Errorf("sampleEvenly(10, 10) len = %d, want 10", len(got))
	}
}
