package github

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCreatePRBranch(t *testing.T) {
	tests := []struct {
		name      string
		client    func(srv string) *Client
		handler   http.HandlerFunc
		wantErr   bool
		errSubstr string
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"commit": map[string]any{"sha": "aaaa1111"},
					})
				case http.MethodPost:
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"ref": "refs/heads/test-branch",
					})
				}
			},
		},
		{
			name: "GET error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
		{
			// Given a missing base branch, the error must name the failing
			// operation — not a confusing "decode branch ... EOF".
			name: "missing base branch names the get",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			},
			wantErr:   true,
			errSubstr: "get branch",
		},
		{
			name:    "network error",
			client:  func(string) *Client { return newErroringClient() },
			handler: func(w http.ResponseWriter, r *http.Request) {},
			wantErr: true,
		},
		{
			name:   "POST network error",
			client: func(srv string) *Client { return newSplitTransportClient(srv, "/git/refs") },
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"commit": map[string]any{"sha": "aaaa1111"},
				})
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithServer(t, tt.handler, tt.client, func(c *Client) {
				err := c.CreatePRBranch("owner", "repo", "test-branch", "main")
				if tt.wantErr {
					if err == nil {
						t.Fatal("expected error, got nil")
					}
					if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
						t.Errorf("expected error to contain %q, got: %v", tt.errSubstr, err)
					}
				} else if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}, nil)
		})
	}
}

func TestCreatePullRequest(t *testing.T) {
	tests := []struct {
		name    string
		client  func(srv string) *Client
		handler http.HandlerFunc
		wantErr bool
		wantNum int
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["title"] != "test title" {
					t.Errorf("expected title 'test title', got %v", body["title"])
				}
				if body["head"] != "feature" {
					t.Errorf("expected head 'feature', got %v", body["head"])
				}
				if body["base"] != "main" {
					t.Errorf("expected base 'main', got %v", body["base"])
				}
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{"number": 42})
			},
			wantNum: 42,
		},
		{
			name: "HTTP error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"message":"invalid"}`))
			},
			wantErr: true,
		},
		{
			name:    "network error",
			client:  func(string) *Client { return newErroringClient() },
			handler: func(w http.ResponseWriter, r *http.Request) {},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithServer(t, tt.handler, tt.client, func(c *Client) {
				num, err := c.CreatePullRequest("owner", "repo", "test title", "test body", "feature", "main")
				if tt.wantErr {
					if err == nil {
						t.Fatal("expected error, got nil")
					}
				} else {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					if num != tt.wantNum {
						t.Errorf("expected PR number %d, got %d", tt.wantNum, num)
					}
				}
			}, nil)
		})
	}
}

