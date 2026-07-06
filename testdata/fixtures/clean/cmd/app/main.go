package main

import (
	"fmt"

	"example.test/clean/internal/domain/order"
	"example.test/clean/internal/handler"
	"example.test/clean/internal/repository"
	"example.test/clean/internal/service"
)

func main() {
	o := order.New(" o-1 ")
	fmt.Println(handler.Describe(o), service.Process(o), repository.Store(o))
}
