package checkout

import (
	"testing"

	_ "example.test/dirty/internal/repository"
)

// A test-only import of another component: still a violation under the
// hybrid test_files mode (only external test imports are exempt).
func TestCheckout(t *testing.T) {
	if Checkout(100) != 100 {
		t.Fatal("nope")
	}
}
