package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong Authorization header: %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := NewClient("test-token", false).WithBaseURL(srv.URL)
	resp, err := c.get("/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestClientBase_DefaultFallback(t *testing.T) {
	c := &Client{Token: "t", Verbose: false, baseURL: ""}
	got := c.base()
	if got != defaultBase {
		t.Errorf("expected %s, got %s", defaultBase, got)
	}
}

func TestWithBaseURL_RejectsNonHTTPS(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantSet bool
	}{
		{"empty url", "", false},
		{"invalid url", "not a url", false},
		{"http scheme", "http://evil.com", false},
		{"ftp scheme", "ftp://evil.com", false},
		{"https scheme", "https://api.github.com", true},
		{"localhost", "http://localhost:8080", true},
		{"127.0.0.1", "http://127.0.0.1:8080", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := (&Client{Token: "t", Verbose: false}).WithBaseURL(tt.url)
			if tt.wantSet && c.baseURL == "" {
				t.Error("expected baseURL to be set, got empty")
			}
			if !tt.wantSet && c.baseURL != "" {
				t.Errorf("expected baseURL to remain empty, got %q", c.baseURL)
			}
		})
	}
}

func TestClientPatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient("test-token", false).WithBaseURL(srv.URL)
	resp, err := c.patch("/", map[string]any{"foo": "bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRepoPath_SafeEscaping(t *testing.T) {
	tests := []struct {
		name  string
		owner string
		repo  string
		want  string
	}{
		{"simple", "owner", "repo", "/repos/owner/repo"},
		{"slash in owner", "owner/repo", "bad", "/repos/owner%2Frepo/bad"},
		{"slash in repo", "owner", "repo/name", "/repos/owner/repo%2Fname"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repoPath(tt.owner, tt.repo)
			if got != tt.want {
				t.Errorf("repoPath(%q, %q) = %q, want %q", tt.owner, tt.repo, got, tt.want)
			}
		})
	}
}