package github

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestEnableSecurity(t *testing.T) {
	tests := []struct {
		name           string
		opts           SecurityOptions
		wantCodeQL     bool
		wantSecretScan bool
	}{
		{
			name:           "public enables CodeQL and secret scanning",
			opts:           SecurityOptions{Visibility: "public"},
			wantCodeQL:     true,
			wantSecretScan: true,
		},
		{
			name:           "public with advanced workflow skips default setup",
			opts:           SecurityOptions{Visibility: "public", AdvancedCodeQL: true},
			wantCodeQL:     false,
			wantSecretScan: true,
		},
		{
			name:           "private skips GHAS unless opted in",
			opts:           SecurityOptions{Visibility: "private"},
			wantCodeQL:     false,
			wantSecretScan: false,
		},
		{
			name:           "private with paid features enables secret scanning only",
			opts:           SecurityOptions{Visibility: "private", PaidFeatures: true},
			wantCodeQL:     false,
			wantSecretScan: true,
		},
		{
			name:           "internal skips GHAS unless opted in",
			opts:           SecurityOptions{Visibility: "internal"},
			wantCodeQL:     false,
			wantSecretScan: false,
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
			if err := c.EnableSecurity("owner", "repo", tt.opts); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !paths["PUT:/repos/owner/repo/vulnerability-alerts"] {
				t.Error("expected vulnerability-alerts PUT")
			}
			if !paths["PUT:/repos/owner/repo/automated-security-fixes"] {
				t.Error("expected automated-security-fixes PUT")
			}
			hasSecret := paths["PATCH:/repos/owner/repo"]
			if hasSecret != tt.wantSecretScan {
				t.Errorf("secret scanning PATCH: got %v, want %v", hasSecret, tt.wantSecretScan)
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
		name    string
		opts    SecurityOptions
		handler http.HandlerFunc
		client  func(srv string) *Client
	}{
		{
			name:    "network error",
			opts:    SecurityOptions{Visibility: "private"},
			client:  func(string) *Client { return newErroringClient() },
			handler: func(w http.ResponseWriter, r *http.Request) {},
		},
		{
			name:   "secret scanning bad status",
			opts:   SecurityOptions{Visibility: "private", PaidFeatures: true},
			client: newZeroDelayClient,
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
			name:   "CodeQL network error",
			opts:   SecurityOptions{Visibility: "public"},
			client: func(srv string) *Client { return newSplitTransportClient(srv, "code-scanning") },
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "CodeQL bad status",
			opts: SecurityOptions{Visibility: "public"},
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
			err := c.EnableSecurity("owner", "repo", tt.opts)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestIsPublicVisibility(t *testing.T) {
	if !IsPublicVisibility("public") {
		t.Error("public should be public")
	}
	if IsPublicVisibility("private") || IsPublicVisibility("internal") || IsPublicVisibility("") {
		t.Error("non-public visibilities must not count as public")
	}
}
