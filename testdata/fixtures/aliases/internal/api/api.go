// Package api may use the whole external SDK: both goodlib and extlib are
// covered by the `sdk` alias in its allow list, so neither import is flagged.
package api

import (
	"fmt"

	"example.test/extlib"
	"example.test/goodlib"
)

// Run touches both externals so neither import is dropped by the compiler.
func Run() {
	fmt.Println(extlib.Answer(), goodlib.OK())
}
