package templates

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestValidGitBranch(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"main", "main", true},
		{"slash", "feature/my-branch_1", true},
		{"dotted release", "release/1.2", true},
		{"plus", "release+stable", true},
		{"empty", "", false},
		{"angle brackets", "feat/<test>", false},
		{"leading slash", "/main", false},
		{"trailing slash", "main/", false},
		{"double slash", "feat//x", false},
		{"leading dot", ".hidden", false},
		{"trailing dot", "main.", false},
		{"double dot", "feat/../x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validGitBranch(tt.value)
			if got != tt.want {
				t.Errorf("validGitBranch(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestRepoDataSanitize(t *testing.T) {
	tests := []struct {
		name    string
		input   RepoData
		want    RepoData
		wantErr bool
	}{
		{
			name:  "valid input unchanged",
			input: RepoData{Owner: "jpvelasco", RepoName: "fundamentum", DefaultBranch: "main", Visibility: "private"},
			want:  RepoData{Owner: "jpvelasco", RepoName: "fundamentum", DefaultBranch: "main", Visibility: "private"},
		},
		{
			name:    "owner with special chars rejected",
			input:   RepoData{Owner: "jp<script>alert(1)</script>", RepoName: "repo", DefaultBranch: "main", Visibility: "public"},
			wantErr: true,
		},
		{
			name:    "empty owner rejected",
			input:   RepoData{Owner: "", RepoName: "repo", DefaultBranch: "main", Visibility: "public"},
			wantErr: true,
		},
		{
			name:    "empty repo rejected",
			input:   RepoData{Owner: "owner", RepoName: "", DefaultBranch: "main", Visibility: "public"},
			wantErr: true,
		},
		{
			name:    "empty branch rejected",
			input:   RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "", Visibility: "public"},
			wantErr: true,
		},
		{
			name:  "invalid visibility falls back to private",
			input: RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "secret"},
			want:  RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"},
		},
		{
			name:  "internal visibility uses private templates",
			input: RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "internal"},
			want:  RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"},
		},
		{
			name:  "visibility case normalized",
			input: RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "PRIVATE"},
			want:  RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"},
		},
		{
			name:  "branch with slash preserved",
			input: RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "feature/my-branch_1", Visibility: "public"},
			want:  RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "feature/my-branch_1", Visibility: "public"},
		},
		{
			name:    "branch with angle brackets rejected",
			input:   RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "feat/<test>", Visibility: "public"},
			wantErr: true,
		},
		{
			name:  "repo with dots preserved",
			input: RepoData{Owner: "owner", RepoName: "my.repo.name", DefaultBranch: "main", Visibility: "public"},
			want:  RepoData{Owner: "owner", RepoName: "my.repo.name", DefaultBranch: "main", Visibility: "public"},
		},
		{
			name:  "repo with underscores preserved",
			input: RepoData{Owner: "owner", RepoName: "my_repo_name", DefaultBranch: "main", Visibility: "public"},
			want:  RepoData{Owner: "owner", RepoName: "my_repo_name", DefaultBranch: "main", Visibility: "public"},
		},
		{
			name:  "dotted release branch preserved",
			input: RepoData{Owner: "example", RepoName: "project", DefaultBranch: "release/1.2", Visibility: "public"},
			want:  RepoData{Owner: "example", RepoName: "project", DefaultBranch: "release/1.2", Visibility: "public"},
		},
		{
			name:  "plus-sign branch preserved",
			input: RepoData{Owner: "example", RepoName: "project", DefaultBranch: "release+stable", Visibility: "public"},
			want:  RepoData{Owner: "example", RepoName: "project", DefaultBranch: "release+stable", Visibility: "public"},
		},
		{
			name:  "dot-github repo name preserved",
			input: RepoData{Owner: "example", RepoName: ".github", DefaultBranch: "main", Visibility: "public"},
			want:  RepoData{Owner: "example", RepoName: ".github", DefaultBranch: "main", Visibility: "public"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.input.sanitize()
			if (err != nil) != tt.wantErr {
				t.Fatalf("sanitize() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			want := tt.want
			if want.CodeOwnerLine == "" {
				want.CodeOwnerLine = "* @" + want.Owner
			}
			if got != want {
				t.Errorf("sanitize() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestRepoDataSanitize_CodeOwnerLine(t *testing.T) {
	tests := []struct {
		name  string
		input RepoData
		want  string
	}{
		{
			name:  "user line kept",
			input: RepoData{Owner: "alice", RepoName: "r", DefaultBranch: "main", Visibility: "public", CodeOwnerLine: "* @alice"},
			want:  "* @alice",
		},
		{
			name:  "org comment kept",
			input: RepoData{Owner: "acme", RepoName: "r", DefaultBranch: "main", Visibility: "public", CodeOwnerLine: "# Organizations need a team (@org/team), not @acme"},
			want:  "# Organizations need a team (@org/team), not @acme",
		},
		{
			name:  "empty falls back to owner",
			input: RepoData{Owner: "alice", RepoName: "r", DefaultBranch: "main", Visibility: "public"},
			want:  "* @alice",
		},
		{
			name:  "script tags stripped",
			input: RepoData{Owner: "alice", RepoName: "r", DefaultBranch: "main", Visibility: "public", CodeOwnerLine: "* @alice<script>"},
			want:  "* @alicescript",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.input.sanitize()
			if err != nil {
				t.Fatalf("sanitize() unexpected error: %v", err)
			}
			if got.CodeOwnerLine != tt.want {
				t.Errorf("CodeOwnerLine = %q, want %q", got.CodeOwnerLine, tt.want)
			}
		})
	}
}

func TestRender_PreservesValidGitHubNames(t *testing.T) {
	tests := []struct {
		name string
		data RepoData
		want []string
	}{
		{
			name: "dotted release branch",
			data: RepoData{Owner: "example", RepoName: "project", DefaultBranch: "release/1.2", Visibility: "public"},
			want: []string{"release/1.2"},
		},
		{
			name: "plus-sign branch",
			data: RepoData{Owner: "example", RepoName: "project", DefaultBranch: "release+stable", Visibility: "public"},
			want: []string{"release+stable"},
		},
		{
			name: "dot-github repo",
			data: RepoData{Owner: "example", RepoName: ".github", DefaultBranch: "main", Visibility: "public"},
			want: []string{"# Contributing to .github"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := Render(tt.data)
			if err != nil {
				t.Fatalf("Render() unexpected error: %v", err)
			}
			joined := ""
			for _, f := range files {
				joined += f.Content
			}
			for _, needle := range tt.want {
				if !strings.Contains(joined, needle) {
					t.Errorf("rendered output missing %q", needle)
				}
			}
		})
	}
}

func TestRender_RejectsInvalidNames(t *testing.T) {
	tests := []struct {
		name string
		data RepoData
	}{
		{
			name: "script in owner",
			data: RepoData{Owner: "jp<script>alert(1)</script>", RepoName: "repo", DefaultBranch: "main", Visibility: "public"},
		},
		{
			name: "empty owner",
			data: RepoData{Owner: "", RepoName: "repo", DefaultBranch: "main", Visibility: "public"},
		},
		{
			name: "empty repo",
			data: RepoData{Owner: "owner", RepoName: "", DefaultBranch: "main", Visibility: "public"},
		},
		{
			name: "empty branch",
			data: RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "", Visibility: "public"},
		},
		{
			name: "angle brackets in branch",
			data: RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "feat/<test>", Visibility: "public"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Render(tt.data)
			if err == nil {
				t.Fatal("Render() error = nil, want validation error")
			}
		})
	}
}

func TestRenderSanitizesInput(t *testing.T) {
	data := RepoData{
		Owner:         "<script>alert(1)</script>",
		RepoName:      "repo; rm -rf /",
		DefaultBranch: "main; drop table",
		Visibility:    "public",
	}
	if _, err := Render(data); err == nil {
		t.Fatal("Render() error = nil, want validation error for injection payload")
	}
}

func TestRender(t *testing.T) {
	// public visibility so the public-only Codacy config (.codacy.yml) renders.
	data := RepoData{Owner: "jpvelasco", RepoName: "fundamentum", DefaultBranch: "main", Visibility: "public"}
	files, err := Render(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one rendered file")
	}
	found := false
	codacyFound := false
	for _, f := range files {
		if f.Path == ".github/CONTRIBUTING.md" {
			found = true
			if strings.Contains(f.Content, "{{") {
				t.Error(".github/CONTRIBUTING.md still has unrendered template placeholders")
			}
		}
		if f.Path == ".codacy.yml" {
			codacyFound = true
		}
		if strings.HasPrefix(f.Path, "dotgithub/") {
			t.Errorf("path %q still has dotgithub prefix, expected .github/", f.Path)
		}
		if f.Path == "codacy.yml" {
			t.Errorf("path %q is missing its dot prefix, expected .codacy.yml", f.Path)
		}
	}
	if !found {
		t.Error(".github/CONTRIBUTING.md not found in rendered files")
	}
	if !codacyFound {
		t.Error(".codacy.yml not found in rendered files")
	}
}

func TestRender_CodeOwners(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
		deny string
	}{
		{
			name: "user",
			line: "* @jpvelasco",
			want: "* @jpvelasco",
		},
		{
			name: "org comment",
			line: "# Organizations need a team (@org/team), not @acme",
			want: "# Organizations need a team (@org/team), not @acme",
			deny: "* @acme",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := Render(RepoData{
				Owner:         "acme",
				RepoName:      "r",
				DefaultBranch: "main",
				Visibility:    "public",
				CodeOwnerLine: tt.line,
			})
			if err != nil {
				t.Fatalf("Render() error: %v", err)
			}
			var content string
			for _, f := range files {
				if f.Path == ".github/CODEOWNERS" {
					content = f.Content
					break
				}
			}
			if content == "" {
				t.Fatal("missing .github/CODEOWNERS")
			}
			if !strings.Contains(content, tt.want) {
				t.Errorf("CODEOWNERS %q does not contain %q", content, tt.want)
			}
			if tt.deny != "" && strings.Contains(content, tt.deny) {
				t.Errorf("CODEOWNERS %q unexpectedly contains %q", content, tt.deny)
			}
		})
	}
}

