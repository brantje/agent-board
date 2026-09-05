package app

import "errors"

type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Err }

func NewError(code, message string, err error) error {
	return &Error{Code: code, Message: message, Err: err}
}

func AsError(err error) (*Error, bool) {
	var target *Error
	ok := errors.As(err, &target)
	return target, ok
}
