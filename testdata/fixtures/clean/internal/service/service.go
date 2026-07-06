package service

import (
	"errors"

	"example.test/clean/internal/domain/order"
)

var ErrEmpty = errors.New("empty order id")

func Process(o order.Order) error {
	if o.ID == "" {
		return ErrEmpty
	}
	return nil
}
