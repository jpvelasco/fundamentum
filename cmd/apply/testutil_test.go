package apply

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/util"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

// newTestServer creates an httptest.Server with the given handler and returns
// the server plus a github.Client configured to point at it. The caller must
// defer the returned server's Close() call.
func newTestServer(handler http.HandlerFunc) (*httptest.Server, *github.Client) {
	srv := httptest.NewServer(handler)
	return srv, github.NewClient("t", false).WithBaseURL(srv.URL)
}

func buildItemsForTest(
	t *testing.T,
	c *github.Client,
	visibility string,
	rendered []templates.RenderedFile,
	branchPlan, tagPlan github.RulesetPlan,
	classicExists bool,
) []wizard.Item {
	t.Helper()
	items, err := buildItems(c, nil, "owner", "repo", "main", visibility, rendered, branchPlan, tagPlan, classicExists, false, &github.BranchProtectionOptions{}, false, "", io.Discard)
	if err != nil {
		t.Fatalf("buildItems() error: %v", err)
	}
	return items
}

func buildFreeTierPrivateItemsForTest(t *testing.T, c *github.Client, rendered []templates.RenderedFile) []wizard.Item {
	t.Helper()
	items, err := buildItems(c, nil, "owner", "repo", "main", "private", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, true, &github.BranchProtectionOptions{}, false, "", io.Discard)
	if err != nil {
		t.Fatalf("buildItems() error: %v", err)
	}
	return items
}

// stubGhTokenFailure makes token resolution deterministic on dev machines
// with an authenticated gh CLI (the fallback would otherwise provide a token).
func stubGhTokenFailure(t *testing.T) {
	t.Helper()
	restore := util.GhAuthToken
	util.GhAuthToken = func() (string, error) { return "", errors.New("no gh") }
	t.Cleanup(func() { util.GhAuthToken = restore })
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
