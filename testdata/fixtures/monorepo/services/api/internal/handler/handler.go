// Package handler is the HTTP edge. May import service, domain and stdlib.
package handler

import (
	"fmt"

	"example.test/api/internal/domain/order"
	"example.test/api/internal/service"
)

func Handle(id string) (string, error) {
	o := order.New(id)
	if err := service.Process(o); err != nil {
		return "", err
	}
	return fmt.Sprintf("order %s ok", o.ID), nil
}
