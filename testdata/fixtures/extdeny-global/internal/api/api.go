// Package api allows any external module in its own rule, yet the module-wide
// deny still blocks example.test/extlib here. The permitted example.test/goodlib
// passes, proving the ban is targeted, not a blanket external block.
package api

import (
	"fmt"

	"example.test/extlib"
	"example.test/goodlib"
)

func Run() {
	fmt.Println(extlib.Answer(), goodlib.OK())
}
