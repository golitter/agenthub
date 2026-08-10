package service

type BizError struct {
	Code    int
	Message string
}

func (err *BizError) Error() string {
	return err.Message
}

func ErrBadRequest(msg string) *BizError {
	return &BizError{Code: 400, Message: msg}
}

func ErrUnauthorized(msg string) *BizError {
	return &BizError{Code: 401, Message: msg}
}

func ErrNotFound(msg string) *BizError {
	return &BizError{Code: 404, Message: msg}
}

func ErrGone(msg string) *BizError {
	return &BizError{Code: 410, Message: msg}
}

func ErrAccepted(msg string) *BizError {
	return &BizError{Code: 202, Message: msg}
}

func ErrConflict(msg string) *BizError {
	return &BizError{Code: 409, Message: msg}
}

func ErrForbidden(msg string) *BizError {
	return &BizError{Code: 403, Message: msg}
}

func ErrServiceUnavailable(msg string) *BizError {
	return &BizError{Code: 503, Message: msg}
}

func ErrInternal(msg string) *BizError {
	return &BizError{Code: 500, Message: msg}
}
