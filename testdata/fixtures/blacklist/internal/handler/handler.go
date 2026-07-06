package handler

import (
	"example.test/blacklist/internal/domain"
	_ "example.test/blacklist/internal/service"
)

func Handle() string { return domain.ID() }