func TestUpsertFileOnBranch(t *testing.T) {
	tests := []struct {
		name       string
		client     func(srv string) *Client
		handler    http.HandlerFunc
		filePath   string
		content    []byte
		wantErr    bool
		wantLock   bool
		wantAction string
	}{
		{
			name:     "network error on GET",
			client:   func(string) *Client { return newErroringClient() },
			handler:  func(w http.ResponseWriter, r *http.Request) {},
			filePath: "test.md",
			content:  []byte("hello"),
			wantErr:  true,
		},
		{
			name:   "PUT network error",
			client: func(srv string) *Client { return newMethodSplitTransportClient(srv, http.MethodPut) },
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			filePath: "test.md",
			content:  []byte("hello"),
			wantErr:  true,
		},
		{
			name: "create",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					w.WriteHeader(http.StatusNotFound)
				case http.MethodPut:
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["branch"] != "feature" {
						t.Errorf("expected branch 'feature', got %v", body["branch"])
					}
					msg, _ := body["message"].(string)
					if strings.Contains(msg, "fundamentum") {
						t.Errorf("commit message should not contain 'fundamentum': %s", msg)
					}
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{})
				}
			},
			filePath:   "test.md",
			content:    []byte("hello"),
			wantAction: "created",
		},
		{
			name: "update",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"content": "dGVzdA==",
						"sha":     "abc123",
					})
				case http.MethodPut:
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					msg, _ := body["message"].(string)
					if strings.Contains(msg, "fundamentum") {
						t.Errorf("commit message should not contain 'fundamentum': %s", msg)
					}
					if !strings.Contains(msg, "chore: update") {
						t.Errorf("expected update message, got: %s", msg)
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{})
				}
			},
			filePath:   "test.md",
			content:    []byte("new content"),
			wantAction: "updated",
		},
		{
			name: "workflow 404 (locked)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"content": base64.StdEncoding.EncodeToString([]byte("old")),
						"sha":     "abc123",
					})
				case http.MethodPut:
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				}
			},
			filePath:   ".github/workflows/ci.yml",
			content:    []byte("new content"),
			wantErr:    true,
			wantLock:   true,
			wantAction: "skipped",
		},
		{
			name: "404 on create (real error)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					w.WriteHeader(http.StatusNotFound)
				case http.MethodPut:
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				}
			},
			filePath: "new_file.md",
			content:  []byte("hello"),
			wantErr:  true,
		},
		{
			name: "skip (content unchanged)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"content": "aGVsbG8=",
						"sha":     "abc123",
					})
				}
			},
			filePath:   "test.md",
			content:    []byte("hello"),
			wantAction: "skipped",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithServer(t, tt.handler, tt.client, func(c *Client) {
				action, err := c.UpsertFileOnBranch("owner", "repo", "feature", tt.filePath, tt.content)

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
			}, nil)
		})
	}
}

