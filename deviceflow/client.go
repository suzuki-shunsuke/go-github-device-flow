package deviceflow

import (
	"errors"
	"net/http"
	"time"
)

// Client handles GitHub App authentication and access token generation using OAuth device flow.
// It manages the complete authentication flow including device code requests, user authorization,
// and access token polling.
type Client struct {
	input *Input // Configuration and dependencies for the client
}

// New creates a new Client with the provided HTTP client.
// The client uses the provided HTTP client for all API requests.
func New(input *Input) *Client {
	if input == nil {
		input = &Input{}
	}
	if input.HTTPClient == nil {
		input.HTTPClient = http.DefaultClient
	}
	if input.wallSince == nil {
		input.wallSince = wallSince
	}
	return &Client{
		input: input,
	}
}

// Input contains all dependencies and configuration needed by the Client.
// It allows for dependency injection and makes testing easier by providing
// customizable implementations of external dependencies.
type Input struct {
	HTTPClient *http.Client // HTTP client for API requests

	// wallSince measures elapsed time with the wall clock. Tests replace it to
	// simulate a monotonic clock that runs faster than real time.
	wallSince func(start time.Time) time.Duration
}

// wallSince returns the time elapsed since start according to the wall clock.
// time.Time carries both a wall clock reading and a monotonic reading, and
// time.Since would use the monotonic one, which is exactly the reading that
// cannot be trusted here. Passing start through Round(0), UTC() or a marshaller
// strips its monotonic reading and makes this fall back to plain subtraction.
func wallSince(start time.Time) time.Duration {
	return time.Duration(time.Now().UnixNano() - start.UnixNano())
}

var (
	errNotOK            = errors.New("status code isn't 200")
	errEmptyAccessToken = errors.New("access_token is empty")
	errTooManySlowDowns = errors.New("GitHub rejected too many polls as too frequent")
)

// AccessToken represents the response from GitHub's access token endpoint.
// It contains either an access token or an error message.
type AccessToken struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	Interval              int    `json:"interval"`

	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	ErrorURI         string `json:"error_uri"`
}
