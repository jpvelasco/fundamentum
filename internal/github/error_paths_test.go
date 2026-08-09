package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// failReadCloser is an io.ReadCloser whose reads always fail, simulating a
// body that errors mid-read (connection reset while streaming a response).
type failReadCloser struct{}

func (failReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("simulated read failure")
}

func (failReadCloser) Close() error { return nil }

func TestDecodeExistingContentsFile_ReadError(t *testing.T) {
	resp := &http.Response{Body: failReadCloser{}}
	_, err := decodeExistingContentsFile(resp, ".github/README.md")
	if err == nil {
		t.Fatal("expected error from body read failure")
	}
}

func TestDecodeExistingContentsFile_ParseError(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(`not json`))}
	_, err := decodeExistingContentsFile(resp, ".github/README.md")
	if err == nil {
		t.Fatal("expected error for invalid JSON body")
	}
}

// invalidJSONHandler serves the given method-specific status with a body that
// is valid per status but not parseable as a Contents API response.
func TestCheckExistingFile_DecodeError(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}), nil, func(c *Client) {
		action, sha, err := c.checkExistingFile("/repos/owner/repo/contents/test.md", "test.md", []byte("data"))
		if err == nil {
			t.Fatal("expected decode error from checkExistingFile")
		}
		if action != fileActionSkip {
			t.Errorf("expected fileActionSkip on error, got %v", action)
		}
		if sha != "" {
			t.Errorf("expected empty sha on error, got %q", sha)
		}
	}, nil)
}

func TestAnyFileExists_NetworkError(t *testing.T) {
	c := newErroringClient()
	_, err := c.AnyFileExists("owner", "repo", []string{"README.md"})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestCreateRepo_NetworkError(t *testing.T) {
	c := newErroringClient()
	if err := c.CreateRepo("repo", false); err == nil {
		t.Fatal("expected network error")
	}
}

func TestGetRepoVisibility_NetworkError(t *testing.T) {
	c := newErroringClient()
	if _, err := c.GetRepoVisibility("owner", "repo"); err == nil {
		t.Fatal("expected network error")
	}
}

func TestGetRepoVisibility_DecodeError(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}), nil, func(c *Client) {
		if _, err := c.GetRepoVisibility("owner", "repo"); err == nil {
			t.Fatal("expected decode error")
		}
	}, nil)
}

func TestRulesetExists_NetworkError(t *testing.T) {
	c := newErroringClient()
	if _, err := c.RulesetExists("owner", "repo", "protect-main"); err == nil {
		t.Fatal("expected network error")
	}
}

func TestRulesetExists_DecodeError(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}), nil, func(c *Client) {
		if _, err := c.RulesetExists("owner", "repo", "protect-main"); err == nil {
			t.Fatal("expected decode error")
		}
	}, nil)
}

func TestCreateBranchRuleset_NetworkError(t *testing.T) {
	c := newErroringClient()
	if err := c.CreateBranchRuleset("owner", "repo", nil, BranchProtectionOptions{}); err == nil {
		t.Fatal("expected network error")
	}
}

func TestCreateTagRuleset_NetworkError(t *testing.T) {
	c := newErroringClient()
	if err := c.CreateTagRuleset("owner", "repo"); err == nil {
		t.Fatal("expected network error")
	}
}

func TestEnableSecurity_SecretScanningNetworkError(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}), func(url string) *Client {
		return newMethodSplitTransportClient(url, http.MethodPatch)
	}, func(c *Client) {
		if err := c.EnableSecurity("owner", "repo", "private", false); err == nil {
			t.Fatal("expected network error on secret scanning PATCH")
		}
	}, nil)
}

func TestApplyGeneralSettings_NetworkError(t *testing.T) {
	c := newErroringClient()
	if err := c.ApplyGeneralSettings("owner", "repo"); err == nil {
		t.Fatal("expected network error")
	}
}

func TestCreatePRBranch_DecodeError(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}), nil, func(c *Client) {
		if err := c.CreatePRBranch("owner", "repo", "feat/x", "main"); err == nil {
			t.Fatal("expected decode error")
		}
	}, nil)
}

func TestCreatePullRequest_DecodeError(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`not json`))
	}), nil, func(c *Client) {
		if _, err := c.CreatePullRequest("owner", "repo", "t", "b", "head", "main"); err == nil {
			t.Fatal("expected decode error")
		}
	}, nil)
}