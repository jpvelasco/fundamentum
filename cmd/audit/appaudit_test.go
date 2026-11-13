package audit

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/steps"
)

func resetAppAudit(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		globals.AppAudit = false
		globals.AppAuditPolicy = nil
		globals.AppPrivateKeys = nil
		globals.AppPrivateKeyEnvs = nil
		globals.AppTokenEnvs = nil
		globals.BotManaged = false
		globals.Strict = false
		globals.ResetExplicit()
	})
}

func TestAppScopePolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		grants    map[string]string
		repos     []string
		selection string
		installed bool
		err       error
		policy    map[string]string
		want      status
	}{
		{"least privilege", map[string]string{"metadata": "read", "contents": "read"}, []string{"OWNER/REPO"}, "selected", true, nil, map[string]string{"contents": "write"}, statusPass},
		{"excess grant", map[string]string{"contents": "admin"}, []string{"owner/repo"}, "selected", true, nil, map[string]string{"contents": "read"}, statusFail},
		{"unexpected grant", map[string]string{"workflows": "write"}, []string{"owner/repo"}, "selected", true, nil, map[string]string{}, statusFail},
		{"unknown level", map[string]string{"contents": "future"}, []string{"owner/repo"}, "selected", true, nil, map[string]string{}, statusWarn},
		{"empty level", map[string]string{"contents": ""}, []string{"owner/repo"}, "selected", true, nil, map[string]string{}, statusWarn},
		{"missing grants", nil, []string{"owner/repo"}, "selected", true, nil, map[string]string{}, statusWarn},
		{"missing policy", map[string]string{"contents": "read"}, []string{"owner/repo"}, "selected", true, nil, nil, statusWarn},
		{"extra repo", map[string]string{}, []string{"owner/repo", "owner/other"}, "selected", true, nil, map[string]string{}, statusFail},
		{"future repos", map[string]string{}, []string{"owner/repo"}, "all", true, nil, map[string]string{}, statusWarn},
		{"target absent", map[string]string{}, []string{"owner/other"}, "selected", true, nil, map[string]string{}, statusWarn},
		{"not installed", nil, nil, "", false, nil, map[string]string{}, statusFail},
		{"auth failure", nil, nil, "", false, errors.New("could not identify app"), map[string]string{}, statusWarn},
		{"scope unreadable", map[string]string{}, nil, "selected", true, errors.New("could not list repositories"), map[string]string{}, statusWarn},
		{"known excess survives error", map[string]string{"contents": "write"}, nil, "selected", true, errors.New("suspended"), map[string]string{}, statusFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := github.AppInstallationScope{Installed: tc.installed, Permissions: tc.grants, Repositories: tc.repos, RepositorySelection: tc.selection}
			result := appScopeCheck(globals.ExpectedApp{AppID: 123, Permissions: tc.policy}, scope, tc.err, "owner/repo")
			if result.Status != tc.want {
				t.Fatalf("check = %+v, want %s", result, tc.want.String())
			}
		})
	}
}

func TestAuditNamedAppKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	for _, tc := range []struct {
		name, granted string
		wantFail      bool
	}{
		{"complete pass", "read", false},
		{"excess grants", "write", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetAppAudit(t)
			resetAuditGlobals(t)
			globals.AppAudit = true
			globals.AppAuditPolicy = &globals.AppAuditConfig{ExpectedApps: []globals.ExpectedApp{{AppID: 123, Permissions: map[string]string{"contents": "read"}}}}
			globals.AppPrivateKeyEnvs = []string{"123=APP_TEST_KEY"}
			t.Setenv("APP_TEST_KEY", keyPEM)
			base := passingAuditHandler(t)
			handler := func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/app":
					_, _ = fmt.Fprint(w, `{"id":123}`)
				case "/repos/owner/repo/installation":
					_, _ = fmt.Fprintf(w, `{"id":1,"app_id":123,"account":{"login":"owner"},"permissions":{"contents":%q},"repository_selection":"selected"}`, tc.granted)
				case "/app/installations/1/access_tokens":
					w.WriteHeader(http.StatusCreated)
					_, _ = fmt.Fprint(w, `{"token":"app-minted-secret","permissions":{"metadata":"read"}}`)
				case "/installation/repositories":
					_, _ = fmt.Fprint(w, `{"total_count":1,"repositories":[{"full_name":"owner/repo"}]}`)
				default:
					base(w, r)
				}
			}
			out, err := runAuditWithHandler(t, handler)
			if (err != nil) != tc.wantFail {
				t.Fatalf("err=%v, output=%s", err, out)
			}
			if !strings.Contains(out, "app installations") || !strings.Contains(out, "other apps not inventoried") {
				t.Fatalf("missing scoped app report: %s", out)
			}
			for _, secret := range []string{keyPEM, "PRIVATE KEY", "app-minted-secret", "APP_TEST_KEY"} {
				if strings.Contains(out, secret) {
					t.Fatal("app audit exposed credential material")
				}
			}
		})
	}
}

func TestAppAuditIncompleteConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name        string
		apps        []globals.ExpectedApp
		credentials []string
		want        string
	}{
		{"nonpositive ID", []globals.ExpectedApp{{AppID: 0}}, nil, "positive"},
		{"duplicate ID", []globals.ExpectedApp{{AppID: 1}, {AppID: 1}}, nil, "unique"},
		{"bad ceiling", []globals.ExpectedApp{{AppID: 1, Permissions: map[string]string{"contents": "future"}}}, nil, "ceilings"},
		{"bad repo", []globals.ExpectedApp{{AppID: 1, Repositories: []string{"bad-name"}}}, nil, "OWNER/REPO"},
		{"target excluded", []globals.ExpectedApp{{AppID: 1, Repositories: []string{}}}, nil, "audited target"},
		{"bad credential", []globals.ExpectedApp{{AppID: 1}}, []string{"malformed"}, "APP_ID"},
		{"missing source", []globals.ExpectedApp{{AppID: 1}}, []string{"1=MISSING_APP_TEST_TOKEN"}, "empty"},
		{"wrong credential ID", []globals.ExpectedApp{{AppID: 1}}, []string{"2=MISSING_APP_TEST_TOKEN"}, "absent from expected_apps"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetAppAudit(t)
			globals.AppAudit = true
			globals.AppAuditPolicy = &globals.AppAuditConfig{ExpectedApps: tc.apps}
			globals.AppTokenEnvs = tc.credentials
			got := appAuditCheck(github.NewClient("unused", false), "owner", "repo", steps.New(nil, nil))
			if got.Status != statusWarn || !strings.Contains(got.Detail, tc.want) {
				t.Fatalf("check = %+v", got)
			}
		})
	}
}

func TestAppAuditSkipAndTokenOnly(t *testing.T) {
	resetAppAudit(t)
	selector := steps.New(nil, nil)
	client := github.NewClient("primary", false)
	if got := appAuditCheck(client, "owner", "repo", selector); got != nil {
		t.Fatal("default audit must not gain a row")
	}
	globals.BotManaged = true
	if got := appAuditCheck(client, "owner", "repo", selector); got.Status != statusSkip {
		t.Fatalf("bot default = %+v", got)
	}
	globals.AppAuditPolicy = &globals.AppAuditConfig{ExpectedApps: []globals.ExpectedApp{{AppID: 123, Permissions: map[string]string{}}}}
	if got := appAuditCheck(client, "owner", "repo", selector); got.Status != statusSkip {
		t.Fatalf("no credential = %+v", got)
	}
	globals.AppTokenEnvs = []string{"123=APP_TEST_TOKEN"}
	t.Setenv("APP_TEST_TOKEN", "token-secret-sentinel")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/installation/repositories" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"total_count":1,"repositories":[{"full_name":"owner/repo"}]}`)
	}))
	defer srv.Close()
	client = client.WithBaseURL(srv.URL)
	got := appAuditCheck(client, "owner", "repo", selector)
	if got.Status != statusWarn || !strings.Contains(got.Detail, "identity, grants and full installation scope unverified") || strings.Contains(got.Detail, "token-secret-sentinel") {
		t.Fatalf("token-only = %+v", got)
	}
	if failedRequired([]check{*got}) {
		t.Fatal("warning failed a nonstrict audit")
	}
	globals.Strict = true
	if !failedRequired([]check{*got}) {
		t.Fatal("strict did not fail incomplete app evidence")
	}
	if got := appAuditCheck(client, "owner", "repo", steps.New([]string{steps.AppAudit}, nil)); got.Status != statusSkip {
		t.Fatalf("selector = %+v", got)
	}
	globals.NoteExplicit("app-audit")
	globals.AppAudit = false
	if got := appAuditCheck(client, "owner", "repo", selector); got != nil {
		t.Fatal("explicit off ignored")
	}
}

func TestAppCredentialLoading(t *testing.T) {
	resetAppAudit(t)
	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, []byte("key-secret-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_TEST_KEY", "key-secret-sentinel")
	for _, cred := range []appCredential{{source: "file", value: path}, {source: "key-env", value: "APP_TEST_KEY"}} {
		raw, err := cred.load()
		if err != nil || string(raw) != "key-secret-sentinel" {
			t.Fatalf("load = %v", err)
		}
	}
	for _, cred := range []appCredential{{source: "file", value: t.TempDir()}, {source: "file", value: path + "/missing"}, {source: "file", value: os.DevNull}, {source: "key-env", value: "ABSENT_APP_TEST_KEY"}} {
		if _, err := cred.load(); err == nil || strings.Contains(err.Error(), cred.value) {
			t.Fatal("credential read failure accepted or exposed reference")
		}
	}
	oversized := filepath.Join(t.TempDir(), "oversized.pem")
	if err := os.WriteFile(oversized, []byte(strings.Repeat("x", maxAppCredentialBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (appCredential{source: "file", value: oversized}).load(); err == nil {
		t.Fatal("oversized credential accepted")
	}
	globals.AppPrivateKeys = []string{"123=" + path}
	creds, err := appCredentials()
	if err != nil || len(creds) != 1 {
		t.Fatalf("credentials = %v, %v", creds, err)
	}
	globals.AppPrivateKeyEnvs = []string{"123=APP_TEST_KEY"}
	if _, err := appCredentials(); err == nil {
		t.Fatal("duplicate app credential accepted")
	}
	globals.AppPrivateKeyEnvs = nil
	for _, invalid := range []string{"token-secret-sentinel", "0=x", "-1=x", "abc=x", "1="} {
		globals.AppPrivateKeys = []string{invalid}
		if _, err := appCredentials(); err == nil || strings.Contains(err.Error(), "token-secret-sentinel") {
			t.Fatal("invalid credential mapping accepted or exposed")
		}
	}
}
