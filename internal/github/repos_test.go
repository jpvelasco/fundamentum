package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateRepo(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/user/repos" {
			called = true
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"full_name":"owner/repo"}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	if err := c.CreateRepo("repo", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected POST /user/repos to be called")
	}
}

func TestGetRepoVisibility(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		want       string
		wantErr    bool
	}{
		{
			name:       "public",
			statusCode: http.StatusOK,
			response:   `{"visibility":"public"}`,
			want:       "public",
		},
		{
			name:       "private",
			statusCode: http.StatusOK,
			response:   `{"visibility":"private"}`,
			want:       "private",
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response:   `{"message":"Not Found"}`,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo" {
					t.Errorf("expected /repos/owner/repo, got %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			got, err := c.GetRepoVisibility("owner", "repo")
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}