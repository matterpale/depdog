// Package comparator is service B. Importing service A crosses the cmd-services
// boundary (member → member) and is a hard deny.
package comparator

import _ "boundaryfix/cmd/query-ce"

// Run is the service entrypoint.
func Run() {}
