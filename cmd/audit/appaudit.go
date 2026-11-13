package audit

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/util"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/steps"
)

const maxAppCredentialBytes = 1 << 20

type appCredential struct {
	source string
	value  string
}

func appCredentials() (map[int64]appCredential, error) {
	out := map[int64]appCredential{}
	for _, group := range []struct {
		source string
		values []string
	}{{"file", globals.AppPrivateKeys}, {"key-env", globals.AppPrivateKeyEnvs}, {"token-env", globals.AppTokenEnvs}} {
		for _, raw := range group.values {
			id, value, ok := strings.Cut(raw, "=")
			appID, err := strconv.ParseInt(id, 10, 64)
			if !ok || err != nil || appID <= 0 || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("app credentials require APP_ID=PATH or APP_ID=ENV_NAME with a positive ID")
			}
			if _, duplicate := out[appID]; duplicate {
				return nil, fmt.Errorf("configure only one credential source per app")
			}
			out[appID] = appCredential{source: group.source, value: value}
		}
	}
	return out, nil
}

func (c appCredential) load() ([]byte, error) {
	var raw []byte
	if c.source == "file" {
		// Reject devices and pipes before opening, then verify the opened file
		// as well in case the path changed between the two operations.
		info, err := os.Stat(c.value)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("app key path must refer to a readable regular file")
		}
		f, err := os.Open(c.value)
		if err != nil {
			return nil, fmt.Errorf("cannot open app key file; check the configured path and permissions")
		}
		defer func() { _ = f.Close() }()
		info, err = f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("app key path must refer to a readable regular file")
		}
		raw, err = io.ReadAll(io.LimitReader(f, maxAppCredentialBytes+1))
		if err != nil {
			return nil, fmt.Errorf("cannot read app key file")
		}
	} else {
		raw = []byte(os.Getenv(c.value))
	}
	if len(raw) > maxAppCredentialBytes || len(strings.TrimSpace(string(raw))) == 0 {
		return nil, fmt.Errorf("app credential is empty or exceeds 1 MiB; check its runtime source")
	}
	return raw, nil
}

func appAuditCheck(client *github.Client, owner, repo string, selector steps.Selector) *check {
	if !globals.AppAuditEnabled() {
		return nil
	}
	result := &check{Name: "app installations", Status: statusSkip}
	if !selector.IncludeStep(steps.AppAudit) {
		result.Detail = "opted out (" + selector.ExcludeReason(steps.AppAudit) + ")"
		return result
	}
	if globals.AppAuditPolicy == nil || len(globals.AppAuditPolicy.ExpectedApps) == 0 {
		result.Detail = "no expected_apps configured; other apps not inventoried"
		return result
	}
	expected := globals.AppAuditPolicy.ExpectedApps
	target := owner + "/" + repo
	if err := validateAppExpectations(expected, target); err != nil {
		result.Status, result.Detail = statusWarn, err.Error()
		return result
	}
	credentials, err := appCredentials()
	if err != nil {
		result.Status, result.Detail = statusWarn, err.Error()
		return result
	}
	if len(credentials) == 0 {
		result.Detail = "no app credentials supplied; named apps not verified"
		return result
	}
	result.Status = statusPass
	for _, app := range expected {
		finding := auditExpectedApp(client, owner, repo, app, credentials[app.AppID])
		addAppFinding(result, finding.Status, fmt.Sprintf("app %d: %s", app.AppID, finding.Detail))
		delete(credentials, app.AppID)
	}
	if len(credentials) > 0 {
		addAppFinding(result, statusWarn, "some credential IDs are absent from expected_apps")
	}
	addAppFinding(result, statusPass, "named apps only; other apps not inventoried")
	return result
}

func validateAppExpectations(apps []globals.ExpectedApp, target string) error {
	seen := map[int64]bool{}
	for _, app := range apps {
		if app.AppID <= 0 || seen[app.AppID] {
			return fmt.Errorf("expected_apps requires unique positive app_id values")
		}
		seen[app.AppID] = true
		for _, level := range app.Permissions {
			if level == "" || permissionLevel(level) < 0 {
				return fmt.Errorf("app permission ceilings must be none, read, write, or admin")
			}
		}
		for _, repo := range app.Repositories {
			if _, _, err := util.ParseOwnerRepo(repo); err != nil {
				return fmt.Errorf("app repository allowances must use OWNER/REPO names")
			}
		}
		if app.Repositories != nil && !containsRepo(app.Repositories, target) {
			return fmt.Errorf("each app repository allowance must include the audited target")
		}
	}
	return nil
}

