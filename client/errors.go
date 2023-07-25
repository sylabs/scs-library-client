package client

import "errors"

var (
	errHTTP = errors.New("http error")
	// ErrUnauthorized represents HTTP status "401 Unauthorized"
	ErrUnauthorized             = errors.New("unauthorized")
	errImageRefArchRequired     = errors.New("imageRef and arch are required")
	errBadRequest               = errors.New("bad request")
	errUnexpectedMalformedValue = errors.New("unexpected/malformed value")
	errOCIRegistry              = errors.New("OCI registry error")
	errArchNotSpecified         = errors.New("architecture not specified")
	errInvalidAuthHeader        = errors.New("invalid auth header")
	errResetHTTPBody            = errors.New("unable to reset HTTP request body")
	errArchitectureNotPresent   = errors.New("architecture not present")
	errDigestNotVerified        = errors.New("digest not verified")
	errOCIDownloadNotSupported  = errors.New("not supported")
	errGettingPresignedURL      = errors.New("error getting presigned URL")
	errParsingPresignedURL      = errors.New("error parsing presigned URL")
	// ErrRefSchemeNotValid represents a ref with an invalid scheme.
	ErrRefSchemeNotValid = errors.New("library: ref scheme not valid")
	// ErrRefUserNotPermitted represents a ref with an invalid user.
	ErrRefUserNotPermitted = errors.New("library: user not permitted in ref")
	// ErrRefQueryNotPermitted represents a ref with an invalid query.
	ErrRefQueryNotPermitted = errors.New("library: query not permitted in ref")
	// ErrRefFragmentNotPermitted represents a ref with an invalid fragment.
	ErrRefFragmentNotPermitted = errors.New("library: fragment not permitted in ref")
	// ErrRefPathNotValid represents a ref with an invalid path.
	ErrRefPathNotValid = errors.New("library: ref path not valid")
	// ErrRefTagsNotValid represents a ref with invalid tags.
	ErrRefTagsNotValid = errors.New("library: ref tags not valid")
	// ErrNotFound is returned by when a resource is not found (http status 404)
	ErrNotFound                  = errors.New("not found")
	errQueryValueMustBeSpecified = errors.New("search query ('value') must be specified")
)
