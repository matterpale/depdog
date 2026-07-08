// Package badshared is a shared lib that reaches into a service. Under the
// sealed cmd-services boundary an ungrouped source importing a member is denied.
package badshared

import _ "boundaryfix/cmd/query-ce"
