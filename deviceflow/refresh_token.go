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

const wordRefreshToken = "refresh_token"

// RefreshToken exchanges a refresh token for a new access token.
// It returns the new access token, the raw HTTP response and body, and an error
// if the request fails or GitHub reports an error.
func (c *Client) RefreshToken(ctx context.Context, clientID, refreshToken string) (*AccessToken, *http.Response, []byte, error) {
	if clientID == "" {
		return nil, nil, nil, errors.New("client id is required")
	}
	if refreshToken == "" {
		return nil, nil, nil, errors.New("refresh token is required")
	}

	reqBody := map[string]string{
		wordClientID:     clientID,
		wordRefreshToken: refreshToken,
		"grant_type":     wordRefreshToken,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal request body as JSON: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create a request for refresh token: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.input.HTTPClient.Do(req)
	if err != nil {
		return nil, resp, nil, fmt.Errorf("send a request for refresh token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp, nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp, body, fmt.Errorf("%w (%d)", errNotOK, resp.StatusCode)
	}

	token, err := parseAccessToken(body)
	return token, resp, body, err
}
