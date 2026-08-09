package apply

import (
	"net/http"
	"net/http/httptest"

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