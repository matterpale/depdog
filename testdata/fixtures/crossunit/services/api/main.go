// The api service illegally depends on its boundary peer billing (both are
// members of the work file's `services` boundary).
package main

import (
	"fmt"

	"example.com/billing/pkg"
)

func main() {
	fmt.Println(pkg.Bill())
}
