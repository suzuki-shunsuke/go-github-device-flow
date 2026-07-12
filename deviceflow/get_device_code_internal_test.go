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

func TestClient_GetDeviceCode(t *testing.T) { //nolint:cyclop,funlen
	t.Parallel()
	tests := []struct {
		name        string
		clientID    string
		handler     http.HandlerFunc
		want        *DeviceCodeResponse
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful device code request",
			clientID: "test-client-id",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/login/device/code" {
					t.Errorf("expected /login/device/code, got %s", r.URL.Path)
				}

				body, _ := io.ReadAll(r.Body)
				var req map[string]string
				if err := json.Unmarshal(body, &req); err != nil {
					t.Errorf("failed to unmarshal request body: %v", err)
				}
				if req["client_id"] != "test-client-id" {
					t.Errorf("expected client_id test-client-id, got %s", req["client_id"])
				}

				resp := DeviceCodeResponse{
					DeviceCode:      "device123",
					UserCode:        "USER-CODE",
					VerificationURI: "https://github.com/login/device",
					ExpiresIn:       900,
					Interval:        5,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errchkjson,errcheck
			},
			want: &DeviceCodeResponse{
				DeviceCode:      "device123",
				UserCode:        "USER-CODE",
				VerificationURI: "https://github.com/login/device",
				ExpiresIn:       900,
				Interval:        5,
			},
			wantErr: false,
		},
		{
			name:     "error response from GitHub",
			clientID: "test-client-id",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"invalid_client","error_description":"The client_id is not valid"}`)) //nolint:errcheck
			},
			want:        nil,
			wantErr:     true,
			errContains: "status code isn't 200",
		},
		{
			name:     "invalid JSON response",
			clientID: "test-client-id",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`invalid json`)) //nolint:errcheck
			},
			want:        nil,
			wantErr:     true,
			errContains: "unmarshal response body as JSON",
		},
		{
			name:     "empty client ID",
			clientID: "",
			handler: func(_ http.ResponseWriter, _ *http.Request) {
				// Should not be called
				t.Error("handler should not be called with empty client ID")
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			// Override the GitHub API URL in the test
			originalURL := "https://github.com/login/device/code"
			_ = originalURL // We'll need to modify the actual implementation to make URL configurable

			input := newMockInput()
			input.HTTPClient = server.Client()
			client := &Client{
				input: input,
			}

			// Create a custom transport that redirects requests
			transport := &testTransport{
				server: server,
				base:   http.DefaultTransport,
			}
			client.input.HTTPClient = &http.Client{Transport: transport}

			got, _, _, err := client.GetDeviceCode(t.Context(), tt.clientID) //nolint:bodyclose
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
				t.Errorf("DeviceCodeResponse mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
