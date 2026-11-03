package github

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// revertOps drives every revert helper against a failing server.
var revertOps = map[string]func(*Client) error{
	"GetFileContent": func(c *Client) error { _, _, _, err := c.GetFileContent("o", "r", "f.yml", "main"); return err },
	"GetRulesetBody": func(c *Client) error { _, _, err := c.GetRulesetBody("o", "r", "protect-main"); return err },
	"DeleteRuleset":  func(c *Client) error { return c.DeleteRuleset("o", "r", "protect-main") },
	"UpdateRepoSettings": func(c *Client) error {
		return c.UpdateRepoSettings("o", "r", SettingsState{})
	},
	"DisableSecurity": func(c *Client) error { return c.DisableSecurity("o", "r", SecurityState{}) },
	"DeleteFile":      func(c *Client) error { return c.DeleteFile("o", "r", "f.yml") },
	"RulesetIDForUpdate": func(c *Client) error {
		_, err := c.RulesetIDForUpdate("o", "r", "protect-main")
		return err
	},
}

func TestRevertOps_StatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := NewClient("t", false).WithBaseURL(srv.URL)
	for name, op := range revertOps {
		t.Run(name, func(t *testing.T) {
			if err := op(c); err == nil {
				t.Fatal("expected error on 401")
			}
		})
	}
}

func TestRevertOps_TransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	c := NewClient("t", false).WithBaseURL(srv.URL)
	srv.Close() // every request now fails to connect
	for name, op := range revertOps {
		t.Run(name, func(t *testing.T) {
			if err := op(c); err == nil {
				t.Fatal("expected transport error")
			}
		})
	}
}

func TestGetFileContent_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	if _, _, _, err := NewClient("t", false).WithBaseURL(srv.URL).GetFileContent("o", "r", "f.yml", ""); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestDeleteFile_MissingIsNoop(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if err := NewClient("t", false).WithBaseURL(srv.URL).DeleteFile("o", "r", "f.yml"); err != nil {
		t.Fatalf("missing file must be a no-op: %v", err)
	}
}

func TestDeleteFile_DeleteRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"content":"eA==","sha":"s1"}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if err := NewClient("t", false).WithBaseURL(srv.URL).DeleteFile("o", "r", "f.yml"); err == nil {
		t.Fatal("expected error when DELETE is rejected")
	}
}

func TestDisableSecurity_TurnsOffEverythingApplyEnabled(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPatch {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	if err := NewClient("t", false).WithBaseURL(srv.URL).DisableSecurity("o", "r", SecurityState{}); err != nil {
		t.Fatalf("DisableSecurity error: %v", err)
	}
	got := strings.Join(seen, ",")
	for _, want := range []string{"DELETE /repos/o/r/vulnerability-alerts", "DELETE /repos/o/r/automated-security-fixes", "PATCH /repos/o/r"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing request %q in %s", want, got)
		}
	}
}

func TestDisableSecurity_SecretScanningRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	if err := NewClient("t", false).WithBaseURL(srv.URL).DisableSecurity("o", "r", SecurityState{DependabotAlerts: true, AutomatedFixes: true}); err == nil {
		t.Fatal("expected error when secret-scanning patch is rejected")
	}
}
