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

func (c *Client) RefreshToken(ctx context.Context, clientID, refreshToken string) (*AccessToken, *http.Response, []byte, error) {
	reqBody := map[string]string{
		"client_id":     clientID,
		"refresh_token": refreshToken,
		"grant_type":    "refresh_token",
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
		return nil, resp, body, errNotOK
	}

	token := &AccessToken{}
	if err := json.Unmarshal(body, token); err != nil {
		return nil, resp, body, fmt.Errorf("unmarshal response body as JSON: %w", err)
	}
	if token.Error != "" {
		if token.ErrorDescription != "" {
			return token, resp, body, errors.New(token.ErrorDescription)
		}
		return token, resp, body, errors.New(token.Error)
	}

	if token.AccessToken == "" {
		return token, resp, body, errEmptyAccessToken
	}
	return token, resp, body, nil
}
