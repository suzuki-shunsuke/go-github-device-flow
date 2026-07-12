package deviceflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const (
	wordClientID = "client_id"
)

// InputGetAccessToken holds optional parameters for GetAccessToken and Poll.
type InputGetAccessToken struct {
	// RepositoryID scopes the requested access token to a single repository.
	// When empty, the token is not restricted to a specific repository.
	RepositoryID string
}

// GetAccessToken checks if an access token is available for the given device code.
// It returns the access token if available, or an error indicating the current status.
func (c *Client) GetAccessToken(ctx context.Context, clientID, deviceCode string, input *InputGetAccessToken) (*AccessToken, *http.Response, []byte, error) {
	reqBody := map[string]string{
		wordClientID:  clientID,
		"device_code": deviceCode,
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
	}
	if input != nil && input.RepositoryID != "" {
		reqBody["repository_id"] = input.RepositoryID
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal request body as JSON: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create a request for access token: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.input.HTTPClient.Do(req)
	if err != nil {
		return nil, resp, nil, fmt.Errorf("send a request for access token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp, nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp, body, errNotOK
	}

	token, err := parseAccessToken(body)
	return token, resp, body, err
}

// parseAccessToken decodes an access token response body and maps its error
// fields to an error. It returns errEmptyAccessToken when the response contains
// neither an error nor an access token.
func parseAccessToken(body []byte) (*AccessToken, error) {
	token := &AccessToken{}
	if err := json.Unmarshal(body, token); err != nil {
		return nil, fmt.Errorf("unmarshal response body as JSON: %w", err)
	}

	if token.Error != "" {
		if token.ErrorDescription != "" {
			return token, errors.New(token.ErrorDescription)
		}
		return token, errors.New(token.Error)
	}

	if token.AccessToken == "" {
		return token, errEmptyAccessToken
	}
	return token, nil
}
