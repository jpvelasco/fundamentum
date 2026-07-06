package github

import (
	"net/http"
	"net/http/httptest"
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