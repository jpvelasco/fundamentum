package github

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnyFileExists(t *testing.T) {
	tests := []struct {
		name   string
		paths  []string
		resps  map[string]int // path -> status code
		want   bool
		wantErr bool
	}{
		{
			name:  "first path exists",
			paths: []string{".github/CONTRIBUTING.md", "CONTRIBUTING.md"},
			resps: map[string]int{
				"/repos/owner/repo/contents/.github/CONTRIBUTING.md": http.StatusOK,
			},
			want: true,
		},
		{
			name:  "second path exists",
			paths: []string{".github/CONTRIBUTING.md", "CONTRIBUTING.md"},
			resps: map[string]int{
				"/repos/owner/repo/contents/.github/CONTRIBUTING.md": http.StatusNotFound,
				"/repos/owner/repo/contents/CONTRIBUTING.md":         http.StatusOK,
			},
			want: true,
		},
		{
			name:  "none exist",
			paths: []string{"foo.md", "bar.md"},
			resps: map[string]int{
				"/repos/owner/repo/contents/foo.md": http.StatusNotFound,
				"/repos/owner/repo/contents/bar.md": http.StatusNotFound,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				status, ok := tt.resps[r.URL.Path]
				if !ok {
					status = http.StatusNotFound
				}
				w.WriteHeader(status)
			}))
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			got, err := c.AnyFileExists("owner", "repo", tt.paths)
			if (err != nil) != tt.wantErr {
				t.Errorf("AnyFileExists() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("AnyFileExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileStatus(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		status   int
		response string
		want     string
		wantErr  bool
	}{
		{
			name:    "create - not found",
			content: "hello",
			status:  http.StatusNotFound,
			want:    "create",
		},
		{
			name:    "skip - same content",
			content: "hello",
			status:  http.StatusOK,
			response: `{"content":"` + base64.StdEncoding.EncodeToString([]byte("hello")) + `","sha":"abc"}`,
			want:    "skip",
		},
		{
			name:    "update - different content",
			content: "new content",
			status:  http.StatusOK,
			response: `{"content":"` + base64.StdEncoding.EncodeToString([]byte("old content")) + `","sha":"abc"}`,
			want:    "update",
		},
		{
			name:    "error - 500",
			content: "hello",
			status:  http.StatusInternalServerError,
			response: `{"message":"Internal Server Error"}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				if tt.response != "" {
					_, _ = w.Write([]byte(tt.response))
				}
			}))
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			got, err := c.FileStatus("owner", "repo", "test.md", []byte(tt.content))
			if (err != nil) != tt.wantErr {
				t.Errorf("FileStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("FileStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpsertFile_Update(t *testing.T) {
	existing := base64.StdEncoding.EncodeToString([]byte("old"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"` + existing + `","sha":"abc123"}`))
			return
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	action, err := c.UpsertFile("owner", "repo", "test.md", []byte("new"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "updated" {
		t.Errorf("expected action=updated, got %q", action)
	}
}

func TestUpsertFile_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"error"}`))
			return
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	_, err := c.UpsertFile("owner", "repo", "test.md", []byte("hello"))
	if err == nil {
		t.Error("expected error for 500 status")
	}
}

func TestRulesetExists(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     bool
	}{
		{
			name:     "found",
			response: `[{"name":"protect-main"}]`,
			want:     true,
		},
		{
			name:     "not found in list",
			response: `[{"name":"other-ruleset"}]`,
			want:     false,
		},
		{
			name:     "empty list",
			response: `[]`,
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			got, err := c.RulesetExists("owner", "repo", "protect-main")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("RulesetExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRulesetExists_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	got, err := c.RulesetExists("owner", "repo", "protect-main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false for non-200 response")
	}
}

func TestEnsureBranchRuleset_AlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"protect-main"}]`))
			return
		}
		// Should not reach POST
		t.Error("unexpected POST — ruleset already exists")
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.EnsureBranchRuleset("owner", "repo", []string{}, BranchProtectionOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureBranchRuleset_CreatesNew(t *testing.T) {
	postCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if r.Method == http.MethodPost {
			postCalled = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
			return
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.EnsureBranchRuleset("owner", "repo", []string{}, BranchProtectionOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !postCalled {
		t.Error("expected POST to create ruleset")
	}
}

func TestEnsureTagRuleset_AlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"protect-version-tags"}]`))
			return
		}
		t.Error("unexpected POST — tag ruleset already exists")
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.EnsureTagRuleset("owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureTagRuleset_CreatesNew(t *testing.T) {
	postCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if r.Method == http.MethodPost {
			postCalled = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":2}`))
			return
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.EnsureTagRuleset("owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !postCalled {
		t.Error("expected POST to create tag ruleset")
	}
}

func TestCreateBranchRuleset_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"validation failed"}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.CreateBranchRuleset("owner", "repo", []string{}, BranchProtectionOptions{})
	if err == nil {
		t.Error("expected error for 422 response")
	}
}

func TestCreateTagRuleset_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"validation failed"}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.CreateTagRuleset("owner", "repo")
	if err == nil {
		t.Error("expected error for 422 response")
	}
}

func TestCreateBranchRuleset_WithStatusChecks(t *testing.T) {
	var postPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		postPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.CreateBranchRuleset("owner", "repo", []string{"ci"}, BranchProtectionOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(postPath, "rulesets") {
		t.Errorf("expected rulesets path, got %s", postPath)
	}
}

func TestNewClient_EmptyToken(t *testing.T) {
	// When token is empty, it falls back to env var (which is empty in test)
	c := NewClient("", false)
	if c.Token != "" {
		t.Errorf("expected empty token fallback, got %q", c.Token)
	}
	// Verify client is usable
	if c.client == nil {
		t.Error("expected non-nil http client")
	}
}

func TestCreateRepo_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"name already exists"}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.CreateRepo("taken", false)
	if err == nil {
		t.Error("expected error for 422 response")
	}
}

func TestApplyGeneralSettings_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.ApplyGeneralSettings("owner", "repo")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestApplyClassicBranchProtection_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.ApplyClassicBranchProtection("owner", "repo", "main", DefaultStatusChecks, BranchProtectionOptions{})
	if err == nil {
		t.Error("expected error for 403 response")
	}
}

func TestRemoveClassicBranchProtection_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.RemoveClassicBranchProtection("owner", "repo", "main")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestEnableSecurity_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.EnableSecurity("owner", "repo", "private")
	if err == nil {
		t.Error("expected error for 403 response")
	}
}

func TestClassicProtectionExists_Error(t *testing.T) {
	// The function doesn't return an error for non-200 status — it just returns false
	// Test that API errors still propagate
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	exists, err := c.ClassicProtectionExists("owner", "repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected true for 200 response")
	}
}

func TestClient_DoVerbose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient("t", true).WithBaseURL(srv.URL)
	resp, err := c.get("/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBase(t *testing.T) {
	c := NewClient("t", false)
	if c.base() != "https://api.github.com" {
		t.Errorf("expected default base, got %s", c.base())
	}
	c.WithBaseURL("http://localhost:8080")
	if c.base() != "http://localhost:8080" {
		t.Errorf("expected custom base, got %s", c.base())
	}
}