// Package parity holds the declarative engine's M4 trust-gate test. It proves
// that the generic engine (internal/lang/spec), driven by the illustrative Ruby
// spec, reproduces the hand-written Ruby adapter (internal/lang/ruby)
// byte-for-byte on that adapter's own fixtures — same module path, nodes, edges,
// classes, relative dirs, test-only attribution, and positions.
//
// It lives in its own package because the test imports BOTH adapters, which each
// adapter's self-check rule (rightly) forbids inside the adapter's own package.
// This package is test-only; the doc file exists solely so `go build ./...` sees
// a non-test file here.
package parity
