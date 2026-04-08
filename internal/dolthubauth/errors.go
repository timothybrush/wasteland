package dolthubauth

import "errors"

var (
	// ErrNotFound indicates a requested record was not present.
	ErrNotFound = errors.New("not found")
	// ErrConflict indicates an optimistic concurrency conflict.
	ErrConflict = errors.New("conflict")
	// ErrInvalidMetadata indicates the submitted connection metadata was rejected.
	ErrInvalidMetadata = errors.New("invalid metadata")
	// ErrInvalidConnectToken indicates a connect token could not be redeemed.
	ErrInvalidConnectToken = errors.New("invalid connect token")
	// ErrExpiredConnectToken indicates a connect token expired before redemption.
	ErrExpiredConnectToken = errors.New("expired connect token")
	// ErrMetadataMismatch indicates the submitted metadata did not match the token.
	ErrMetadataMismatch = errors.New("metadata mismatch")
	// ErrServiceUnauthorized indicates the service-auth signature was invalid.
	ErrServiceUnauthorized = errors.New("service unauthorized")
	// ErrServiceReplay indicates the service-auth nonce was already used.
	ErrServiceReplay = errors.New("service replay")
	// ErrLastWasteland indicates a delete would remove the final Wasteland entry.
	ErrLastWasteland = errors.New("last wasteland")
	// ErrWastelandNotFound indicates the requested Wasteland entry was missing.
	ErrWastelandNotFound = errors.New("wasteland not found")
	// ErrValidationFailed indicates the upstream credential validation failed.
	ErrValidationFailed = errors.New("credential validation failed")
	// ErrOriginNotAllowlisted indicates the browser origin is not approved.
	ErrOriginNotAllowlisted = errors.New("origin not allowlisted")
	// ErrUnsupportedProxyTarget indicates a proxy request targeted an unsupported URL.
	ErrUnsupportedProxyTarget = errors.New("unsupported proxy target")
)

// ValidationError wraps an upstream credential validation failure with a code.
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
