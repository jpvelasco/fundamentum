package apply

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
)

// newTestServer creates an httptest.Server with the given handler and returns
// the server plus a github.Client configured to point at it. The caller must
// defer the returned server's Close() call.
func newTestServer(handler http.HandlerFunc) (*httptest.Server, *github.Client) {
	srv := httptest.NewServer(handler)
	return srv, github.NewClient("t", false).WithBaseURL(srv.URL)
}

// newTestClient returns a github.Client pointed at the given server URL.
func newTestClient(srv *httptest.Server) *github.Client {
	return github.NewClient("t", false).WithBaseURL(srv.URL)
}

// testWithServer creates a test server with the given handler, builds a client
// pointed at it, runs the action, and calls verify with the results. The server
// is closed automatically. This replaces the common srv/defer/call/assert
// skeleton across table-driven tests.
func testWithServer(t *testing.T, handler http.HandlerFunc, action func(*github.Client), verify func(*testing.T)) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	action(c)
	if verify != nil {
		verify(t)
	}
}