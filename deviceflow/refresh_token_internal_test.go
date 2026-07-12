package deviceflow

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestClient_RefreshToken(t *testing.T) { //nolint:gocognit,cyclop,funlen
	t.Parallel()
	tests := []struct {
		name         string
		clientID     string
		refreshToken string
		handler      http.HandlerFunc
		want         *AccessToken
		wantErr      bool
		errMsg       string
	}{
		{
			name:         "successful refresh",
			clientID:     "test-client-id",
			refreshToken: "refresh123",
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
				if req[wordRefreshToken] != "refresh123" {
					t.Errorf("expected refresh_token refresh123, got %s", req[wordRefreshToken])
				}
				if req["grant_type"] != wordRefreshToken {
					t.Errorf("unexpected grant_type: %s", req["grant_type"])
				}

				resp := AccessToken{ //nolint:gosec // test fixture, not a real credential
					AccessToken:           "gho_newtoken123",
					ExpiresIn:             28800,
					RefreshToken:          "ghr_newrefresh123",
					RefreshTokenExpiresIn: 15897600,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errchkjson,errcheck,gosec
			},
			want: &AccessToken{ //nolint:gosec // test fixture, not a real credential
				AccessToken:           "gho_newtoken123",
				ExpiresIn:             28800,
				RefreshToken:          "ghr_newrefresh123",
				RefreshTokenExpiresIn: 15897600,
			},
			wantErr: false,
		},
		{
			name:         "error response with description",
			clientID:     "test-client-id",
			refreshToken: "refresh123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				resp := AccessToken{
					Error:            "bad_refresh_token",
					ErrorDescription: "The refresh token is invalid",
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errchkjson,errcheck,gosec
			},
			want:    nil,
			wantErr: true,
			errMsg:  "The refresh token is invalid",
		},
		{
			name:         "error response without description",
			clientID:     "test-client-id",
			refreshToken: "refresh123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				resp := AccessToken{
					Error: "bad_refresh_token",
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errchkjson,errcheck,gosec
			},
			want:    nil,
			wantErr: true,
			errMsg:  "bad_refresh_token",
		},
		{
			name:         "empty access token",
			clientID:     "test-client-id",
			refreshToken: "refresh123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{}`)) //nolint:errcheck
			},
			want:    nil,
			wantErr: true,
			errMsg:  "access_token is empty",
		},
		{
			name:         "non-200 status",
			clientID:     "test-client-id",
			refreshToken: "refresh123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"invalid_request"}`)) //nolint:errcheck
			},
			want:    nil,
			wantErr: true,
			errMsg:  "status code isn't 200",
		},
		{
			name:         "invalid JSON response",
			clientID:     "test-client-id",
			refreshToken: "refresh123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`invalid json`)) //nolint:errcheck
			},
			want:    nil,
			wantErr: true,
			errMsg:  "unmarshal response body as JSON",
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

			got, _, _, err := client.RefreshToken(t.Context(), tt.clientID, tt.refreshToken) //nolint:bodyclose
			if err != nil {
				if !tt.wantErr {
					t.Fatalf("unexpected error: %v", err)
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %v, want error containing %v", err.Error(), tt.errMsg)
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
