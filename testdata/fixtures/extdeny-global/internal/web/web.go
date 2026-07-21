// Package web is a second, independently-ruled component that also allows any
// external module. The module-wide deny blocks example.test/extlib here too,
// which a catch-all component could never do — this is the case that motivates a
// top-level deny.
package web

import (
	"fmt"

	"example.test/extlib"
)

func Serve() {
	fmt.Println(extlib.Answer())
}
