package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpsertFile(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		filePath string
		content  []byte
		wantErr  bool
		wantLock bool
		wantAction string
	}{
		{
			name: "create",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if r.Method == http.MethodPut {
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{}`))
					return
				}
			},
			filePath: "CONTRIBUTING.md",
			content: []byte("hello"),
			wantAction: "created",
		},
		{
			name: "workflow 404 (locked)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				existing := base64.StdEncoding.EncodeToString([]byte("old workflow content"))
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"content": existing,
						"sha":     "abc123",
					})
					return
				}
				if r.Method == http.MethodPut {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"message":"Not Found"}`))
					return
				}
			},
			filePath: ".github/workflows/ci.yml",
			content: []byte("new content"),
			wantErr: true,
			wantLock: true,
			wantAction: "skipped",
		},
		{
			name: "404 on create (real error)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if r.Method == http.MethodPut {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"message":"Not Found"}`))
					return
				}
			},
			filePath: "new_file.md",
			content: []byte("hello"),
			wantErr: true,
		},
		{
			name: "skip (content unchanged)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"content": base64.StdEncoding.EncodeToString([]byte("hello")),
						"sha":     "abc123",
					})
					return
				}
			},
			filePath: "CONTRIBUTING.md",
			content: []byte("hello"),
			wantAction: "skipped",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			action, err := c.UpsertFile("owner", "repo", tt.filePath, tt.content)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantLock && !IsWorkflowLocked(err) {
					t.Errorf("expected ErrWorkflowLocked, got: %v", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantAction != "" && action != tt.wantAction {
				t.Errorf("expected action=%q, got %q", tt.wantAction, action)
			}
		})
	}
}
