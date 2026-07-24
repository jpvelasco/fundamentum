package github

import (
	"errors"
	"net/http"
	"strings"
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
