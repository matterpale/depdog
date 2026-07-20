// Package app imports domain (allowed via the `core` alias defined under the
// deprecated groups: key) and the standard library.
package app

import (
	"fmt"

	"example.test/groupsdep/internal/domain"
)

// Run touches domain and std so both edges exist and neither is dropped.
func Run() { fmt.Println(domain.Name()) }
