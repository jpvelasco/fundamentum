package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// AppInstallationScope contains grants verified with the named app's JWT.
// A non-nil error can accompany metadata when repository scope is incomplete.
type AppInstallationScope struct {
	Installed           bool
	Permissions         map[string]string
	RepositorySelection string
	Repositories        []string
}

// appClient preserves transport/base URL for tests without changing the primary
// repo credential. App requests never log credential-dependent response data.
func (c *Client) appClient(token string) *Client {
	app := *c
	app.Token, app.Verbose = token, false
	transport := *c.client
	transport.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	app.client = &transport
	return &app
}

// appRequest deliberately omits response bodies and transport/decoder messages:
// those can echo authentication material, including a newly minted token.
func (c *Client) appRequest(method, path, action string, body, out any, want int) error {
	resp, err := c.do(method, path, body)
	if err != nil {
		return fmt.Errorf("%s: request failed; check connectivity and app credentials", action)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		return &HTTPError{StatusCode: resp.StatusCode, Msg: fmt.Sprintf("%s: HTTP %d; check app credentials and access", action, resp.StatusCode)}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("%s: invalid response; could not verify app scope", action)
	}
	return nil
}

// AuditAppInstallation reads a named app's installation grants and mints only a
// metadata-read token to enumerate its full repository selection. It changes no
// installation settings, repository permissions, or repository contents.
func (c *Client) AuditAppInstallation(owner, repo string, appID int64, keyPEM []byte) (AppInstallationScope, error) {
	var scope AppInstallationScope
	jwt, err := appJWT(appID, keyPEM, time.Now())
	if err != nil {
		return scope, err
	}
	app := c.appClient(jwt)
	var identity struct{ ID int64 }
	if err := app.appRequest(http.MethodGet, "/app", "identify app", nil, &identity, http.StatusOK); err != nil {
		return scope, err
	}
	if identity.ID != appID {
		return scope, fmt.Errorf("authenticated app does not match expected app ID")
	}
	var installation struct {
		ID                  int64
		AppID               int64 `json:"app_id"`
		Account             struct{ Login string }
		Permissions         map[string]string
		RepositorySelection string  `json:"repository_selection"`
		SuspendedAt         *string `json:"suspended_at"`
	}
	err = app.appRequest(http.MethodGet, repoPath(owner, repo)+"/installation", "read app installation", nil, &installation, http.StatusOK)
	if hasStatusCode(err, http.StatusNotFound) {
		return scope, nil
	}
	if err != nil {
		return scope, err
	}
	if installation.ID <= 0 || installation.AppID != appID || !strings.EqualFold(installation.Account.Login, owner) || installation.Permissions == nil {
		return scope, fmt.Errorf("installation metadata is incomplete or does not match the expected app/account")
	}
	scope = AppInstallationScope{Installed: true, Permissions: installation.Permissions, RepositorySelection: installation.RepositorySelection}
	if installation.SuspendedAt != nil {
		return scope, fmt.Errorf("installation is suspended; repository scope not verified")
	}
	if scope.RepositorySelection != "all" && scope.RepositorySelection != "selected" {
		return scope, fmt.Errorf("installation repository selection is unknown")
	}
	var minted struct {
		Token       string
		Permissions map[string]string
	}
	body := map[string]any{"permissions": map[string]string{"metadata": "read"}}
	path := "/app/installations/" + strconv.FormatInt(installation.ID, 10) + "/access_tokens"
	if err := app.appRequest(http.MethodPost, path, "create metadata-only app token", body, &minted, http.StatusCreated); err != nil {
		return scope, err
	}
	if minted.Token == "" || len(minted.Permissions) != 1 || minted.Permissions["metadata"] != "read" {
		return scope, fmt.Errorf("metadata-only token grant could not be verified")
	}
	scope.Repositories, err = c.AppTokenRepositories(minted.Token)
	return scope, err
}

// AppTokenRepositories verifies only this token's visible repositories. A
// pre-minted token may be narrower than the installation that issued it.
func (c *Client) AppTokenRepositories(token string) ([]string, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("installation token is empty")
	}
	app := c.appClient(token)
	var repos []string
	seen := map[string]bool{}
	total := -1
	for page := 1; ; page++ {
		var list struct {
			TotalCount   *int `json:"total_count"`
			Repositories []struct {
				FullName string `json:"full_name"`
			}
		}
		// Generate pages locally. Never forward credentials to a Link URL.
		path := "/installation/repositories?per_page=100&page=" + strconv.Itoa(page)
		if err := app.appRequest(http.MethodGet, path, "list app token repositories", nil, &list, http.StatusOK); err != nil {
			return nil, err
		}
		if list.TotalCount == nil || list.Repositories == nil || *list.TotalCount < 0 || (total >= 0 && total != *list.TotalCount) {
			return nil, fmt.Errorf("repository count is missing or changed during app audit")
		}
		total = *list.TotalCount
		for _, repo := range list.Repositories {
			name := strings.ToLower(repo.FullName)
			if name == "" || seen[name] {
				return nil, fmt.Errorf("app repository list contains missing or repeated entries")
			}
			seen[name] = true
			repos = append(repos, repo.FullName)
		}
		if len(repos) == total {
			return repos, nil
		}
		if len(repos) > total || len(list.Repositories) == 0 {
			return nil, fmt.Errorf("app repository list is incomplete or inconsistent")
		}
	}
}
