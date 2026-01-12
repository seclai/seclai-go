package seclai

import "fmt"

// ConfigurationError indicates invalid or missing client configuration.
type ConfigurationError struct {
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
	StatusCode   int
	Method       string
	URL          string
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
	ValidationError *HTTPValidationError
}

func (e *APIValidationError) Error() string {
	if e == nil {
		return "seclai: api validation error"
	}
	return (&e.APIStatusError).Error()
}