func TestRenderVisibilityFiltering(t *testing.T) {
	tests := []struct {
		name       string
		visibility string
		wantFiles  []string
		// excludeFiles must NOT render for this visibility. ci.yml and socket.yml
		// render for BOTH visibilities, so they appear in neither list.
		excludeFiles []string
	}{
		{
			name:       "public repo",
			visibility: "public",
			wantFiles: []string{
				".github/workflows/ci.yml",
				"codecov.yml",
				".github/workflows/octopus.yml",
				".github/workflows/codeql.yml",
				".github/codeql/codeql-config.yml",
				"socket.yml",
				".codacy.yml",
				".github/workflows/codacy-coverage.yml",
				".github/instructions/codacy.instructions.md",
			},
			excludeFiles: []string{".github/workflows/octocov.yml"},
		},
		{
			name:       "private repo",
			visibility: "private",
			wantFiles: []string{
				".github/workflows/ci.yml",
				".github/workflows/octocov.yml",
				"socket.yml",
			},
			// Codacy's free (Open Source) plan is public-only; its three files
			// must not ship to private repos.
			excludeFiles: []string{
				"codecov.yml",
				".github/workflows/octopus.yml",
				".github/workflows/codeql.yml",
				".codacy.yml",
				".github/workflows/codacy-coverage.yml",
				".github/instructions/codacy.instructions.md",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := RepoData{Owner: "o", RepoName: "r", DefaultBranch: "main", Visibility: tt.visibility}
			files, err := Render(data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pathSet := make(map[string]bool)
			for _, f := range files {
				pathSet[f.Path] = true
			}

			for _, want := range tt.wantFiles {
				if !pathSet[want] {
					t.Errorf("missing %q in rendered files", want)
				}
			}
			for _, exclude := range tt.excludeFiles {
				if pathSet[exclude] {
					t.Errorf("%s: %q should not be rendered for %s repos", tt.visibility, exclude, tt.visibility)
				}
			}
		})
	}
}

func TestRenderCIPackFiltering(t *testing.T) {
	tests := []struct {
		pack    string
		want    []string
		exclude []string
	}{
		{
			pack:    CIPackGo,
			want:    []string{".github/workflows/ci.yml", "codecov.yml", ".github/workflows/codeql.yml"},
			exclude: []string{
				// generic_ci.yml also resolves to ci.yml; content must stay Go.
			},
		},
		{
			pack:    CIPackGeneric,
			want:    []string{".github/workflows/ci.yml", "socket.yml", ".github/CODEOWNERS"},
			exclude: []string{"codecov.yml", ".github/workflows/codeql.yml", ".github/workflows/codacy-coverage.yml"},
		},
		{
			pack:    CIPackNone,
			want:    []string{"socket.yml", ".github/CODEOWNERS"},
			exclude: []string{".github/workflows/ci.yml", "codecov.yml", ".github/workflows/codeql.yml"},
		},
		{
			pack:    CIPackNode,
			want:    []string{".github/workflows/ci.yml"},
			exclude: []string{"codecov.yml", ".github/workflows/codeql.yml"},
		},
		{
			pack:    CIPackPython,
			want:    []string{".github/workflows/ci.yml"},
			exclude: []string{"codecov.yml"},
		},
		{
			pack:    CIPackRust,
			want:    []string{".github/workflows/ci.yml"},
			exclude: []string{"codecov.yml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.pack, func(t *testing.T) {
			files, err := Render(RepoData{Owner: "o", RepoName: "r", DefaultBranch: "main", Visibility: "public", CIPack: tt.pack})
			if err != nil {
				t.Fatalf("Render() error: %v", err)
			}
			pathSet := make(map[string]string, len(files))
			for _, f := range files {
				pathSet[f.Path] = f.Content
			}
			for _, want := range tt.want {
				if _, ok := pathSet[want]; !ok {
					t.Errorf("missing %q", want)
				}
			}
			for _, exclude := range tt.exclude {
				if _, ok := pathSet[exclude]; ok {
					t.Errorf("unexpected %q for pack %s", exclude, tt.pack)
				}
			}
			if tt.pack == CIPackGeneric {
				if !strings.Contains(pathSet[".github/workflows/ci.yml"], "Minimal pack for repos that are not Go") {
					t.Error("generic pack must ship generic_ci.yml, not the Go workflow")
				}
				if strings.Contains(pathSet[".github/workflows/ci.yml"], "go-version-file") {
					t.Error("generic CI must not assume go.mod")
				}
			}
			if tt.pack == CIPackGo && !strings.Contains(pathSet[".github/workflows/ci.yml"], "go-version-file") {
				t.Error("go pack must keep go-version-file")
			}
			if tt.pack == CIPackNode && !strings.Contains(pathSet[".github/workflows/ci.yml"], "setup-node") {
				t.Error("node pack must ship setup-node")
			}
			if tt.pack == CIPackPython && !strings.Contains(pathSet[".github/workflows/ci.yml"], "setup-python") {
				t.Error("python pack must ship setup-python")
			}
			if tt.pack == CIPackRust && !strings.Contains(pathSet[".github/workflows/ci.yml"], "rust-toolchain") {
				t.Error("rust pack must ship rust-toolchain")
			}
		})
	}
}

func TestSubstitute(t *testing.T) {
	tests := []struct {
		name string
		in   string
		data RepoData
		want string
	}{
		{
			name: "replace all four fields",
			in:   "{{.Owner}}/{{.RepoName}} on {{.DefaultBranch}} ({{.Visibility}})",
			data: RepoData{Owner: "jpvelasco", RepoName: "fundamentum", DefaultBranch: "main", Visibility: "public"},
			want: "jpvelasco/fundamentum on main (public)",
		},
		{
			name: "replace code owner line",
			in:   "{{.CodeOwnerLine}}",
			data: RepoData{CodeOwnerLine: "# Organizations need a team (@org/team), not @acme"},
			want: "# Organizations need a team (@org/team), not @acme",
		},
		{
			name: "unknown placeholders preserved",
			in:   "{{.Owner}}/{{.Unknown}}",
			data: RepoData{Owner: "alice"},
			want: "alice/{{.Unknown}}",
		},
		{
			name: "no placeholders",
			in:   "hello world",
			data: RepoData{},
			want: "hello world",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := substitute(tt.in, tt.data)
			if got != tt.want {
				t.Errorf("substitute(%q, %+v) = %q, want %q", tt.in, tt.data, got, tt.want)
			}
		})
	}
}

