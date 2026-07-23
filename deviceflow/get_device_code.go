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

// DeviceCodeResponse represents the response from GitHub's device code endpoint.
// It contains the device code and user code needed for authentication.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// GetDeviceCode requests a device code from GitHub's OAuth device endpoint.
// It returns the device code response containing the user code and verification URL.
func (c *Client) GetDeviceCode(ctx context.Context, clientID string) (*DeviceCodeResponse, *http.Response, []byte, error) {
	if clientID == "" {
		return nil, nil, nil, errors.New("client id is required")
	}
	jsonData, err := json.Marshal(map[string]string{
		wordClientID: clientID,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal a request body as JSON: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/device/code", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create a request for device code: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.input.HTTPClient.Do(req)
	if err != nil {
		return nil, resp, nil, fmt.Errorf("send a request for device code: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp, nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp, body, fmt.Errorf("%w (%d)", errNotOK, resp.StatusCode)
	}

	deviceCode := &DeviceCodeResponse{}
	if err := json.Unmarshal(body, deviceCode); err != nil {
		return nil, resp, body, fmt.Errorf("unmarshal response body as JSON: %w", err)
	}

	return deviceCode, resp, body, nil
}
