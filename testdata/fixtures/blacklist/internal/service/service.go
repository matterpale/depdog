package service

// The domain import is fine here: policy allow, and no rule denies it.
import "example.test/blacklist/internal/domain"

func Serve() string { return domain.ID() }