func TestIsConflict409(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"409: rule violations", true},
		{"409: GH013", true},
		{"409: something else", false},
		{"403: rule violations", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			err := &testErr{msg: tt.msg}
			if got := IsConflict409(err); got != tt.want {
				t.Errorf("IsConflict409(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
	if got := IsConflict409(nil); got {
		t.Error("IsConflict409(nil) = true, want false")
	}
}

func TestIsForbidden403(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"403 Forbidden: Forbidden", true},
		{"create branch ruleset: 403 Forbidden: {\"message\":\"Forbidden\"}", true},
		{"403: rule violations", true},
		{"409: rule violations", false},
		{"404: not found", false},
		{"422 Unprocessable Entity: validation failed", false},
		{"500 Internal Server Error", false},
		{"network error: connection refused", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			err := &testErr{msg: tt.msg}
			if got := IsForbidden403(err); got != tt.want {
				t.Errorf("IsForbidden403(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
	if got := IsForbidden403(nil); got {
		t.Error("IsForbidden403(nil) = true, want false")
	}
}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

func TestHTTPError_Error(t *testing.T) {
	e := &HTTPError{StatusCode: http.StatusConflict, Msg: "409 Conflict: rule violations"}
	if e.Error() != "409 Conflict: rule violations" {
		t.Errorf("Error() = %q, want %q", e.Error(), "409 Conflict: rule violations")
	}
}

func TestIsConflict409_HTTPError(t *testing.T) {
	if !IsConflict409(&HTTPError{StatusCode: http.StatusConflict, Msg: "conflict"}) {
		t.Error("expected true for HTTPError with StatusCode 409")
	}
	if IsConflict409(&HTTPError{StatusCode: http.StatusForbidden, Msg: "forbidden"}) {
		t.Error("expected false for HTTPError with a non-409 StatusCode")
	}
}

func TestIsBranchProtectionUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"generic 403", &HTTPError{StatusCode: http.StatusForbidden, Msg: "403 Forbidden: Forbidden"}, false},
		{"token scope", &HTTPError{StatusCode: http.StatusForbidden, Msg: "403 Forbidden: Resource not accessible by integration"}, false},
		{"upgrade to pro", &HTTPError{StatusCode: http.StatusForbidden, Msg: "403 Forbidden: Upgrade to GitHub Pro or make this repository public to enable this feature."}, true},
		{"make public", &HTTPError{StatusCode: http.StatusForbidden, Msg: "403 Forbidden: make this repository public"}, true},
		{"github team", fmt.Errorf("create branch ruleset: 403 Forbidden: GitHub Team is required"), false},
		{"free plan", &HTTPError{StatusCode: http.StatusForbidden, Msg: "403: this repository is using the free plan"}, false},
		{"not available", &HTTPError{StatusCode: http.StatusForbidden, Msg: "403 Forbidden: Branch protection is not available for this repository"}, false},
		{"409", &HTTPError{StatusCode: http.StatusConflict, Msg: "409 Conflict"}, false},
		{"upgrade text non-403", fmt.Errorf("upgrade to github pro required, but the request was rejected"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBranchProtectionUnavailable(tt.err); got != tt.want {
				t.Errorf("IsBranchProtectionUnavailable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsForbidden403_HTTPError(t *testing.T) {
	if !IsForbidden403(&HTTPError{StatusCode: http.StatusForbidden, Msg: "forbidden"}) {
		t.Error("expected true for HTTPError with StatusCode 403")
	}
	if IsForbidden403(&HTTPError{StatusCode: http.StatusConflict, Msg: "conflict"}) {
		t.Error("expected false for HTTPError with a non-403 StatusCode")
	}
}

// Given two consecutive PR-mode runs, each must get its own branch name so a
// second apply in the same second cannot collide with the first.
func TestApplyViaPR_UniqueBranchNames(t *testing.T) {
	var refs []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"sha": "aaaa1111"}})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/refs") && !strings.Contains(r.URL.Path, "/heads/"):
			var body struct {
				Ref string `json:"ref"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			refs = append(refs, body.Ref)
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/harden-"):
			_, _ = w.Write([]byte(`{"object":{"sha":"basesha"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/"):
			_, _ = w.Write([]byte(`{"tree":{"sha":"basesha-tree"}}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/blobs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"blobsha"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/trees"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"newtree"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/commits"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"newcommit"}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/harden-"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/pulls"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":42}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	testWithServer(t, handler, nil, func(c *Client) {
		changes := []FileChange{{Path: "README.md", Content: []byte("hi")}}
		if _, err := c.ApplyViaPR("owner", "repo", "main", changes, io.Discard); err != nil {
			t.Fatalf("first ApplyViaPR: %v", err)
		}
		if _, err := c.ApplyViaPR("owner", "repo", "main", changes, io.Discard); err != nil {
			t.Fatalf("second ApplyViaPR: %v", err)
		}
	}, func(t *testing.T) {
		if len(refs) != 2 {
			t.Fatalf("expected 2 branch refs created, got %d", len(refs))
		}
		if refs[0] == refs[1] {
			t.Errorf("expected distinct branch refs, got %q twice", refs[0])
		}
	})
}

// Given a real 409 from the API, the whole client path must produce an error
// that IsConflict409 classifies structurally (the apply auto-fallback to PR
// mode depends on this).
func TestIsConflict409_ProductionError(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"branch protection rule violations (GH013)"}`))
	}), nil, func(c *Client) {
		err := c.CreateBranchRuleset("owner", "repo", nil, BranchProtectionOptions{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !IsConflict409(err) {
			t.Errorf("expected IsConflict409 to be true for a production 409, got: %v", err)
		}
	}, nil)
}

// Given a real 403 from the API, IsForbidden403 must classify it structurally
// (the ruleset → classic fallback for free-tier private repos depends on this).
func TestIsForbidden403_ProductionError(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
	}), nil, func(c *Client) {
		err := c.CreateBranchRuleset("owner", "repo", nil, BranchProtectionOptions{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !IsForbidden403(err) {
			t.Errorf("expected IsForbidden403 to be true for a production 403, got: %v", err)
		}
	}, nil)
}

// Given a workflow file update rejected with 404 (GitHub Actions lock), the
// returned error must satisfy errors.Is against ErrWorkflowLocked so apply can
// treat it as "skip" rather than failure.
func TestIsWorkflowLocked_ErrorsIs(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"b2xk","sha":"abc"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), nil, func(c *Client) {
		_, err := c.UpsertFile("owner", "repo", ".github/workflows/ci.yml", []byte("new"))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrWorkflowLocked) {
			t.Errorf("expected errors.Is(err, ErrWorkflowLocked), got: %v", err)
		}
	}, nil)
}

func TestDeleteBranch_Errors(t *testing.T) {
	c := newErroringClient()
	if err := c.deleteBranch("owner", "repo", "harden-main-1"); err == nil {
		t.Fatal("expected network error")
	}
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusConflict)
	}), nil, func(c *Client) {
		if err := c.deleteBranch("owner", "repo", "harden-main-1"); err == nil {
			t.Fatal("expected status error")
		}
	}, nil)
}