func auditExpectedApp(client *github.Client, owner, repo string, expected globals.ExpectedApp, credential appCredential) check {
	if credential.source == "" {
		return check{Status: statusWarn, Detail: "no credential supplied"}
	}
	raw, err := credential.load()
	if err != nil {
		return check{Status: statusWarn, Detail: err.Error()}
	}
	if credential.source == "token-env" {
		repos, err := client.AppTokenRepositories(strings.TrimSpace(string(raw)))
		detail := "token-only: identity, grants and full installation scope unverified"
		if err != nil {
			detail += "; " + err.Error()
		} else {
			detail += fmt.Sprintf("; token sees %d repositories, target accessible=%t", len(repos), containsRepo(repos, owner+"/"+repo))
		}
		return check{Status: statusWarn, Detail: detail}
	}
	scope, err := client.AuditAppInstallation(owner, repo, expected.AppID, raw)
	return appScopeCheck(expected, scope, err, owner+"/"+repo)
}

func appScopeCheck(expected globals.ExpectedApp, scope github.AppInstallationScope, scopeErr error, target string) check {
	result := check{Status: statusPass}
	if !scope.Installed {
		if scopeErr != nil {
			return check{Status: statusWarn, Detail: scopeErr.Error()}
		}
		return check{Status: statusFail, Detail: "expected app is not installed for the target"}
	}
	if expected.Permissions == nil {
		addAppFinding(&result, statusWarn, "permission ceiling not configured")
	} else {
		excess, unknown := excessiveAppPermissions(scope.Permissions, expected.Permissions)
		if excess > 0 {
			addAppFinding(&result, statusFail, fmt.Sprintf("%d grants exceed permission ceilings", excess))
		}
		if unknown {
			addAppFinding(&result, statusWarn, "permission evidence is incomplete or unrecognized")
		}
	}
	if scopeErr != nil {
		addAppFinding(&result, statusWarn, scopeErr.Error())
		return result
	}
	if !containsRepo(scope.Repositories, target) {
		addAppFinding(&result, statusWarn, "target absent from repository list; scope evidence inconsistent")
		return result
	}
	allowed := expected.Repositories
	if allowed == nil {
		allowed = []string{target}
	}
	extra := 0
	for _, repo := range scope.Repositories {
		if !containsRepo(allowed, repo) {
			extra++
		}
	}
	if extra > 0 {
		addAppFinding(&result, statusFail, fmt.Sprintf("%d repositories outside allowed set", extra))
	}
	if scope.RepositorySelection == "all" {
		addAppFinding(&result, statusWarn, "all-repository selection also grants future repositories")
	}
	if result.Status == statusPass {
		result.Detail = "grants and selected repositories within policy"
	}
	return result
}

func excessiveAppPermissions(actual, ceilings map[string]string) (excess int, unknown bool) {
	unknown = actual == nil
	for name, level := range actual {
		ceiling, configured := ceilings[name]
		if name == "metadata" && !configured {
			ceiling = "read"
		}
		rank := permissionLevel(level)
		if level == "" || rank < 0 {
			unknown = true
		} else if rank > permissionLevel(ceiling) {
			excess++
		}
	}
	return excess, unknown
}

func permissionLevel(level string) int {
	switch level {
	case "", "none":
		return 0
	case "read":
		return 1
	case "write":
		return 2
	case "admin":
		return 3
	default:
		return -1
	}
}

func containsRepo(repos []string, target string) bool {
	return slices.ContainsFunc(repos, func(repo string) bool { return strings.EqualFold(repo, target) })
}

func addAppFinding(result *check, severity status, detail string) {
	if severity == statusFail || (severity == statusWarn && result.Status != statusFail) {
		result.Status = severity
	}
	if result.Detail != "" {
		result.Detail += "; "
	}
	result.Detail += detail
}
