package service

import (
	"testing"

	"example.test/extlib"
)

// External and test-only: exempt under hybrid, so not a violation.
func TestProcess(t *testing.T) {
	if Process() != "ok" || extlib.Answer() != 42 {
		t.Fatal("nope")
	}
}
