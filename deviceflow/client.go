package deviceflow

import (
	"errors"
	"net/http"
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
	return &Client{
		input: input,
	}
}

// Input contains all dependencies and configuration needed by the Client.
// It allows for dependency injection and makes testing easier by providing
// customizable implementations of external dependencies.
type Input struct {
	HTTPClient *http.Client // HTTP client for API requests
}

var (
	errNotOK            = errors.New("status code isn't 200")
	errEmptyAccessToken = errors.New("access_token is empty")
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
