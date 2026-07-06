// Package order is the domain heart of the fixture: std-lib imports only.
package order

import "strings"

type Order struct {
	ID string
}

func New(id string) Order {
	return Order{ID: strings.TrimSpace(id)}
}