func TestApplyViaPR(t *testing.T) {
	// gitDataPRHandler mocks the full single-commit PR flow:
	// CreatePRBranch (GET /branches/{base}, POST /git/refs) ->
	// CommitFiles on the harden branch for non-workflow files (GET
	// /git/ref, GET /git/commits, POST /git/blobs, /git/trees,
	// /git/commits, PATCH /git/refs/heads) -> per-file Contents upsert
	// for workflow files (GET/PUT /contents) -> POST /pulls.
	// failStage aborts one named stage with failStatus.
	gitDataPRHandler := func(t *testing.T, failStage string, failStatus int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method == failMethod(failStage) && failPathMatch(failStage, r.URL.Path) {
				w.WriteHeader(failStatus)
				_, _ = w.Write([]byte(`{}`))
				return
			}
			switch {
			case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/main"):
				_, _ = w.Write([]byte(`{"commit":{"sha":"basesha"}}`))
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/refs") && !strings.Contains(r.URL.Path, "/heads/"):
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{}`))
			case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/harden-"):
				_, _ = w.Write([]byte(`{"object":{"sha":"basesha"}}`))
			case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/"):
				_, _ = w.Write([]byte(`{"tree":{"sha":"basesha-tree"}}`))
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/blobs"):
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"sha":"blobsha"}`))
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/trees"):
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"sha":"newtree"}`))
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/commits"):
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"sha":"newcommit"}`))
			case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/harden-"):
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
				// Workflow file does not exist on the harden branch.
				w.WriteHeader(http.StatusNotFound)
			case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/"):
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{}`))
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/pulls"):
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"number":7}`))
			case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/git/refs/heads/harden-"):
				w.WriteHeader(http.StatusNoContent)
			default:
				t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		changes    []FileChange
		wantErr    bool
		errMsg     string
		wantZeroPR bool
	}{
		{
			name:    "happy path with 2 files incl a workflow",
			handler: gitDataPRHandler(t, "", 0),
			changes: []FileChange{
				{Path: "README.md", Content: []byte("hello")},
				{Path: ".github/workflows/ci.yml", Content: []byte("name: CI")},
			},
		},
		{
			// The workflow-scope gate 404s the Contents PUT; the workflow
			// file is skipped with a warning and the PR still opens with
			// the batched files.
			name:    "refused workflow write skipped, PR opens with the rest",
			handler: gitDataPRHandler(t, "contents-put", http.StatusNotFound),
			changes: []FileChange{
				{Path: "README.md", Content: []byte("hello")},
				{Path: ".github/workflows/ci.yml", Content: []byte("name: CI")},
			},
		},
		{
			// Only workflow files and every write refused: nothing to put
			// in a PR — the harden branch is deleted and no PR opens.
			name:       "all workflow writes refused means no PR",
			handler:    gitDataPRHandler(t, "contents-put", http.StatusNotFound),
			changes:    []FileChange{{Path: ".github/workflows/ci.yml", Content: []byte("name: CI")}},
			wantZeroPR: true,
		},
		{
			// A workflow file already identical on the branch is not a
			// write — with nothing else to change, no PR opens.
			name: "workflow file identical on branch counts as no write",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/") {
					_, _ = w.Write([]byte(`{"content":"bmFtZTogQ0k=","sha":"x"}`)) // "name: CI"
					return
				}
				gitDataPRHandler(t, "", 0)(w, r)
			},
			changes:    []FileChange{{Path: ".github/workflows/ci.yml", Content: []byte("name: CI")}},
			wantZeroPR: true,
		},
		{
			// A non-locked workflow failure (e.g. 500) is a real error:
			// branch deleted, run fails.
			name:    "workflow upsert non-locked error fails the run",
			handler: gitDataPRHandler(t, "contents-put", http.StatusInternalServerError),
			changes: []FileChange{
				{Path: "README.md", Content: []byte("hello")},
				{Path: ".github/workflows/ci.yml", Content: []byte("name: CI")},
			},
			wantErr: true,
			errMsg:  "upsert .github/workflows/ci.yml",
		},
		{
			name:    "git ref read fails",
			handler: gitDataPRHandler(t, "ref-head", http.StatusForbidden),
			changes: []FileChange{{Path: "README.md", Content: []byte("hello")}},
			wantErr: true,
			errMsg:  "commit 1 files",
		},
		{
			name:    "blob create fails",
			handler: gitDataPRHandler(t, "blob", http.StatusForbidden),
			changes: []FileChange{{Path: "README.md", Content: []byte("hello")}},
			wantErr: true,
			errMsg:  "commit 1 files",
		},
		{
			name:    "tree create fails",
			handler: gitDataPRHandler(t, "tree", http.StatusInternalServerError),
			changes: []FileChange{{Path: "README.md", Content: []byte("hello")}},
			wantErr: true,
			errMsg:  "commit 1 files",
		},
		{
			name:    "commit create fails",
			handler: gitDataPRHandler(t, "commit", http.StatusInternalServerError),
			changes: []FileChange{{Path: "README.md", Content: []byte("hello")}},
			wantErr: true,
			errMsg:  "commit 1 files",
		},
		{
			name:    "ref update fails",
			handler: gitDataPRHandler(t, "ref-update", http.StatusUnprocessableEntity),
			changes: []FileChange{{Path: "README.md", Content: []byte("hello")}},
			wantErr: true,
			errMsg:  "commit 1 files",
		},
		{
			name:    "PR create fails and deletes the branch",
			handler: gitDataPRHandler(t, "pulls", http.StatusUnprocessableEntity),
			changes: []FileChange{{Path: "README.md", Content: []byte("hello")}},
			wantErr: true,
			errMsg:  "create PR",
		},
		{
			name: "empty changes",
			handler: func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("must not call GitHub when there are no changes: %s %s", r.Method, r.URL.Path)
			},
			changes:    nil,
			wantZeroPR: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithServer(t, tt.handler, nil, func(c *Client) {
				prNum, err := c.ApplyViaPR("owner", "repo", "main", tt.changes, io.Discard)
				if tt.wantErr {
					if err == nil {
						t.Fatal("expected error, got nil")
					}
					if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
					}
				} else {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					if tt.wantZeroPR {
						if prNum != 0 {
							t.Errorf("expected no PR, got #%d", prNum)
						}
					} else if prNum == 0 {
						t.Error("expected non-zero PR number")
					}
				}
			}, nil)
		})
	}
}

// failMethod / failPathMatch map a git-data stage name to the HTTP method
// and URL shape whose failure that stage simulates.
func failMethod(stage string) string {
	switch stage {
	case "ref-head":
		return http.MethodGet
	case "blob", "tree", "commit":
		return http.MethodPost
	case "ref-update":
		return http.MethodPatch
	case "contents-put":
		return http.MethodPut
	case "pulls":
		return http.MethodPost
	default:
		return "" // no stage: never matches
	}
}

func failPathMatch(stage, p string) bool {
	switch stage {
	case "ref-head":
		return strings.Contains(p, "/git/ref/heads/harden-")
	case "blob":
		return strings.Contains(p, "/git/blobs")
	case "tree":
		return strings.Contains(p, "/git/trees")
	case "commit":
		return strings.Contains(p, "/git/commits")
	case "ref-update":
		return strings.Contains(p, "/git/refs/heads/harden-")
	case "contents-put":
		return strings.Contains(p, "/contents/")
	case "pulls":
		return strings.Contains(p, "/pulls")
	default:
		return false
	}
}
