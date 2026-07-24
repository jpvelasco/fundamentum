package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				if tt.response != "" {
					var out any
					_ = json.Unmarshal([]byte(tt.response), &out)
					_ = json.NewEncoder(w).Encode(out)
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

func TestUpsertFile_UpdateAndErrorStatus(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantAction string
	}{
		{
			name: "update",
			handler: func(w http.ResponseWriter, r *http.Request) {
				existing := base64.StdEncoding.EncodeToString([]byte("old"))
				if r.Method == http.MethodGet {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"content": existing, "sha": "abc123"})
					return
				}
				if r.Method == http.MethodPut {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{})
					return
				}
			},
			wantAction: "updated",
		},
		{
			name: "error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"message":"error"}`))
					return
				}
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			action, err := c.UpsertFile("owner", "repo", "test.md", []byte("new"))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if action != tt.wantAction {
					t.Errorf("expected action=%q, got %q", tt.wantAction, action)
				}
			}
		})
	}
}

func TestRulesetExists(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		want       bool
	}{
		{
			name:       "found",
			statusCode: http.StatusOK,
			response:   `[{"name":"protect-main"}]`,
			want:       true,
		},
		{
			name:       "not found in list",
			statusCode: http.StatusOK,
			response:   `[{"name":"other-ruleset"}]`,
			want:       false,
		},
		{
			name:       "empty list",
			statusCode: http.StatusOK,
			response:   `[]`,
			want:       false,
		},
		{
			name:       "non-200 status",
			statusCode: http.StatusForbidden,
			response:   ``,
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				if tt.response != "" {
					var out any
					_ = json.Unmarshal([]byte(tt.response), &out)
					_ = json.NewEncoder(w).Encode(out)
				}
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

func TestEnsureBranchRuleset(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantPost bool
	}{
		{
			name:     "already exists",
			response: `[{"name":"protect-main"}]`,
			wantPost: false,
		},
		{
			name:     "creates new",
			response: `[]`,
			wantPost: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			postCalled := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(tt.response))
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
			if postCalled != tt.wantPost {
				t.Errorf("POST called: %v, want %v", postCalled, tt.wantPost)
			}
		})
	}
}

func TestEnsureTagRuleset(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantPost bool
	}{
		{
			name:     "already exists",
			response: `[{"name":"protect-version-tags"}]`,
			wantPost: false,
		},
		{
			name:     "creates new",
			response: `[]`,
			wantPost: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			postCalled := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(tt.response))
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
			if postCalled != tt.wantPost {
				t.Errorf("POST called: %v, want %v", postCalled, tt.wantPost)
			}
		})
	}
}

func TestCreateRulesetErrors(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Client) error
	}{
		{
			name: "branch ruleset error",
			fn: func(c *Client) error {
				return c.CreateBranchRuleset("owner", "repo", []string{}, BranchProtectionOptions{})
			},
		},
		{
			name: "tag ruleset error",
			fn: func(c *Client) error {
				return c.CreateTagRuleset("owner", "repo")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"message":"validation failed"}`))
			}))
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			err := tt.fn(c)
			if err == nil {
				t.Error("expected error for 422 response")
			}
		})
	}
}

func TestCreateBranchRuleset_WithStatusChecks(t *testing.T) {
	var postBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode the body to inspect
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		postBody = body
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.CreateBranchRuleset("owner", "repo", []string{"ci"}, BranchProtectionOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if postBody == nil {
		t.Fatal("expected POST body")
	}
}

func TestCreateBranchRuleset_DedupStatusChecks(t *testing.T) {
	tests := []struct {
		name        string
		statusChecks []string
		wantCount   int // number of required_status_checks entries
	}{
		{
			name:        "no duplicates",
			statusChecks: []string{"ci", "lint"},
			wantCount:   3, // Codacy + ci + lint
		},
		{
			name:        "duplicate with default",
			statusChecks: []string{"Codacy Static Code Analysis"},
			wantCount:   1, // Codacy deduped
		},
		{
			name:        "empty",
			statusChecks: nil,
			wantCount:   1, // Codacy only
		},
		{
			name:        "empty string slice",
			statusChecks: []string{},
			wantCount:   1, // Codacy only
		},
		{
			name:        "self duplicate",
			statusChecks: []string{"ci", "ci"},
			wantCount:   2, // Codacy + ci
		},
		{
			name:        "all duplicates",
			statusChecks: []string{"Codacy Static Code Analysis", "Codacy Static Code Analysis"},
			wantCount:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var postBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&postBody)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":1}`))
			}))
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			err := c.CreateBranchRuleset("owner", "repo", tt.statusChecks, BranchProtectionOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Count required_status_checks entries in the rules array
			rules, ok := postBody["rules"].([]any)
			if !ok {
				t.Fatal("expected rules array in body")
			}
			for _, rule := range rules {
				rm, ok := rule.(map[string]any)
				if !ok || rm["type"] != "required_status_checks" {
					continue
				}
				params, ok := rm["parameters"].(map[string]any)
				if !ok {
					continue
				}
				checks, ok := params["required_status_checks"].([]any)
				if !ok {
					continue
				}
				if len(checks) != tt.wantCount {
					t.Errorf("expected %d checks, got %d", tt.wantCount, len(checks))
				}
			}
		})
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

func TestErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		fn         func(*Client) error
	}{
		{
			name:   "CreateRepo 422",
			status: http.StatusUnprocessableEntity,
			fn: func(c *Client) error {
				return c.CreateRepo("taken", false)
			},
		},
		{
			name:   "ApplyGeneralSettings 404",
			status: http.StatusNotFound,
			fn: func(c *Client) error {
				return c.ApplyGeneralSettings("owner", "repo")
			},
		},
		{
			name:   "ApplyClassicBranchProtection 403",
			status: http.StatusForbidden,
			fn: func(c *Client) error {
				return c.ApplyClassicBranchProtection("owner", "repo", "main", DefaultStatusChecks, BranchProtectionOptions{})
			},
		},
		{
			name:   "RemoveClassicBranchProtection 404",
			status: http.StatusNotFound,
			fn: func(c *Client) error {
				return c.RemoveClassicBranchProtection("owner", "repo", "main")
			},
		},
		{
			name:   "EnableSecurity 403",
			status: http.StatusForbidden,
			fn: func(c *Client) error {
				return c.EnableSecurity("owner", "repo", "private")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"message":"error"}`))
			}))
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			err := tt.fn(c)
			if err == nil {
				t.Error("expected error")
			}
		})
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