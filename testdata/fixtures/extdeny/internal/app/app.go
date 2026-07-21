// Package app imports one permitted external module (goodlib) and one denied
// external module (extlib). depdog should flag only the extlib edge: the
// deny-list entry wins over the broad `allow: [external]`.
package app

import (
	"fmt"

	"example.test/extlib"
	"example.test/goodlib"
)

// Run touches both externals so neither import is dropped by the compiler.
func Run() {
	fmt.Println(extlib.Answer(), goodlib.OK())
}
