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

func TestUpsertFile_Workflow404_ErrWorkflowLocked(t *testing.T) {
	// GitHub Actions locks workflow files — PUT returns 404 on update.
	existing := base64.StdEncoding.EncodeToString([]byte("old workflow content"))
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": existing,
				"sha":     "abc123",
			})
			return
		}
		if r.Method == http.MethodPut {
			callCount++
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	action, err := c.UpsertFile("owner", "repo", ".github/workflows/ci.yml", []byte("new content"))
	if err == nil {
		t.Fatal("expected ErrWorkflowLocked, got nil")
	}
	if !IsWorkflowLocked(err) {
		t.Errorf("expected ErrWorkflowLocked, got: %v", err)
	}
	if action != "skipped" {
		t.Errorf("expected action=skipped, got %q", action)
	}
	if callCount != 1 {
		t.Errorf("expected 1 PUT call, got %d", callCount)
	}
}

func TestUpsertFile_Create404_Error(t *testing.T) {
	// If file doesn't exist (create) and PUT still returns 404, it's a real error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	_, err := c.UpsertFile("owner", "repo", "new_file.md", []byte("hello"))
	if err == nil {
		t.Fatal("expected error for 404 on create, got nil")
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
