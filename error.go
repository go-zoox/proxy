package proxy

import "net/http"

// HTTPError is an error that wraps an HTTP status code.
type HTTPError struct {
	status  int
	message string
}

// NewHTTPError creates a new HTTPError.
func NewHTTPError(status int, message string) error {
	return &HTTPError{status, message}
}

// Status returns the HTTP status code.
func (h *HTTPError) Status() int {
	if h.status == 0 {
		return http.StatusInternalServerError
	}

	return h.status
}

// Error returns the error message.
func (h *HTTPError) Error() string {
	return h.message
}
