package user

import "errors"

var (
	errSelfOperation      = errors.New("cannot target your own account")
	errInvalidUserPayload = errors.New("invalid user payload")
)