func TestRender_SanitizesXSSPayloads(t *testing.T) {
	data := RepoData{
		Owner:         "<img onerror=alert(1) src=x>",
		RepoName:      "\x22><script>alert('xss')</script>",
		DefaultBranch: "main{{.Owner}}",
		Visibility:    "public",
	}
	if _, err := Render(data); err == nil {
		t.Fatal("Render() error = nil, want validation error for XSS payload")
	}
}

func TestSanitizeOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no html", "hello world", "hello world"},
		{"script tag", "hello<script>alert(1)</script>world", "helloalert(1)world"},
		{"img tag", "text<img src=x onerror=alert(1)>more", "textmore"},
		{"your-credentials preserved", "git@github.com:<your-username>/repo.git", "git@github.com:<your-username>/repo.git"},
		{"div tag", "<div class='evil'>content</div>", "content"},
		{"onerror attribute", "<input onfocus=steal()>", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeOutput(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeOutput(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// errFS wraps fstest.MapFS with a ReadFile implementation that always fails.
// WalkDir and Stat still succeed (the embedded MapFS handles them), so
// renderFromFS' ReadFile failure branch is exercised.
type errFS struct {
	fstest.MapFS
}

func (errFS) ReadFile(string) ([]byte, error) { return nil, fs.ErrPermission }

func TestRenderFromFS_ReadError(t *testing.T) {
	m := errFS{MapFS: fstest.MapFS{
		"template.yml": &fstest.MapFile{Data: []byte("hello")},
	}}
	_, err := renderFromFS(m, RepoData{Owner: "o", RepoName: "r", DefaultBranch: "main", Visibility: "public"})
	if err == nil {
		t.Fatal("expected error when template read fails")
	}
	if !strings.Contains(err.Error(), "read template") {
		t.Errorf("expected 'read template' in error, got: %v", err)
	}
}
