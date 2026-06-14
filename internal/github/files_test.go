package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpsertFile_Create(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
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
	action, err := c.UpsertFile("owner", "repo", "CONTRIBUTING.md", []byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "created" {
		t.Errorf("expected action=created, got %q", action)
	}
}

func TestUpsertFile_Skip(t *testing.T) {
	existing := base64.StdEncoding.EncodeToString([]byte("hello"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": existing,
				"sha":     "abc123",
			})
			return
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	action, err := c.UpsertFile("owner", "repo", "CONTRIBUTING.md", []byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "skipped" {
		t.Errorf("expected action=skipped, got %q", action)
	}
}
