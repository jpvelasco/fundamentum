package templates

import (
	"strings"
	"testing"
)

// renderMap renders and indexes files by path for comparison.
func renderMap(t *testing.T, data RepoData) map[string]string {
	t.Helper()
	files, err := Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	out := make(map[string]string, len(files))
	for _, f := range files {
		if _, dup := out[f.Path]; dup {
			t.Fatalf("duplicate rendered path %q", f.Path)
		}
		out[f.Path] = f.Content
	}
	return out
}

// TestBotManagedOffGoldenIsBaseline is the golden test for the profile's
// off-by-default contract: with BotManaged false, rendering is byte-identical
// to the un-profiled baseline. It renders the same repo with BotManaged off
// and on and asserts the ONLY delta is the intended bot-managed one (octocov
// loses pull-requests: write on private repos); the file set never changes.
func TestBotManagedOffGoldenIsBaseline(t *testing.T) {
	cases := []struct {
		name       string
		visibility string
		pack       string
	}{
		{"public go", "public", CIPackGo},
		{"private go", "private", CIPackGo},
		{"public generic", "public", CIPackGeneric},
		{"private generic", "private", CIPackGeneric},
		{"public none", "public", CIPackNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := RepoData{Owner: "o", RepoName: "r", DefaultBranch: "main", Visibility: tc.visibility, CIPack: tc.pack, BotManaged: false}
			on := base
			on.BotManaged = true

			off := renderMap(t, base)
			onMap := renderMap(t, on)

			// Identical file set: the profile never adds or drops a file.
			if len(off) != len(onMap) {
				t.Fatalf("file count off=%d on=%d; bot-managed must not change the file set", len(off), len(onMap))
			}
			for path, offContent := range off {
				onContent, ok := onMap[path]
				if !ok {
					t.Errorf("bot-managed dropped file %q", path)
					continue
				}
				if offContent == onContent {
					continue
				}
				// The only permitted delta: private octocov loses its
				// pull-requests: write grant.
				if path == ".github/workflows/octocov.yml" {
					if strings.Contains(onContent, "pull-requests: write") {
						t.Errorf("bot-managed octocov still grants pull-requests: write")
					}
					if !strings.Contains(offContent, "pull-requests: write") {
						t.Errorf("baseline octocov lost pull-requests: write unexpectedly")
					}
					continue
				}
				t.Errorf("bot-managed changed %q (only octocov.yml may differ):\n--- off ---\n%s\n--- on ---\n%s", path, offContent, onContent)
			}
		})
	}
}

// TestBotManagedPrivateOctocovDropsPRWrite pins the specific transform:
// private octocov keeps contents: read but loses pull-requests: write.
func TestBotManagedPrivateOctocovDropsPRWrite(t *testing.T) {
	data := RepoData{Owner: "o", RepoName: "r", DefaultBranch: "main", Visibility: "private", CIPack: CIPackGo, BotManaged: true}
	m := renderMap(t, data)
	octo, ok := m[".github/workflows/octocov.yml"]
	if !ok {
		t.Fatal("private bot-managed repo must still ship octocov.yml")
	}
	if strings.Contains(octo, "pull-requests: write") {
		t.Errorf("bot-managed octocov must drop pull-requests: write:\n%s", octo)
	}
	if !strings.Contains(octo, "contents: read") {
		t.Errorf("bot-managed octocov must keep contents: read:\n%s", octo)
	}
	// Baseline (off) still has the grant.
	off := renderMap(t, RepoData{Owner: "o", RepoName: "r", DefaultBranch: "main", Visibility: "private", CIPack: CIPackGo})
	if !strings.Contains(off[".github/workflows/octocov.yml"], "pull-requests: write") {
		t.Error("baseline octocov must keep pull-requests: write")
	}
}

// TestBotManagedShipsNoPrivilegedTriggers asserts the profile never ships a
// workflow_run or pull_request_target trigger for any visibility/pack — the
// two triggers that run a workflow in a privileged context.
func TestBotManagedShipsNoPrivilegedTriggers(t *testing.T) {
	for _, visibility := range []string{"public", "private"} {
		for _, pack := range []string{CIPackGo, CIPackGeneric, CIPackNode, CIPackPython, CIPackRust, CIPackNone} {
			m := renderMap(t, RepoData{Owner: "o", RepoName: "r", DefaultBranch: "main", Visibility: visibility, CIPack: pack, BotManaged: true})
			for path, content := range m {
				if !strings.HasPrefix(path, ".github/workflows/") {
					continue
				}
				if strings.Contains(content, "workflow_run:") {
					t.Errorf("%s (%s/%s) ships a workflow_run trigger", path, visibility, pack)
				}
				if strings.Contains(content, "pull_request_target:") {
					t.Errorf("%s (%s/%s) ships a pull_request_target trigger", path, visibility, pack)
				}
			}
		}
	}
}

// TestBotManagedKeepsCodeQLWriteScope documents the one expected write scope:
// the CodeQL analyze job keeps security-events: write, which the profile
// accepts because CodeQL cannot report without it.
func TestBotManagedKeepsCodeQLWriteScope(t *testing.T) {
	m := renderMap(t, RepoData{Owner: "o", RepoName: "r", DefaultBranch: "main", Visibility: "public", CIPack: CIPackGo, BotManaged: true})
	codeql, ok := m[".github/workflows/codeql.yml"]
	if !ok {
		t.Fatal("bot-managed public repo must keep codeql.yml")
	}
	if !strings.Contains(codeql, "security-events: write") {
		t.Errorf("codeql.yml must keep its security-events: write (the one expected write scope):\n%s", codeql)
	}
}

// TestDropYAMLPermission exercises the permission-line stripper directly.
func TestDropYAMLPermission(t *testing.T) {
	in := "permissions:\n  contents: read\n  pull-requests: write\n\njobs:\n"
	got := dropYAMLPermission(in, "pull-requests: write")
	if strings.Contains(got, "pull-requests: write") {
		t.Errorf("permission not dropped:\n%s", got)
	}
	if !strings.Contains(got, "contents: read") {
		t.Errorf("unrelated permission wrongly dropped:\n%s", got)
	}
	// A comment mentioning the scope is not a grant and must survive.
	withComment := "permissions:\n  contents: read\n  pull-requests: write  # the one grant\n"
	got = dropYAMLPermission(withComment, "pull-requests: write")
	if !strings.Contains(got, "# the one grant") {
		t.Errorf("commented grant line should be kept (only the exact bare line is stripped):\n%s", got)
	}
}
