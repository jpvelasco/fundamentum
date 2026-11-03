package apply

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

func existsPlan(exists bool) github.RulesetPlan {
	return github.RulesetPlan{Exists: exists}
}

func newBuildItemsTest(t *testing.T, handler http.HandlerFunc, visibility string) []wizard.Item {
	t.Helper()
	return newBuildItemsTestFull(t, handler, visibility, false, false, false)
}

// newBuildItemsTestFull is newBuildItemsTest with explicit control over the
// rulesetExists/tagExists/classicExists inputs to buildItems.

func newBuildItemsTestFull(t *testing.T, handler http.HandlerFunc, visibility string, rulesetExists, tagExists, classicExists bool) []wizard.Item {
	t.Helper()
	srv, c := newTestServer(handler)
	defer srv.Close()
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: visibility}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	return buildItemsForTest(t, c, visibility, rendered, existsPlan(rulesetExists), existsPlan(tagExists), classicExists)
}

func TestNewCmd(t *testing.T) {
	cmd := NewCmd()
	if cmd.Use != "apply OWNER/REPO" {
		t.Errorf("expected use 'apply OWNER/REPO', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected non-empty Short")
	}
	if !strings.Contains(cmd.Long, "--ci generic") {
		t.Error("expected --ci example in Long help")
	}
	if !strings.Contains(cmd.Long, "settings still apply live") {
		t.Error("expected --pr help to say settings still apply live")
	}
	if !strings.Contains(cmd.Long, "--preset oss") {
		t.Error("expected --preset example in Long help")
	}
}

func TestRun_MissingToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Cleanup(func() { globals.Token = "" })
	globals.Token = ""
	stubGhTokenFailure(t)
	cmd := NewCmd()
	cmd.SetArgs([]string{"owner/repo"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "GitHub token is required") {
		t.Fatalf("expected missing-token error, got %v", err)
	}
}

func TestRun_InvalidArg(t *testing.T) {
	cmd := NewCmd()
	cmd.SetArgs([]string{"norepo"})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid arg")
	}
}

func TestRun_NoArg(t *testing.T) {
	cmd := NewCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for missing arg")
	}
}

func TestRun_ArgValidation(t *testing.T) {
	cmd := NewCmd()
	// Valid arg format passes validation
	err := cmd.Args(cmd, []string{"a/b"})
	if err != nil {
		t.Errorf("unexpected arg error: %v", err)
	}
	// No args fails
	err = cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error for missing arg")
	}
}

// lineReader returns input one line at a time, mimicking terminal line-buffered
// input so sequential bufio.Scanner-based prompts each see their own line.
// (strings.Reader hands back the whole stream in one Read, so the first prompt
// buffers everything and later prompts hit EOF.)

type lineReader struct {
	lines []string
}

func newLineReader(input string) *lineReader {
	return &lineReader{lines: strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")}
}

func (r *lineReader) Read(p []byte) (int, error) {
	if len(r.lines) == 0 {
		return 0, io.EOF
	}
	line := r.lines[0] + "\n"
	r.lines = r.lines[1:]
	n := copy(p, line)
	if n < len(line) {
		r.lines = append([]string{line[n:]}, r.lines...)
	}
	return n, nil
}

func TestRun_Success(t *testing.T) {
	t.Cleanup(func() { newClient = github.NewClient })

	srv := newRunFlowServer()
	defer srv.Close()
	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}

	t.Cleanup(func() { globals.Token = "" })
	globals.Token = "t"
	cmd := NewCmd()
	var out strings.Builder
	cmd.SetIn(newLineReader("solo\ny\n"))
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"owner/repo"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("run() error: %v", err)
	}
	if !strings.Contains(out.String(), "✓ Done — https://github.com/owner/repo") {
		t.Errorf("expected done message, got:\n%s", out.String())
	}
}

// TestRunWithClient_RenderError verifies template-render failures surface a
// wrapped error.
