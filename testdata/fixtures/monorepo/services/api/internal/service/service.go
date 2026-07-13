// Package service orchestrates the domain. May import domain + stdlib.
package service

import (
	"errors"

	"example.test/api/internal/domain/order"
)

var ErrEmpty = errors.New("empty order id")

func Process(o order.Order) error {
	if o.ID == "" {
		return ErrEmpty
	}
	return nil
}
