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

	c := &Client{Token: "t", baseURL: srv.URL}
	if err := c.CreateRepo("repo", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected POST /user/repos to be called")
	}
}
