package deviceflow

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestClient_pollForAccessToken(t *testing.T) { //nolint:funlen
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
				ExpiresIn:       10,
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
			timeout: 5 * time.Second,
		},
		{
			name:     "context cancelled",
			clientID: "test-client-id",
			deviceCode: &DeviceCodeResponse{
				DeviceCode:      "device123",
				UserCode:        "USER-CODE",
				VerificationURI: "https://github.com/login/device",
				ExpiresIn:       10,
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
			timeout:     5 * time.Millisecond,
		},
		// TODO
		// {
		// 	name:     "slow down handling",
		// 	clientID: "test-client-id",
		// 	deviceCode: &DeviceCodeResponse{
		// 		DeviceCode:      "device123",
		// 		UserCode:        "USER-CODE",
		// 		VerificationURI: "https://github.com/login/device",
		// 		ExpiresIn:       10,
		// 		Interval:        1,
		// 	},
		// 	handler: func() http.HandlerFunc {
		// 		callCount := 0
		// 		return func(w http.ResponseWriter, r *http.Request) {
		// 			callCount++
		// 			if callCount == 1 {
		// 				// First call returns slow_down
		// 				resp := AccessToken{
		// 					Error: "slow_down",
		// 				}
		// 				json.NewEncoder(w).Encode(resp)
		// 			} else {
		// 				// Subsequent calls return success
		// 				resp := AccessToken{
		// 					AccessToken: "gho_testtoken123",
		// 					ExpiresIn:   28800,
		// 				}
		// 				json.NewEncoder(w).Encode(resp)
		// 			}
		// 		}
		// 	}(),
		// 	want: &AccessToken{
		// 		AccessToken: "gho_testtoken123",
		// 		ExpiresIn:   28800,
		// 	},
		// 	wantErr: false,
		// 	timeout: 10 * time.Second,
		// },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			transport := &testTransport{
				server: server,
				base:   http.DefaultTransport,
			}

			input := newMockInput()
			input.HTTPClient = &http.Client{Transport: transport}
			client := New(input)

			ctx := t.Context()
			if tt.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.timeout)
				defer cancel()
			}
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
	}
}
