package progress

import "errors"

var (
	ErrNotFound     = errors.New("progress not found")
	ErrPageNotFound = errors.New("page not found")
	ErrInvalidInput = errors.New("invalid progress input")
)
