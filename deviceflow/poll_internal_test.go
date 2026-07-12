package deviceflow

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestClient_Poll(t *testing.T) { //nolint:funlen,gocognit
	t.Parallel()
	tests := []struct {
		name        string
		clientID    string
		deviceCode  *DeviceCodeResponse
		handler     http.HandlerFunc
		want        *AccessToken
		wantErr     bool
		errContains string
		timeout     time.Duration
	}{
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
						// handlePollError takes the 10s fallback reset. The fake
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
				logger := slog.New(slog.DiscardHandler)

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
