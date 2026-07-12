package deviceflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	wordAuthPending = "authorization_pending"
	wordSlowDown    = "slow_down"
)

// Poll continuously polls GitHub for an access token.
// It respects the polling interval and handles authorization pending and slow down responses.
// The polling continues until the device code expires or the user completes authentication.
func (c *Client) Poll(ctx context.Context, logger *slog.Logger, clientID string, deviceCode *DeviceCodeResponse, input *InputGetAccessToken) (*AccessToken, error) {
	ticker := c.input.NewTicker(max(time.Duration(deviceCode.Interval)*time.Second, 5*time.Second)) //nolint:mnd
	defer ticker.Stop()

	deadline := c.input.Now().Add(time.Duration(deviceCode.ExpiresIn) * time.Second)

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context was cancelled: %w", ctx.Err())
		case <-ticker.C:
			if c.input.Now().After(deadline) {
				return nil, errors.New("device code expired")
			}

			token, _, _, err := c.GetAccessToken(ctx, clientID, deviceCode.DeviceCode, input) //nolint:bodyclose
			if err != nil {
				if rerr := c.handlePollError(logger, ticker, token, err); rerr != nil {
					return nil, rerr
				}
				continue
			}

			if token != nil {
				return token, nil
			}
		}
	}
}

// handlePollError processes an error from GetAccessToken during polling.
// It returns nil when polling should continue, or a non-nil error when polling
// should stop and return that error.
func (c *Client) handlePollError(logger *slog.Logger, ticker *time.Ticker, token *AccessToken, err error) error {
	if token == nil {
		return err
	}
	switch token.Error {
	case wordAuthPending:
		logger.Debug(
			"device flow's authorization is still pending",
			"error_description", token.ErrorDescription,
			"error_uri", token.ErrorURI,
		)
		return nil
	case wordSlowDown:
		logger.Debug(
			"device flow's polling was too frequent, slowing down",
			"error_description", token.ErrorDescription,
			"error_uri", token.ErrorURI,
			"interval", token.Interval,
		)
		interval := 10 * time.Second //nolint:mnd
		if token.Interval > 0 {
			interval = time.Duration(token.Interval) * time.Second
		}
		ticker.Reset(interval)
		return nil
	default:
		return err
	}
}
