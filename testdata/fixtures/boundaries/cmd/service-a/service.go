// Package servicea is service A. Importing a shared lib (member → ungrouped) is
// allowed even under the sealed boundary — the wall is one-way.
package servicea

import _ "boundaryfix/internal/shared"

// Run is the service entrypoint.
func Run() {}
