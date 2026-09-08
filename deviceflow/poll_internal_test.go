package deviceflow

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestClient_Poll(t *testing.T) { //nolint:funlen,gocognit,cyclop
	t.Parallel()
	tests := []struct {
		name        string
		clientID    string
		deviceCode  *DeviceCodeResponse
		handler     http.HandlerFunc
		nilLogger   bool
		want        *AccessToken
		wantErr     bool
		errContains string
		timeout     time.Duration
	}{
		{
			name:     "nil logger does not panic",
			clientID: "test-client-id",
			deviceCode: &DeviceCodeResponse{
				DeviceCode:      "device123",
				UserCode:        "USER-CODE",
				VerificationURI: "https://github.com/login/device",
				ExpiresIn:       900,
				Interval:        1,
			},
			// A nil logger must be tolerated: the first poll returns
			// authorization_pending, which exercises the logger.Debug path in
			// handlePollError before the second poll succeeds.
			nilLogger: true,
			handler: func() http.HandlerFunc {
				callCount := 0
				return func(w http.ResponseWriter, _ *http.Request) {
					callCount++
					if callCount == 1 {
						json.NewEncoder(w).Encode(AccessToken{Error: "authorization_pending"}) //nolint:errcheck,gosec
					} else {
						json.NewEncoder(w).Encode(AccessToken{ //nolint:errcheck,gosec
							AccessToken: "gho_testtoken123",
							ExpiresIn:   28800,
						})
					}
				}
			}(),
			want: &AccessToken{
				AccessToken: "gho_testtoken123",
				ExpiresIn:   28800,
			},
			wantErr: false,
			timeout: 300 * time.Second,
		},
		{
			name:       "nil device code returns error",
			clientID:   "test-client-id",
			deviceCode: nil,
			handler: func(_ http.ResponseWriter, _ *http.Request) {
				t.Error("handler should not be called with nil device code")
			},
			want:        nil,
			wantErr:     true,
			errContains: "device code is required",
			timeout:     300 * time.Second,
		},
		{
			name:     "successful after one poll",
			clientID: "test-client-id",
			deviceCode: &DeviceCodeResponse{
				DeviceCode:      "device123",
				UserCode:        "USER-CODE",
				VerificationURI: "https://github.com/login/device",
				ExpiresIn:       900,
				Interval:        1,
			},
			handler: func() http.HandlerFunc {
				callCount := 0
				return func(w http.ResponseWriter, _ *http.Request) {
					callCount++
					if callCount == 1 {
						// First call returns pending
						resp := AccessToken{
							Error: "authorization_pending",
						}
						json.NewEncoder(w).Encode(resp) //nolint:errcheck,gosec
					} else {
						// Second call returns success
						resp := AccessToken{
							AccessToken: "gho_testtoken123",
							ExpiresIn:   28800,
						}
						json.NewEncoder(w).Encode(resp) //nolint:errcheck,gosec
					}
				}
			}(),
			want: &AccessToken{
				AccessToken: "gho_testtoken123",
				ExpiresIn:   28800,
			},
			wantErr: false,
			timeout: 300 * time.Second,
		},
		{
			name:     "context cancelled",
			clientID: "test-client-id",
			deviceCode: &DeviceCodeResponse{
				DeviceCode:      "device123",
				UserCode:        "USER-CODE",
				VerificationURI: "https://github.com/login/device",
				ExpiresIn:       900,
				Interval:        1,
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				resp := AccessToken{
					Error: "authorization_pending",
				}
				json.NewEncoder(w).Encode(resp) //nolint:errcheck,errchkjson,gosec
			},
			want:        nil,
			wantErr:     true,
			errContains: "context was cancelled",
			// Shorter than the 5s minimum poll interval, so the context
			// deadline fires before the first poll.
			timeout: time.Second,
		},
		{
			name:     "slow down handling",
			clientID: "test-client-id",
			deviceCode: &DeviceCodeResponse{
				DeviceCode:      "device123",
				UserCode:        "USER-CODE",
				VerificationURI: "https://github.com/login/device",
				ExpiresIn:       900,
				Interval:        1,
			},
			handler: func() http.HandlerFunc {
				callCount := 0
				return func(w http.ResponseWriter, _ *http.Request) {
					callCount++
					if callCount == 1 {
						// First call returns slow_down with no interval, so
						// handlePollError grows the interval by 5s. The fake
						// clock makes that instant.
						resp := AccessToken{
							Error: "slow_down",
						}
						json.NewEncoder(w).Encode(resp) //nolint:errcheck,gosec
					} else {
						// Subsequent calls return success
						resp := AccessToken{
							AccessToken: "gho_testtoken123",
							ExpiresIn:   28800,
						}
						json.NewEncoder(w).Encode(resp) //nolint:errcheck,gosec
					}
				}
			}(),
			want: &AccessToken{
				AccessToken: "gho_testtoken123",
				ExpiresIn:   28800,
			},
			wantErr: false,
			timeout: 300 * time.Second,
		},
		{
			name:     "non-200 stops polling",
			clientID: "test-client-id",
			deviceCode: &DeviceCodeResponse{
				DeviceCode:      "device123",
				UserCode:        "USER-CODE",
				VerificationURI: "https://github.com/login/device",
				ExpiresIn:       900,
				Interval:        1,
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				// A non-200 response makes GetAccessToken return a nil token
				// with an error; polling must stop and surface it.
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"invalid_request"}`)) //nolint:errcheck
			},
			want:        nil,
			wantErr:     true,
			errContains: "status code isn't 200",
			timeout:     300 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			// synctest runs Poll under a fake clock so the poll timer, the
			// interval growth, and the context deadline advance
			// deterministically without real waiting. Keep-alives are disabled so
			// no connection goroutine lingers inside the synctest bubble.
			synctest.Test(t, func(t *testing.T) {
				transport := &testTransport{
					server: server,
					base:   &http.Transport{DisableKeepAlives: true},
				}
				input := &Input{
					HTTPClient: &http.Client{Transport: transport},
				}
				client := New(input)

				ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
				defer cancel()
				var logger *slog.Logger
				if !tt.nilLogger {
					logger = slog.New(slog.DiscardHandler)
				}

				got, err := client.Poll(ctx, logger, tt.clientID, tt.deviceCode, nil)
				if err != nil {
					if !tt.wantErr {
						t.Fatalf("unexpected error: %v", err)
					}
					if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
						t.Errorf("error = %v, want error containing %v", err, tt.errContains)
					}
					return
				}
				if tt.wantErr {
					t.Fatalf("expected error but got nil")
					return
				}
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("AccessToken mismatch (-want +got):\n%s", diff)
				}
			})
		})
	}
}

// TestClient_Poll_clockDrift covers the wall clock compensation in wait. The
// synctest clock advances wall and monotonic time together, so the drift a
// virtualized environment introduces is simulated by replacing Input.wallSince
// with one that reports less than the monotonic clock did.
func TestClient_Poll_clockDrift(t *testing.T) { //nolint:funlen
	t.Parallel()

	// deviceCode asks for a 1s interval, which Poll floors at minPollInterval and
	// pads with pollIntervalBuffer, so every wait below starts from 5.1s.
	const interval = minPollInterval + pollIntervalBuffer

	tests := []struct {
		name string
		// wallRatio is the fraction of the monotonic elapsed time that the wall
		// clock reports. 1.0 is a healthy machine; 0.8 is a monotonic clock
		// running 25% fast.
		wallRatio   float64
		handler     http.HandlerFunc
		wantElapsed time.Duration
		want        *AccessToken
		wantErr     error
		errContains string
	}{
		{
			name:      "no drift waits exactly one interval",
			wallRatio: 1,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				json.NewEncoder(w).Encode(AccessToken{AccessToken: "gho_testtoken123"}) //nolint:errcheck,errchkjson,gosec
			},
			wantElapsed: interval,
			want:        &AccessToken{AccessToken: "gho_testtoken123"},
		},
		{
			name: "fast monotonic clock keeps the full wall clock interval",
			// The wait must stretch to interval/0.8 = 6.375s of monotonic time so
			// that GitHub, which judges by real time, still sees a full 5.1s gap.
			wallRatio: 0.8,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				json.NewEncoder(w).Encode(AccessToken{AccessToken: "gho_testtoken123"}) //nolint:errcheck,errchkjson,gosec
			},
			wantElapsed: 6375 * time.Millisecond,
			want:        &AccessToken{AccessToken: "gho_testtoken123"},
		},
		{
			name: "stalled wall clock gives up instead of hanging",
			// A wall clock that never advances would make the compensation loop
			// run forever, so waitCompensationFactor caps one wait at 2 intervals.
			wallRatio: 0,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				json.NewEncoder(w).Encode(AccessToken{AccessToken: "gho_testtoken123"}) //nolint:errcheck,errchkjson,gosec
			},
			wantElapsed: waitCompensationFactor * interval,
			want:        &AccessToken{AccessToken: "gho_testtoken123"},
		},
		{
			name: "too many slow_down responses stop polling",
			// Waiting longer is not what is missing once the client has honoured
			// the interval on the wall clock, so Poll reports the drift it
			// measured instead of retrying until the device code expires.
			wallRatio: 1,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				json.NewEncoder(w).Encode(AccessToken{Error: "slow_down"}) //nolint:errcheck,errchkjson,gosec
			},
			wantErr:     errTooManySlowDowns,
			errContains: "system clock is accurate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			synctest.Test(t, func(t *testing.T) {
				transport := &testTransport{
					server: server,
					base:   &http.Transport{DisableKeepAlives: true},
				}
				client := New(&Input{
					HTTPClient: &http.Client{Transport: transport},
					wallSince: func(start time.Time) time.Duration {
						return time.Duration(float64(time.Since(start)) * tt.wallRatio)
					},
				})

				deviceCode := &DeviceCodeResponse{
					DeviceCode:      "device123",
					UserCode:        "USER-CODE",
					VerificationURI: "https://github.com/login/device",
					ExpiresIn:       900,
					Interval:        1,
				}

				start := time.Now()
				got, err := client.Poll(t.Context(), slog.New(slog.DiscardHandler), "test-client-id", deviceCode, nil)
				elapsed := time.Since(start)

				if tt.wantErr != nil {
					if !errors.Is(err, tt.wantErr) {
						t.Fatalf("error = %v, want %v", err, tt.wantErr)
					}
					if !strings.Contains(err.Error(), tt.errContains) {
						t.Errorf("error = %v, want error containing %v", err, tt.errContains)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("AccessToken mismatch (-want +got):\n%s", diff)
				}
				// The compensation loop converges on the target rather than
				// landing on it exactly, so allow a millisecond of slack.
				if d := elapsed - tt.wantElapsed; d < -time.Millisecond || d > time.Millisecond {
					t.Errorf("elapsed = %v, want about %v", elapsed, tt.wantElapsed)
				}
			})
		})
	}
}
