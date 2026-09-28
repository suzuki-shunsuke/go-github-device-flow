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
	// slowDownIncrement is how much the base interval grows on slow_down, per RFC 8628 section 3.5.
	slowDownIncrement = 5 * time.Second
	// maxSlowDowns is how many slow_down responses Poll tolerates before giving up.
	maxSlowDowns = 3
	// slowDownFactorNumerator and slowDownFactorDenominator make up the 1.3
	// safety factor applied once per slow_down. See pollState.wait.
	slowDownFactorNumerator   = 13
	slowDownFactorDenominator = 10
)

// pollState carries the mutable state of one Poll call.
type pollState struct {
	// interval is the polling interval GitHub asked for. It grows by 5 seconds
	// on every slow_down, as RFC 8628 section 3.5 requires. It does not include
	// pollIntervalBuffer, which wait adds.
	interval time.Duration
	// slowDowns counts the slow_down responses received so far. It also decides
	// the safety factor applied by wait.
	slowDowns int
	// start is when Poll began. It keeps its monotonic reading, so both clocks
	// can be measured over the same span to report how far they disagree.
	start time.Time
}

// wait returns how long to wait before the next poll.
//
// Timers count down on the monotonic clock, which runs faster than real time
// under WSL and other virtualized environments: typically 5-15%, up to 30%. See
// https://github.com/microsoft/WSL/issues/12583. GitHub measures the polling
// interval against real time, so a timer that fires early makes it answer
// slow_down, and the 5 seconds RFC 8628 asks to add never catches up: the
// client falls short by a fraction of whatever GitHub requires, and adding a
// constant does not close a proportional gap. GitHub raises its own interval by
// the same 5 seconds, so both sides escalate in lockstep and the client stays
// short forever.
//
// A proportional error needs a proportional correction, so each slow_down also
// multiplies the wait by 1.3. One slow_down therefore covers a monotonic clock
// up to 30% fast, which is the whole range reported for WSL, and two cover 69%.
// A machine with an accurate clock never receives slow_down, so it keeps
// polling at exactly the interval GitHub asked for and pays nothing for this.
func (s *pollState) wait() time.Duration {
	d := s.interval + pollIntervalBuffer
	for range s.slowDowns {
		d = d * slowDownFactorNumerator / slowDownFactorDenominator
	}
	return d
}

// clockDrift reports how far the monotonic clock ran ahead of the wall clock
// since Poll started, as a fraction of the wall clock time that passed.
//
// It only explains a failure and never steers the polling. A fast monotonic
// clock is what makes GitHub answer slow_down, but a wall clock stepped by NTP
// or by a resume from suspend shows up here just the same, so this number says
// the two clocks disagree, not which one is wrong. Measuring over the whole
// Poll rather than over one interval keeps a single step from dominating it.
func (s *pollState) clockDrift() float64 {
	// time.Since reads the monotonic part of s.start, while UnixNano reads its
	// wall clock part.
	mono := time.Since(s.start)
	wall := time.Duration(time.Now().UnixNano() - s.start.UnixNano())
	if wall <= 0 {
		return 0
	}
	return float64(mono-wall) / float64(wall)
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
		interval: max(time.Duration(deviceCode.Interval)*time.Second, minPollInterval),
		start:    time.Now(),
	}

	ticker := time.NewTicker(state.wait())
	defer ticker.Stop()

	deadline := state.start.Add(time.Duration(deviceCode.ExpiresIn) * time.Second)

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context was cancelled: %w", ctx.Err())
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, errors.New("device code expired")
			}

			token, _, _, err := c.GetAccessToken(ctx, clientID, deviceCode.DeviceCode, input) //nolint:bodyclose
			if err != nil {
				if rerr := c.handlePollError(logger, ticker, state, token, err); rerr != nil {
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
// should stop and return that error. On slow_down it also grows the interval
// held in state and resets the ticker.
func (c *Client) handlePollError(logger *slog.Logger, ticker *time.Ticker, state *pollState, token *AccessToken, err error) error {
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
				"%w (%d times) even though the client waited much longer than GitHub asked for; the monotonic and wall clocks disagree by about %.0f%%, so please check that the system clock is accurate: %w",
				errTooManySlowDowns, state.slowDowns, state.clockDrift()*100, err) //nolint:mnd
		}
		// RFC 8628 section 3.5 requires growing the interval by 5 seconds.
		// GitHub also returns the interval it wants, so honour whichever is larger.
		state.interval = max(state.interval+slowDownIncrement, time.Duration(token.Interval)*time.Second)
		ticker.Reset(state.wait())
		return nil
	default:
		return err
	}
}
