package github

import (
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