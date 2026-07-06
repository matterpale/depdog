package service

import (
	"testing"

	"example.test/extlib"

	"example.test/clean/internal/domain/order"
)

// The extlib import is external and test-only: allowed under the default
// hybrid test_files mode even though service's rules don't list external.
func TestProcess(t *testing.T) {
	if extlib.Answer() != 42 {
		t.Fatal("universe broken")
	}
	if err := Process(order.New("o-1")); err != nil {
		t.Fatal(err)
	}
}
