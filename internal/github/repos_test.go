package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCreateRepo(t *testing.T) {
	tests := []struct {
		name       string
		owner      string
		repo       string
		handler    http.HandlerFunc
		wantErr    bool
		errContain string
	}{
		{
			name:  "authenticated user posts /user/repos with auto_init",
			owner: "alice",
			repo:  "repo",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/user":
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"login": "alice"})
				case r.Method == http.MethodPost && r.URL.Path == "/user/repos":
					body, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(body), `"auto_init":true`) {
						t.Errorf("expected auto_init true, body=%s", body)
					}
					if !strings.Contains(string(body), `"name":"repo"`) {
						t.Errorf("expected name repo, body=%s", body)
					}
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]string{"full_name": "alice/repo"})
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			},
		},
		{
			name:  "organization posts /orgs/{org}/repos with auto_init",
			owner: "my-org",
			repo:  "repo",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/user":
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"login": "alice"})
				case r.Method == http.MethodGet && r.URL.Path == "/users/my-org":
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"type": "Organization"})
				case r.Method == http.MethodPost && r.URL.Path == "/orgs/my-org/repos":
					body, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(body), `"auto_init":true`) {
						t.Errorf("expected auto_init true, body=%s", body)
					}
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]string{"full_name": "my-org/repo"})
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			},
		},
		{
			name:  "other user is rejected",
			owner: "bob",
			repo:  "repo",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/user":
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"login": "alice"})
				case r.Method == http.MethodGet && r.URL.Path == "/users/bob":
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"type": "User"})
				case r.Method == http.MethodPost:
					t.Error("must not POST create-repo for another user")
					w.WriteHeader(http.StatusCreated)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantErr:    true,
			errContain: "not the authenticated user",
		},
		{
			name:  "empty owner is rejected",
			owner: "",
			repo:  "repo",
			handler: func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("must not call API for empty owner: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:    true,
			errContain: "owner is required",
		},
		{
			name:  "user login is case-insensitive",
			owner: "Alice",
			repo:  "repo",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/user":
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"login": "alice"})
				case r.Method == http.MethodPost && r.URL.Path == "/user/repos":
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{})
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithServer(t, tt.handler, nil, func(c *Client) {
				err := c.CreateRepo(tt.owner, tt.repo, false)
				if tt.wantErr {
					if err == nil {
						t.Fatal("expected error, got nil")
					}
					if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
						t.Errorf("error %q does not contain %q", err.Error(), tt.errContain)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}, nil)
		})
	}
}

func TestCreateRepo_POST422(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/user" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"login": "alice"})
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "taken"})
	}), nil, func(c *Client) {
		err := c.CreateRepo("alice", "taken", false)
		if err == nil {
			t.Fatal("expected 422 error")
		}
		if !strings.Contains(err.Error(), "422") {
			t.Errorf("expected 422 in error, got: %v", err)
		}
	}, nil)
}

func TestAuthenticatedLogin_Errors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "decode error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`not json`))
			},
		},
		{
			name: "empty login",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]string{"login": ""})
			},
		},
		{
			name: "non-200",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithServer(t, tt.handler, nil, func(c *Client) {
				if _, err := c.authenticatedLogin(); err == nil {
					t.Fatal("expected error")
				}
			}, nil)
		})
	}
}

func TestAccountType_Errors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "decode error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`not json`))
			},
		},
		{
			name: "empty type",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]string{"type": ""})
			},
		},
		{
			name: "not found",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithServer(t, tt.handler, nil, func(c *Client) {
				if _, err := c.accountType("missing"); err == nil {
					t.Fatal("expected error")
				}
			}, nil)
		})
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
			srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo" {
					t.Errorf("expected /repos/owner/repo, got %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK && tt.response != "" {
					var out any
					_ = json.Unmarshal([]byte(tt.response), &out)
					_ = json.NewEncoder(w).Encode(out)
				}
			}))
			defer srv.Close()
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
