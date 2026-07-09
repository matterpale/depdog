// Package service-b is service B. Importing service A crosses the cmd-services
// boundary (member → member) and is a hard deny.
package serviceb

import _ "boundaryfix/cmd/service-a"

// Run is the service entrypoint.
func Run() {}
