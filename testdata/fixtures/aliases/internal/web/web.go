// Package web is a pure layer barred from the SDK: the `sdk` alias appears in
// its deny list, so this goodlib import is a violation even though goodlib is a
// permitted module for api.
package web

import "example.test/goodlib"

// Ready touches goodlib so the denied import is not dropped by the compiler.
func Ready() bool { return goodlib.OK() }
