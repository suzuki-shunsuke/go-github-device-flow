package deviceflow

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	t.Parallel()
	httpClient := &http.Client{}
	input := newMockInput()
	input.HTTPClient = httpClient
	client := New(input)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.input.HTTPClient != httpClient {
		t.Error("httpClient not set correctly")
	}
}

func newMockInput() *Input {
	return &Input{
		HTTPClient: http.DefaultClient,
	}
}

// testTransport is a custom transport that redirects GitHub API requests to our test server
type testTransport struct {
	server *httptest.Server
	base   http.RoundTripper
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect GitHub API requests to our test server
	if strings.Contains(req.URL.Host, "github.com") {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(t.server.URL, "http://")
	}
	return t.base.RoundTrip(req) //nolint:wrapcheck
}
