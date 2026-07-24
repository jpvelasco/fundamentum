package github

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnableSecurity(t *testing.T) {
	tests := []struct {
		name       string
		visibility string
		wantCodeQL bool
	}{
		{
			name:       "public enables CodeQL",
			visibility: "public",
			wantCodeQL: true,
		},
		{
			name:       "private skips CodeQL",
			visibility: "private",
			wantCodeQL: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := map[string]bool{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths[r.Method+":"+r.URL.Path] = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			if err := c.EnableSecurity("owner", "repo", tt.visibility); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !paths["PUT:/repos/owner/repo/vulnerability-alerts"] {
				t.Error("expected vulnerability-alerts PUT")
			}
			if !paths["PUT:/repos/owner/repo/automated-security-fixes"] {
				t.Error("expected automated-security-fixes PUT")
			}
			if !paths["PATCH:/repos/owner/repo"] {
				t.Error("expected repo PATCH for secret scanning")
			}
			hasCodeQL := paths["PATCH:/repos/owner/repo/code-scanning/default-setup"]
			if hasCodeQL != tt.wantCodeQL {
				t.Errorf("CodeQL: got %v, want %v", hasCodeQL, tt.wantCodeQL)
			}
		})
	}
}

func TestEnableSecurity_NetworkError(t *testing.T) {
	c := newErroringClient()
	err := c.EnableSecurity("owner", "repo", "private")
	if err == nil {
		t.Fatal("expected error on network failure")
	}
}

func TestEnableSecurity_SecretScanningBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case http.MethodPatch:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"internal error"}`))
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.EnableSecurity("owner", "repo", "private")
	if err == nil {
		t.Fatal("expected error for bad secret-scanning status")
	}
}

func TestEnableCodeQL_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// PUTs and the first PATCH (secret scanning) succeed; the code-scanning
	// PATCH fails at the transport level, exercising enableCodeQL's err != nil branch.
	c := newSplitTransportClient(srv.URL, "code-scanning")
	err := c.EnableSecurity("owner", "repo", "public")
	if err == nil {
		t.Fatal("expected error when CodeQL enable fails at the transport level")
	}
}

func TestEnableCodeQL_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "code-scanning"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Advanced Security required"}`))
		case r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	err := c.EnableSecurity("owner", "repo", "public")
	if err == nil {
		t.Fatal("expected error for bad CodeQL status")
	}
}
