package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// erroringTransport is an http.RoundTripper that always fails, simulating a
// network-level error (DNS failure, connection refused, timeout) as opposed
// to an HTTP response with an error status code.
type erroringTransport struct{}

func (erroringTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("simulated network error")
}

// newErroringClient returns a Client whose requests always fail at the
// transport level, for exercising err != nil branches after c.get/do/patch/post.
func newErroringClient() *Client {
	c := NewClient("t", false)
	c.client = &http.Client{Transport: erroringTransport{}}
	return c
}

// pathFailingTransport forwards every request to base except ones whose path
// contains failOnSubstr, which fail at the transport level. Used to reach a
// network-error branch that only triggers on a later call in a sequence
// (e.g. enableCodeQL's PATCH, after EnableSecurity's earlier calls succeed).
type pathFailingTransport struct {
	base         http.RoundTripper
	failOnSubstr string
}

func (t pathFailingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, t.failOnSubstr) {
		return nil, errors.New("simulated network error")
	}
	return t.base.RoundTrip(req)
}

// newSplitTransportClient returns a Client pointed at baseURL whose requests
// all succeed except ones whose path contains failOnSubstr, which fail at
// the transport level.
func newSplitTransportClient(baseURL, failOnSubstr string) *Client {
	c := NewClient("t", false).WithBaseURL(baseURL)
	c.client = &http.Client{Transport: pathFailingTransport{base: http.DefaultTransport, failOnSubstr: failOnSubstr}}
	return c
}

// methodFailingTransport forwards every request to base except ones using
// failOnMethod, which fail at the transport level. Used when GET and PUT/POST
// share the same URL path and only the method distinguishes the call to fail.
type methodFailingTransport struct {
	base         http.RoundTripper
	failOnMethod string
}

func (t methodFailingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == t.failOnMethod {
		return nil, errors.New("simulated network error")
	}
	return t.base.RoundTrip(req)
}

// newMethodSplitTransportClient returns a Client pointed at baseURL whose
// requests all succeed except ones using failOnMethod, which fail at the
// transport level.
func newMethodSplitTransportClient(baseURL, failOnMethod string) *Client {
	c := NewClient("t", false).WithBaseURL(baseURL)
	c.client = &http.Client{Transport: methodFailingTransport{base: http.DefaultTransport, failOnMethod: failOnMethod}}
	return c
}

// newTestServer creates an httptest.Server with the given handler and returns
// the server plus a Client configured to point at it with retry backoff
// disabled (retry timing is only under test in the dedicated retry tests). The
// caller must defer the returned server's Close() call.
func newTestServer(handler http.HandlerFunc) (*httptest.Server, *Client) {
	srv := httptest.NewServer(handler)
	return srv, newZeroDelayClient(srv.URL)
}

// clientFactory returns a test client for the given server URL. When nil,
// the default zero-delay client is used. Non-nil factories allow tests to
// inject erroring or split-transport clients.
type clientFactory func(baseURL string) *Client

// testWithServer creates a test server with the given handler, builds a client
// (using the factory if provided, or the default zero-delay client), runs the
// action, and calls verify with the results. The server is closed automatically.
// This replaces the common srv/defer/call/assert skeleton across table-driven tests.
func testWithServer(t *testing.T, handler http.HandlerFunc, factory clientFactory, action func(*Client), verify func(*testing.T)) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	c := newZeroDelayClient(srv.URL)
	if factory != nil {
		c = factory(srv.URL)
	}
	action(c)
	if verify != nil {
		verify(t)
	}
}
