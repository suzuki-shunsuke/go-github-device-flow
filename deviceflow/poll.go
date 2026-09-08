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
	// pollIntervalBuffer is an extra margin added to the polling interval to prevent slow_down.
	pollIntervalBuffer = 100 * time.Millisecond
	// minPollInterval is the shortest interval GitHub documents for the device flow.
	minPollInterval = 5 * time.Second
	// slowDownIncrement is how much the interval grows on slow_down, per RFC 8628 section 3.5.
	slowDownIncrement = 5 * time.Second
	// maxSlowDowns is how many slow_down responses Poll tolerates before giving up.
	// wait already keeps the full interval on the wall clock, so this many
	// slow_down responses mean waiting longer is not what is missing.
	maxSlowDowns = 3
	// waitCompensationFactor caps how long one wait may take, as a multiple of the
	// requested interval, so a wall clock that stalls or steps backwards cannot
	// stretch a single wait indefinitely.
	waitCompensationFactor = 2
)

// pollState carries the mutable state of one Poll call: the current wait
// interval, how many slow_down responses have arrived, and the largest clock
// drift measured so far, which is used only to explain a failure.
type pollState struct {
	interval  time.Duration
	slowDowns int
	drift     float64
}

// Poll continuously polls GitHub for an access token.
// It respects the polling interval and handles authorization pending and slow down responses.
// The polling continues until the device code expires or the user completes authentication.
func (c *Client) Poll(ctx context.Context, logger *slog.Logger, clientID string, deviceCode *DeviceCodeResponse, input *InputGetAccessToken) (*AccessToken, error) {
	if deviceCode == nil {
		return nil, errors.New("device code is required")
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	state := &pollState{
		interval: max(time.Duration(deviceCode.Interval)*time.Second, minPollInterval) + pollIntervalBuffer,
	}
	// Unlike the polling interval, the expiry stays on the monotonic clock. A fast
	// monotonic clock makes it fire early, which is the safe direction: GitHub
	// answers expired_token once the device code is really gone, so this deadline
	// is only a bound, and keeping it monotonic means a stalled wall clock cannot
	// make Poll loop forever.
	deadline := time.Now().Add(time.Duration(deviceCode.ExpiresIn) * time.Second)

	for {
		if err := c.wait(ctx, logger, state); err != nil {
			return nil, err
		}

		if time.Now().After(deadline) {
			return nil, errors.New("device code expired")
		}

		token, _, _, err := c.GetAccessToken(ctx, clientID, deviceCode.DeviceCode, input) //nolint:bodyclose
		if err != nil {
			if rerr := c.handlePollError(logger, state, token, err); rerr != nil {
				return nil, rerr
			}
			continue
		}

		if token != nil {
			return token, nil
		}
	}
}

// wait blocks until state.interval has elapsed on the wall clock.
//
// A timer counts down on the monotonic clock, which runs faster than real time
// under WSL and other virtualized environments: typically 5-15%, up to 30%. See
// https://github.com/cli/cli/issues/9370 and
// https://github.com/microsoft/WSL/issues/12583. GitHub measures the polling
// interval against real time, so a timer that fires early makes it answer
// slow_down. Once the timer fires, the wall clock says how much of the interval
// is really left, and that remainder is waited out too. Without drift the
// remainder is already zero, so a healthy machine waits exactly once and pays
// nothing for this.
func (c *Client) wait(ctx context.Context, logger *slog.Logger, state *pollState) error {
	start := time.Now()
	timer := time.NewTimer(state.interval)
	defer timer.Stop()

	// budget is spent on the monotonic clock, so a wall clock that stalls or
	// steps backwards cannot keep this loop waiting forever.
	budget := waitCompensationFactor * state.interval
	measured := false

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context was cancelled: %w", ctx.Err())
		case <-timer.C:
		}

		elapsed := c.input.wallSince(start)
		remaining := state.interval - elapsed

		if !measured {
			// The timer fired after state.interval on the monotonic clock, so this
			// first reading is the one that compares the two clocks over a known span.
			measured = true
			state.drift = max(state.drift, driftRatio(state.interval, elapsed))
			if remaining > 0 {
				logger.Debug(
					"the monotonic clock ran ahead of real time, extending the polling interval",
					"interval", state.interval,
					"wall_elapsed", elapsed,
					"remaining", remaining,
					"drift", state.drift,
				)
			}
		}

		if remaining <= 0 {
			return nil
		}

		if spent := time.Since(start); spent+remaining > budget {
			logger.Debug(
				"the wall clock did not advance as expected, polling without waiting further",
				"interval", state.interval,
				"wall_elapsed", elapsed,
				"spent", spent,
			)
			return nil
		}

		timer.Reset(remaining)
	}
}

// driftRatio reports how far the monotonic clock ran ahead of the wall clock,
// as a fraction of the wall clock time that really passed. It is positive when
// the monotonic clock is fast, which is the case that breaks the device flow.
func driftRatio(mono, wall time.Duration) float64 {
	if wall <= 0 {
		return 0
	}
	return float64(mono-wall) / float64(wall)
}

// handlePollError processes an error from GetAccessToken during polling.
// It returns nil when polling should continue, or a non-nil error when polling
// should stop and return that error. On slow_down it also grows the interval
// held in state.
func (c *Client) handlePollError(logger *slog.Logger, state *pollState, token *AccessToken, err error) error {
	if token == nil {
		return err
	}
	switch token.Error {
	case wordAuthPending:
		logger.Debug(
			"device flow's authorization is still pending",
			"error", token.Error,
			"error_description", token.ErrorDescription,
			"error_uri", token.ErrorURI,
		)
		return nil
	case wordSlowDown:
		state.slowDowns++
		logger.Debug(
			"device flow's polling was too frequent, slowing down",
			"error", token.Error,
			"error_description", token.ErrorDescription,
			"error_uri", token.ErrorURI,
			"interval", token.Interval,
			"slow_downs", state.slowDowns,
		)
		if state.slowDowns >= maxSlowDowns {
			return fmt.Errorf(
				"%w (%d times) even though the client waited the requested interval; the monotonic clock ran about %.0f%% ahead of the wall clock, so please check that the system clock is accurate: %w",
				errTooManySlowDowns, state.slowDowns, state.drift*100, err) //nolint:mnd
		}
		// RFC 8628 section 3.5: grow the interval by 5 seconds, unless the server
		// says what to use instead.
		if token.Interval > 0 {
			state.interval = time.Duration(token.Interval)*time.Second + pollIntervalBuffer
		} else {
			state.interval += slowDownIncrement
		}
		return nil
	default:
		return err
	}
}
