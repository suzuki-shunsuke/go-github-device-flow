package deviceflow

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestClient_GetAccessToken(t *testing.T) { //nolint:gocognit,cyclop,funlen
	t.Parallel()
	tests := []struct {
		name       string
		clientID   string
		deviceCode string
		handler    http.HandlerFunc
		want       *AccessToken
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "successful token response",
			clientID:   "test-client-id",
			deviceCode: "device123",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/login/oauth/access_token" {
					t.Errorf("expected /login/oauth/access_token, got %s", r.URL.Path)
				}

				body, _ := io.ReadAll(r.Body)
				var req map[string]string
				if err := json.Unmarshal(body, &req); err != nil {
					t.Errorf("failed to unmarshal request body: %v", err)
				}

				if req["client_id"] != "test-client-id" {
					t.Errorf("expected client_id test-client-id, got %s", req["client_id"])
				}
				if req["device_code"] != "device123" {
					t.Errorf("expected device_code device123, got %s", req["device_code"])
				}
				if req["grant_type"] != "urn:ietf:params:oauth:grant-type:device_code" {
					t.Errorf("unexpected grant_type: %s", req["grant_type"])
				}

				resp := AccessToken{
					AccessToken: "gho_testtoken123",
					ExpiresIn:   28800,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errchkjson,errcheck,gosec
			},
			want: &AccessToken{
				AccessToken: "gho_testtoken123",
				ExpiresIn:   28800,
			},
			wantErr: false,
		},
		{
			name:       "authorization pending",
			clientID:   "test-client-id",
			deviceCode: "device123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				resp := AccessToken{
					Error: "authorization_pending",
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errchkjson,errcheck,gosec
			},
			want:    nil,
			wantErr: true,
			errMsg:  "authorization_pending",
		},
		{
			name:       "slow down response",
			clientID:   "test-client-id",
			deviceCode: "device123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				resp := AccessToken{
					Error: "slow_down",
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck,errchkjson,gosec
			},
			want:    nil,
			wantErr: true,
			errMsg:  "slow_down",
		},
		{
			name:       "access denied",
			clientID:   "test-client-id",
			deviceCode: "device123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				resp := AccessToken{
					Error: "access_denied",
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck,errchkjson,gosec
			},
			want:    nil,
			wantErr: true,
			errMsg:  "access_denied",
		},
		{
			name:       "empty response",
			clientID:   "test-client-id",
			deviceCode: "device123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{}`)) //nolint:errcheck
			},
			want:    nil,
			wantErr: true,
			errMsg:  "access_token is empty",
		},
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

			got, _, _, err := client.GetAccessToken(t.Context(), tt.clientID, tt.deviceCode, nil) //nolint:bodyclose
			if err != nil {
				if !tt.wantErr {
					t.Fatalf("unexpected error: %v", err)
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("error = %v, want %v", err.Error(), tt.errMsg)
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
