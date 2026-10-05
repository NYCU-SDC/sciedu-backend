package pagevisit

import "errors"

var (
	ErrNotFound            = errors.New("page visit not found")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for a different PageVisit request")
	ErrInvalidInput        = errors.New("invalid page visit input")
)
