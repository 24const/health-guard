package errorx

import "errors"

var (
	ErrNotFound      = errors.New("not found")
	ErrBusy          = errors.New("conversation busy")
	ErrIdle          = errors.New("no active conversation")
	ErrInvalidInput  = errors.New("invalid input")
	ErrForbidden     = errors.New("forbidden")
	ErrStaleCallback = errors.New("stale callback")
)
