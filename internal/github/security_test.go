package github

import (
	"encoding/json"
	"net/http"
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
			srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths[r.Method+":"+r.URL.Path] = true
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]any{})
			}))
			defer srv.Close()
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

func TestEnableSecurity_Errors(t *testing.T) {
	tests := []struct {
		name       string
		visibility string
		handler    http.HandlerFunc
		client     func(srv string) *Client
	}{
		{
			name:       "network error",
			visibility: "private",
			client:     func(string) *Client { return newErroringClient() },
			handler:    func(w http.ResponseWriter, r *http.Request) {},
		},
		{
			name:       "secret scanning bad status",
			visibility: "private",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodPut:
					w.WriteHeader(http.StatusOK)
				case http.MethodPatch:
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]string{"message": "internal error"})
				}
			},
		},
		{
			name:       "CodeQL network error",
			visibility: "public",
			client:     func(srv string) *Client { return newSplitTransportClient(srv, "code-scanning") },
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:       "CodeQL bad status",
			visibility: "public",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPut:
					w.WriteHeader(http.StatusOK)
				case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "code-scanning"):
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(map[string]string{"message": "Advanced Security required"})
				case r.Method == http.MethodPatch:
					w.WriteHeader(http.StatusOK)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := newTestServer(tt.handler)
			defer srv.Close()

			var c *Client
			if tt.client != nil {
				c = tt.client(srv.URL)
			} else {
				c = NewClient("t", false).WithBaseURL(srv.URL)
			}
			err := c.EnableSecurity("owner", "repo", tt.visibility)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
