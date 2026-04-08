package dolthubauth

import "errors"

var (
	ErrNotFound               = errors.New("not found")
	ErrConflict               = errors.New("conflict")
	ErrInvalidMetadata        = errors.New("invalid metadata")
	ErrInvalidConnectToken    = errors.New("invalid connect token")
	ErrExpiredConnectToken    = errors.New("expired connect token")
	ErrMetadataMismatch       = errors.New("metadata mismatch")
	ErrServiceUnauthorized    = errors.New("service unauthorized")
	ErrServiceReplay          = errors.New("service replay")
	ErrLastWasteland          = errors.New("last wasteland")
	ErrWastelandNotFound      = errors.New("wasteland not found")
	ErrValidationFailed       = errors.New("credential validation failed")
	ErrOriginNotAllowlisted   = errors.New("origin not allowlisted")
	ErrUnsupportedProxyTarget = errors.New("unsupported proxy target")
)

type ValidationError struct {
	Code ValidationErrorCode
	Err  error
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ErrValidationFailed.Error()
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Code != "" {
		return string(e.Code)
	}
	return ErrValidationFailed.Error()
}

func (e *ValidationError) Unwrap() error {
	return ErrValidationFailed
}
