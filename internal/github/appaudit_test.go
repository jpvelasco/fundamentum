package github

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func appTestKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func TestAppJWT(t *testing.T) {
	key, data := appTestKey(t)
	now := time.Unix(1700000000, 0)
	token, err := appJWT(123, data, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig) != nil {
		t.Fatal("JWT signature invalid")
	}
	var claims struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(raw, &claims) != nil {
		t.Fatal("JWT claims invalid")
	}
	if claims.Iss != "123" || claims.Iat != now.Unix()-60 || claims.Exp <= now.Unix() || claims.Exp > now.Unix()+600 {
		t.Fatalf("claims = %+v", claims)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appJWT(123, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}), now); err != nil {
		t.Fatal(err)
	}
	if _, err := appJWT(0, data, now); err == nil {
		t.Fatal("nonpositive app ID accepted")
	}
	for _, invalid := range [][]byte{nil, []byte("secret-sentinel"), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("secret-sentinel")})} {
		if _, err := appJWT(123, invalid, now); err == nil || strings.Contains(err.Error(), "secret-sentinel") {
			t.Fatal("invalid key accepted or exposed")
		}
	}
}

func TestAuditAppInstallationScope(t *testing.T) {
	_, data := appTestKey(t)
	var minted int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app":
			_, _ = fmt.Fprint(w, `{"id":123}`)
		case "/repos/owner/repo/installation":
			_, _ = fmt.Fprint(w, `{"id":456,"app_id":123,"account":{"login":"owner"},"permissions":{"contents":"write"},"repository_selection":"selected"}`)
		case "/app/installations/456/access_tokens":
			minted++
			if r.Method != http.MethodPost {
				t.Error("expected token mint POST")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			want := `{"permissions":{"metadata":"read"}}`
			got, err := json.Marshal(body)
			if err != nil || string(got) != want {
				t.Errorf("mint body = %s, want %s", got, want)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, `{"token":"installation-secret","permissions":{"metadata":"read"}}`)
		case "/installation/repositories":
			if r.Header.Get("Authorization") != "Bearer installation-secret" {
				t.Error("wrong installation credential")
			}
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", `<https://untrusted.invalid/?token=do-not-follow>; rel="next"`)
				_, _ = fmt.Fprint(w, `{"total_count":2,"repositories":[{"full_name":"owner/repo"}]}`)
			} else {
				_, _ = fmt.Fprint(w, `{"total_count":2,"repositories":[{"full_name":"owner/other"}]}`)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	client := NewClient("primary", true).WithBaseURL(srv.URL)
	scope, err := client.AuditAppInstallation("owner", "repo", 123, data)
	if err != nil {
		t.Fatal(err)
	}
	if !scope.Installed || scope.Permissions["contents"] != "write" || !slices.Equal(scope.Repositories, []string{"owner/repo", "owner/other"}) || minted != 1 {
		t.Fatalf("scope = %+v, minted = %d", scope, minted)
	}
	if client.Token != "primary" || !client.Verbose {
		t.Fatal("app audit modified primary client")
	}
}

func TestAuditAppInstallationFailures(t *testing.T) {
	_, key := appTestKey(t)
	for _, tc := range []struct {
		name      string
		at        string
		status    int
		body      string
		installed bool
		wantErr   bool
		wantMints int
	}{
		{"bad identity", "/app", 200, `{"id":999}`, false, true, 0},
		{"auth refused", "/app", 401, "response-secret", false, true, 0},
		{"bad JSON", "/app", 200, "response-secret", false, true, 0},
		{"not installed", "/repos/owner/repo/installation", 404, "response-secret", false, false, 0},
		{"access refused", "/repos/owner/repo/installation", 403, "response-secret", false, true, 0},
		{"incomplete metadata", "/repos/owner/repo/installation", 200, `{"id":1,"app_id":123}`, false, true, 0},
		{"wrong account", "/repos/owner/repo/installation", 200, `{"id":1,"app_id":123,"account":{"login":"other"},"permissions":{},"repository_selection":"selected"}`, false, true, 0},
		{"suspended", "/repos/owner/repo/installation", 200, `{"id":1,"app_id":123,"account":{"login":"owner"},"permissions":{},"repository_selection":"selected","suspended_at":"2026-01-01"}`, true, true, 0},
		{"unknown selection", "/repos/owner/repo/installation", 200, `{"id":1,"app_id":123,"account":{"login":"owner"},"permissions":{},"repository_selection":"future"}`, true, true, 0},
		{"mint not retried", "/app/installations/1/access_tokens", 500, "response-secret", true, true, 1},
		{"missing minted token", "/app/installations/1/access_tokens", 201, `{"permissions":{"metadata":"read"}}`, true, true, 1},
		{"minted excessive grant", "/app/installations/1/access_tokens", 201, `{"token":"response-secret","permissions":{"metadata":"read","contents":"write"}}`, true, true, 1},
		{"minted malformed JSON", "/app/installations/1/access_tokens", 201, `{"token":"response-secret",broken}`, true, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mints := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					mints++
				}
				if r.URL.Path == tc.at {
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprint(w, tc.body)
					return
				}
				switch r.URL.Path {
				case "/app":
					_, _ = fmt.Fprint(w, `{"id":123}`)
				case "/repos/owner/repo/installation":
					_, _ = fmt.Fprint(w, `{"id":1,"app_id":123,"account":{"login":"owner"},"permissions":{},"repository_selection":"selected"}`)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer srv.Close()
			scope, err := NewClient("primary-secret", true).WithBaseURL(srv.URL).AuditAppInstallation("owner", "repo", 123, key)
			if (err != nil) != tc.wantErr || scope.Installed != tc.installed || mints != tc.wantMints {
				t.Fatalf("scope=%+v err=%v mints=%d", scope, err, mints)
			}
			if err != nil && (strings.Contains(err.Error(), "response-secret") || strings.Contains(err.Error(), "primary-secret")) {
				t.Fatal("API failure exposed credentials")
			}
		})
	}
	if _, err := NewClient("primary", false).AuditAppInstallation("owner", "repo", 123, []byte("key-secret")); err == nil {
		t.Fatal("invalid key accepted")
	}
}

func TestAppTokenRepositoryEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		pages   []string
		wantErr bool
	}{
		{"empty installation", 200, []string{`{"total_count":0,"repositories":[]}`}, false},
		{"count omitted", 200, []string{`{"repositories":[]}`}, true},
		{"negative count", 200, []string{`{"total_count":-1,"repositories":[]}`}, true},
		{"incomplete", 200, []string{`{"total_count":1,"repositories":[]}`}, true},
		{"too many entries", 200, []string{`{"total_count":0,"repositories":[{"full_name":"owner/repo"}]}`}, true},
		{"name omitted", 200, []string{`{"total_count":1,"repositories":[{}]}`}, true},
		{"duplicate entries", 200, []string{`{"total_count":2,"repositories":[{"full_name":"owner/repo"},{"full_name":"OWNER/REPO"}]}`}, true},
		{"changing count", 200, []string{`{"total_count":2,"repositories":[{"full_name":"owner/repo"}]}`, `{"total_count":3,"repositories":[{"full_name":"owner/other"}]}`}, true},
		{"forbidden", 403, []string{"response-secret"}, true},
		{"malformed", 200, []string{"response-secret"}, true},
		{"redirect", 302, []string{"response-secret"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if tc.status == 302 {
					w.Header().Set("Location", "/do-not-follow")
				}
				if requests > len(tc.pages) {
					t.Error("unexpected extra request")
					w.WriteHeader(400)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.pages[requests-1])
			}))
			defer srv.Close()
			_, err := NewClient("primary", true).WithBaseURL(srv.URL).AppTokenRepositories("installation-secret")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if err != nil && (strings.Contains(err.Error(), "response-secret") || strings.Contains(err.Error(), "installation-secret")) {
				t.Fatal("repository failure exposed secret")
			}
		})
	}
	if _, err := NewClient("primary", false).AppTokenRepositories(" "); err == nil {
		t.Fatal("empty token accepted")
	}
	client := NewClient("primary", false)
	client.client.Transport = appFailingTransport{}
	if _, err := client.AppTokenRepositories("installation-secret"); err == nil || strings.Contains(err.Error(), "transport-secret") {
		t.Fatal("transport error accepted or exposed")
	}
}

type appFailingTransport struct{}

func (appFailingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport-secret")
}
