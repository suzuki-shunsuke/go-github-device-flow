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
						// handlePollError falls back to growing the interval by
						// the 5s of RFC 8628. The fake clock makes that instant.
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

			// synctest runs Poll under a fake clock so the ticker, ticker
			// resets, and context deadline advance deterministically without
			// real waiting. Keep-alives are disabled so no connection goroutine
			// lingers inside the synctest bubble.
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

// recordingTransport records when each request is made, on the fake clock of
// the synctest bubble it runs in, so a test can assert how long Poll waited
// between polls.
type recordingTransport struct {
	base  http.RoundTripper
	times []time.Time
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.times = append(t.times, time.Now())
	return t.base.RoundTrip(req) //nolint:wrapcheck
}

// TestClient_Poll_slowDown covers the safety factor applied on slow_down. A
// monotonic clock that runs fast makes every wait fall short of what GitHub
// requires by a fraction of it, so the wait is multiplied by 1.3 once per
// slow_down. After enough of them Poll gives up instead of polling until the
// device code expires.
func TestClient_Poll_slowDown(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(AccessToken{Error: "slow_down"}) //nolint:errcheck,gosec
	}))
	defer server.Close()

	synctest.Test(t, func(t *testing.T) {
		transport := &recordingTransport{
			base: &testTransport{
				server: server,
				base:   &http.Transport{DisableKeepAlives: true},
			},
		}
		client := New(&Input{HTTPClient: &http.Client{Transport: transport}})

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()

		start := time.Now()
		if _, err := client.Poll(ctx, slog.New(slog.DiscardHandler), "test-client-id", &DeviceCodeResponse{
			DeviceCode:      "device123",
			UserCode:        "USER-CODE",
			VerificationURI: "https://github.com/login/device",
			ExpiresIn:       900,
			Interval:        1,
		}, nil); !errors.Is(err, errTooManySlowDowns) {
			t.Fatalf("error = %v, want %v", err, errTooManySlowDowns)
		}

		// The base interval is GitHub's 5s minimum plus the 100ms buffer, and it
		// grows by the 5s of RFC 8628 section 3.5 on every slow_down. On top of
		// that, the wait is multiplied by 1.3 once per slow_down received so far.
		want := []time.Duration{
			5100 * time.Millisecond,  // no slow_down yet
			13130 * time.Millisecond, // 10.1s * 1.3
			25519 * time.Millisecond, // 15.1s * 1.3 * 1.3
		}
		got := make([]time.Duration, 0, len(transport.times))
		prev := start
		for _, tm := range transport.times {
			got = append(got, tm.Sub(prev))
			prev = tm
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("polling intervals mismatch (-want +got):\n%s", diff)
		}
	})
}
