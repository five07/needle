package tidal

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Sentinel errors callers can test with errors.Is against an *APIError.
var (
	// ErrUnauthorized means the access token was rejected and refreshing it
	// did not help. The user must log in again.
	ErrUnauthorized = errors.New("tidal: unauthorized")
	// ErrNotFound means the requested resource does not exist.
	ErrNotFound = errors.New("tidal: not found")
	// ErrRateLimited means retries were exhausted while rate limited.
	ErrRateLimited = errors.New("tidal: rate limited")
	// ErrInvalidResourceID means the API rejected a malformed resource ID.
	ErrInvalidResourceID = errors.New("tidal: invalid resource id")
	// ErrConcurrentPlayback means the account is playing on another device.
	ErrConcurrentPlayback = errors.New("tidal: account is playing on another app or device")
	// ErrGeoRestricted means the content is unavailable in the user's country.
	ErrGeoRestricted = errors.New("tidal: content is unavailable in your location")
	// ErrPurchaseRequired means the content must be purchased first.
	ErrPurchaseRequired = errors.New("tidal: content must be purchased before playback")
	// ErrClientNotEntitled means this app is not allowed to access the content.
	ErrClientNotEntitled = errors.New("tidal: client cannot access this content")
	// ErrPrerequisiteMissing means playback prerequisites are missing.
	ErrPrerequisiteMissing = errors.New("tidal: required playback prerequisites are missing")
)

// codeSentinels maps JSON:API error codes to sentinels.
var codeSentinels = map[string]error{
	"CONCURRENT_PLAYBACK":  ErrConcurrentPlayback,
	"GEO_RESTRICTED":       ErrGeoRestricted,
	"PURCHASE_REQUIRED":    ErrPurchaseRequired,
	"CLIENT_NOT_ENTITLED":  ErrClientNotEntitled,
	"PREREQUISITE_MISSING": ErrPrerequisiteMissing,
	"INVALID_RESOURCE_ID":  ErrInvalidResourceID,
}

// maxDetail bounds how much server-provided text appears in error messages.
const maxDetail = 200

// ErrorObject is a JSON:API error object.
type ErrorObject struct {
	Status string `json:"status"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Source struct {
		Pointer   string `json:"pointer"`
		Parameter string `json:"parameter"`
	} `json:"source"`
}

// APIError is a non-success response from the TIDAL API.
type APIError struct {
	// Method and Path identify the request (path without query string).
	Method string
	Path   string
	// Status is the HTTP status code.
	Status int
	// Errors are the JSON:API error objects, if the body had any.
	Errors []ErrorObject
	// RetryAfter is the server-requested wait for 429 and 503 responses.
	RetryAfter time.Duration
}

// Code returns the first error object's code, or "".
func (e *APIError) Code() string {
	if len(e.Errors) == 0 {
		return ""
	}
	return e.Errors[0].Code
}

// Error describes the failure without tokens or the raw response body.
func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "tidal: %s %s: %d %s", e.Method, e.Path, e.Status, http.StatusText(e.Status))
	if len(e.Errors) > 0 {
		first := e.Errors[0]
		if first.Code != "" {
			b.WriteString(": ")
			b.WriteString(truncate(first.Code, maxDetail))
		}
		if first.Detail != "" {
			b.WriteString(": ")
			b.WriteString(truncate(first.Detail, maxDetail))
		}
	}
	return b.String()
}

// Is matches sentinels by error code first, then by HTTP status.
func (e *APIError) Is(target error) bool {
	for _, obj := range e.Errors {
		if s, ok := codeSentinels[obj.Code]; ok && s == target {
			return true
		}
	}
	switch target {
	case ErrUnauthorized:
		return e.Status == http.StatusUnauthorized
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrRateLimited:
		return e.Status == http.StatusTooManyRequests
	}
	return false
}

// newAPIError builds an APIError from a response body, parsing JSON:API
// error objects when present and ignoring anything else.
func newAPIError(method, path string, status int, body []byte, retryAfter time.Duration) *APIError {
	e := &APIError{Method: method, Path: path, Status: status, RetryAfter: retryAfter}
	var doc struct {
		Errors []ErrorObject `json:"errors"`
	}
	if json.Unmarshal(body, &doc) == nil {
		e.Errors = doc.Errors
	}
	return e
}

func truncate(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
