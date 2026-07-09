package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreatePRBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"commit": map[string]any{"sha": "aaaa1111"},
			})
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ref": "refs/heads/test-branch",
			})
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	if err := c.CreatePRBranch("owner", "repo", "test-branch", "main"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePRBranch_GetError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.CreatePRBranch("owner", "repo", "test-branch", "main")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "main") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCreatePullRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["title"] != "test title" {
			t.Errorf("expected title 'test title', got %v", body["title"])
		}
		if body["head"] != "feature" {
			t.Errorf("expected head 'feature', got %v", body["head"])
		}
		if body["base"] != "main" {
			t.Errorf("expected base 'main', got %v", body["base"])
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 42})
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	num, err := c.CreatePullRequest("owner", "repo", "test title", "test body", "feature", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != 42 {
		t.Errorf("expected PR number 42, got %d", num)
	}
}

func TestCreatePullRequest_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"invalid"}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	_, err := c.CreatePullRequest("owner", "repo", "title", "body", "head", "main")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "create PR") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpsertFileOnBranch_Create(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["branch"] != "feature" {
				t.Errorf("expected branch 'feature', got %v", body["branch"])
			}
			// Verify no "via fundamentum" in commit message
			msg, _ := body["message"].(string)
			if strings.Contains(msg, "fundamentum") {
				t.Errorf("commit message should not contain 'fundamentum': %s", msg)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	action, err := c.UpsertFileOnBranch("owner", "repo", "feature", "test.md", []byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "created" {
		t.Errorf("expected action=created, got %q", action)
	}
}

func TestUpsertFileOnBranch_Update(t *testing.T) {
	existing := "dGVzdA==" // "test" in base64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": existing,
				"sha":     "abc123",
			})
		case http.MethodPut:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			msg, _ := body["message"].(string)
			if strings.Contains(msg, "fundamentum") {
				t.Errorf("commit message should not contain 'fundamentum': %s", msg)
			}
			if !strings.Contains(msg, "chore: update") {
				t.Errorf("expected update message, got: %s", msg)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	action, err := c.UpsertFileOnBranch("owner", "repo", "feature", "test.md", []byte("new content"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "updated" {
		t.Errorf("expected action=updated, got %q", action)
	}
}

func TestUpsertFileOnBranch_Workflow404_Skipped(t *testing.T) {
	// GitHub Actions locks workflow files — PUT returns 404 on update.
	existing := base64.StdEncoding.EncodeToString([]byte("old workflow content"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": existing,
				"sha":     "abc123",
			})
		case http.MethodPut:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	action, err := c.UpsertFileOnBranch("owner", "repo", "feature", ".github/workflows/ci.yml", []byte("new content"))
	if err == nil {
		t.Fatal("expected ErrWorkflowLocked, got nil")
	}
	if !IsWorkflowLocked(err) {
		t.Errorf("expected ErrWorkflowLocked, got: %v", err)
	}
	if action != "skipped" {
		t.Errorf("expected action=skipped, got %q", action)
	}
}

func TestUpsertFileOnBranch_Create404_Error(t *testing.T) {
	// If file doesn't exist (create) and PUT still returns 404, it's a real error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	_, err := c.UpsertFileOnBranch("owner", "repo", "feature", "new_file.md", []byte("hello"))
	if err == nil {
		t.Fatal("expected error for 404 on create, got nil")
	}
}

func TestUpsertFileOnBranch_Skip(t *testing.T) {
	existing := "aGVsbG8=" // "hello" in base64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": existing,
				"sha":     "abc123",
			})
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	action, err := c.UpsertFileOnBranch("owner", "repo", "feature", "test.md", []byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "skipped" {
		t.Errorf("expected action=skipped, got %q", action)
	}
}

func TestIsConflict409(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"409: rule violations", true},
		{"409: GH013", true},
		{"409: something else", false},
		{"403: rule violations", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			err := &testErr{msg: tt.msg}
			if got := IsConflict409(err); got != tt.want {
				t.Errorf("IsConflict409(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
	if got := IsConflict409(nil); got {
		t.Error("IsConflict409(nil) = true, want false")
	}
}

func TestIsForbidden403(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"403 Forbidden: Forbidden", true},
		{"create branch ruleset: 403 Forbidden: {\"message\":\"Forbidden\"}", true},
		{"403: rule violations", true},
		{"409: rule violations", false},
		{"404: not found", false},
		{"422 Unprocessable Entity: validation failed", false},
		{"500 Internal Server Error", false},
		{"network error: connection refused", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			err := &testErr{msg: tt.msg}
			if got := IsForbidden403(err); got != tt.want {
				t.Errorf("IsForbidden403(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
	if got := IsForbidden403(nil); got {
		t.Error("IsForbidden403(nil) = true, want false")
	}
}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

func TestApplyViaPR(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		changes []FileChange
		wantErr bool
		errMsg  string
	}{
		{
			name: "happy path with 2 files",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					// branch info or file check
					if strings.Contains(r.URL.Path, "/branches/") {
						w.WriteHeader(http.StatusOK)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"commit": map[string]any{"sha": "aaaa1111"},
						})
					} else {
						// file does not exist
						w.WriteHeader(http.StatusNotFound)
					}
				case http.MethodPost:
					// create branch ref or create PR
					if strings.Contains(r.URL.Path, "/git/refs") {
						w.WriteHeader(http.StatusCreated)
					} else {
						w.WriteHeader(http.StatusCreated)
						_ = json.NewEncoder(w).Encode(map[string]any{"number": 7})
					}
				case http.MethodPut:
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{})
				}
			},
			changes: []FileChange{
				{Path: "README.md", Content: []byte("hello")},
				{Path: "LICENSE", Content: []byte("MIT")},
			},
		},
		{
			name: "branch create fails",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"commit": map[string]any{"sha": "aaaa1111"},
					})
				case http.MethodPost:
					w.WriteHeader(http.StatusUnprocessableEntity)
					_, _ = w.Write([]byte(`{"message":"invalid"}`))
				}
			},
			changes: []FileChange{
				{Path: "README.md", Content: []byte("hello")},
			},
			wantErr: true,
			errMsg:  "create branch",
		},
		{
			name: "workflow locked continues",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Track which file is being upserted
				path := ""
				if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/") {
					path = strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/contents/")
				}
				if r.Method == http.MethodPut {
					path = strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/contents/")
				}
				switch r.Method {
				case http.MethodGet:
					switch {
					case strings.Contains(r.URL.Path, "/branches/"):
						w.WriteHeader(http.StatusOK)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"commit": map[string]any{"sha": "aaaa1111"},
						})
					case path == ".github/workflows/ci.yml":
						// existing workflow file
						w.WriteHeader(http.StatusOK)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"content": "b2xk",
							"sha":     "abc123",
						})
					default:
						// other file does not exist
						w.WriteHeader(http.StatusNotFound)
					}
				case http.MethodPost:
					if strings.Contains(r.URL.Path, "/git/refs") {
						w.WriteHeader(http.StatusCreated)
					} else {
						w.WriteHeader(http.StatusCreated)
						_ = json.NewEncoder(w).Encode(map[string]any{"number": 9})
					}
				case http.MethodPut:
					if path == ".github/workflows/ci.yml" {
						// workflow locked
						w.WriteHeader(http.StatusNotFound)
					} else {
						w.WriteHeader(http.StatusCreated)
					}
				}
			},
			changes: []FileChange{
				{Path: "README.md", Content: []byte("hello")},
				{Path: ".github/workflows/ci.yml", Content: []byte("new workflow")},
			},
		},
		{
			name: "upsert error returns immediately",
			handler: func(w http.ResponseWriter, r *http.Request) {
				path := ""
				if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/") {
					path = strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/contents/")
				}
				if r.Method == http.MethodPut {
					path = strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/contents/")
				}
				switch r.Method {
				case http.MethodGet:
					if strings.Contains(r.URL.Path, "/branches/") {
						w.WriteHeader(http.StatusOK)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"commit": map[string]any{"sha": "aaaa1111"},
						})
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				case http.MethodPost:
					w.WriteHeader(http.StatusCreated)
				case http.MethodPut:
					if path == "README.md" {
						w.WriteHeader(http.StatusCreated)
					} else {
						// non-workflow error on second file
						w.WriteHeader(http.StatusConflict)
						_, _ = w.Write([]byte(`{"message":"conflict"}`))
					}
				}
			},
			changes: []FileChange{
				{Path: "README.md", Content: []byte("hello")},
				{Path: "bad.md", Content: []byte("bad")},
			},
			wantErr: true,
			errMsg:  "upsert bad.md",
		},
		{
			name: "PR create fails",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					if strings.Contains(r.URL.Path, "/branches/") {
						w.WriteHeader(http.StatusOK)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"commit": map[string]any{"sha": "aaaa1111"},
						})
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				case http.MethodPost:
					if strings.Contains(r.URL.Path, "/git/refs") {
						w.WriteHeader(http.StatusCreated)
					} else {
						// PR creation fails
						w.WriteHeader(http.StatusUnprocessableEntity)
						_, _ = w.Write([]byte(`{"message":"invalid"}`))
					}
				case http.MethodPut:
					w.WriteHeader(http.StatusCreated)
				}
			},
			changes: []FileChange{
				{Path: "README.md", Content: []byte("hello")},
			},
			wantErr: true,
			errMsg:  "create PR",
		},
		{
			name: "empty changes",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"commit": map[string]any{"sha": "aaaa1111"},
					})
				case http.MethodPost:
					if strings.Contains(r.URL.Path, "/git/refs") {
						w.WriteHeader(http.StatusCreated)
					} else {
						w.WriteHeader(http.StatusCreated)
						_ = json.NewEncoder(w).Encode(map[string]any{"number": 3})
					}
				}
			},
			changes: nil,
		},
		{
			name: "skipped file no print",
			handler: func(w http.ResponseWriter, r *http.Request) {
				path := ""
				if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/") {
					path = strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/contents/")
				}
				if r.Method == http.MethodPut {
					path = strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/contents/")
				}
				switch r.Method {
				case http.MethodGet:
					switch {
					case strings.Contains(r.URL.Path, "/branches/"):
						w.WriteHeader(http.StatusOK)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"commit": map[string]any{"sha": "aaaa1111"},
						})
					case path == "README.md":
						// file exists with same content
						w.WriteHeader(http.StatusOK)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"content": "aGVsbG8=", // "hello" base64
							"sha":     "abc123",
						})
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				case http.MethodPost:
					if strings.Contains(r.URL.Path, "/git/refs") {
						w.WriteHeader(http.StatusCreated)
					} else {
						w.WriteHeader(http.StatusCreated)
						_ = json.NewEncoder(w).Encode(map[string]any{"number": 5})
					}
				case http.MethodPut:
					w.WriteHeader(http.StatusCreated)
				}
			},
			changes: []FileChange{
				{Path: "README.md", Content: []byte("hello")},
				{Path: "LICENSE", Content: []byte("MIT")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			prNum, err := c.ApplyViaPR("owner", "repo", "main", tt.changes)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if prNum == 0 {
					t.Error("expected non-zero PR number")
				}
			}
		})
	}
}
