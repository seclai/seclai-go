package seclai

import "fmt"

// ConfigurationError indicates invalid or missing client configuration.
type ConfigurationError struct {
	// Message describes what is misconfigured.
	Message string
}

func (e *ConfigurationError) Error() string {
	if e == nil {
		return "seclai: configuration error"
	}
	return fmt.Sprintf("seclai: configuration error: %s", e.Message)
}

// APIStatusError is returned for non-2xx HTTP responses.
type APIStatusError struct {
	// StatusCode is the HTTP status code (e.g. 400, 404, 500).
	StatusCode int
	// Method is the HTTP method used (e.g. "GET", "POST").
	Method string
	// URL is the full request URL.
	URL string
	// ResponseText is the raw response body, if available.
	ResponseText string
}

func (e *APIStatusError) Error() string {
	if e == nil {
		return "seclai: api status error"
	}
	if e.ResponseText != "" {
		return fmt.Sprintf("seclai: api error (%d) %s %s: %s", e.StatusCode, e.Method, e.URL, e.ResponseText)
	}
	return fmt.Sprintf("seclai: api error (%d) %s %s", e.StatusCode, e.Method, e.URL)
}

// APIValidationError is returned for HTTP 422 responses.
//
// When the API returns a structured validation payload, it is captured in ValidationError.
type APIValidationError struct {
	APIStatusError
	// ValidationError is the parsed validation payload, if available.
	ValidationError *HTTPValidationError
}

func (e *APIValidationError) Error() string {
	if e == nil {
		return "seclai: api validation error"
	}
	return (&e.APIStatusError).Error()
}

// StreamingError indicates a failure during an SSE streaming operation.
type StreamingError struct {
	// Message describes what went wrong.
	Message string
	// RunID is the agent run ID when available, empty otherwise.
	RunID string
}

func (e *StreamingError) Error() string {
	if e == nil {
		return "seclai: streaming error"
	}
	if e.RunID != "" {
		return fmt.Sprintf("seclai: streaming error (run %s): %s", e.RunID, e.Message)
	}
	return fmt.Sprintf("seclai: streaming error: %s", e.Message)
}

// UnexpectedResponseError is returned when a successful (2xx) response cannot be
// read as the list its method returns: either it is not a list at all, or it is
// a list that could not be decoded. Returned rather than an empty result, which
// would read as "no results". Unwrap gives the JSON error where there is one.
type UnexpectedResponseError struct {
	// Method is the HTTP method used (e.g. "GET").
	Method string
	// URL is the request URL, or the API path when the full URL is not known.
	URL string
	// Message describes what was expected and what arrived.
	Message string
	// ResponseText is the raw response body.
	ResponseText string

	cause error
}

// Unwrap returns the underlying JSON decode error, or nil when the body was
// valid JSON of the wrong shape.
func (e *UnexpectedResponseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *UnexpectedResponseError) Error() string {
	if e == nil {
		return "seclai: unexpected response"
	}
	return fmt.Sprintf("seclai: unexpected response from %s %s: %s", e.Method, e.URL, e.Message)
}
