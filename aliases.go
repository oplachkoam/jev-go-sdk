package jev

import (
	"github.com/oplachkoam/jev-go-sdk/internal/apierror"
)

// Error represents an error that originates from the API, i.e. when a request
// is made and the API returns a response with a HTTP status code of 400 or
// above. Use [errors.As] to get at it:
//
//	var apierr *jev.Error
//	if errors.As(err, &apierr) {
//		println(apierr.StatusCode, apierr.RequestID)
//	}
//
// Other errors, such as connection failures, are returned as they are.
type Error = apierror.Error
