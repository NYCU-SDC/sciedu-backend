package progress

import "errors"

var (
	ErrNotFound  = errors.New("progress not found")
	ErrForbidden = errors.New("progress access forbidden")
)
