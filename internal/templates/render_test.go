package templates

import (
	"strings"
	"testing"
)

func TestValidIdentifier(t *testing.T) {
	tests := []struct {
		name string
		r    rune
		want rune
	}{
		{"lowercase", 'a', 'a'},
		{"uppercase", 'Z', 'Z'},
		{"digit", '5', '5'},
		{"hyphen", '-', '-'},
		{"dot", '.', '.'},
		{"space stripped", ' ', -1},
		{"slash stripped", '/', -1},
		{"underscore preserved", '_', '_'},
		{"newline stripped", '\n', -1},
		{"null stripped", 0, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validIdentifier(tt.r)
			if got != tt.want {
				t.Errorf("validIdentifier(%q) = %v, want %v", tt.r, got, tt.want)
			}
		})
	}
}

func TestRepoDataSanitize(t *testing.T) {
	tests := []struct {
		name   string
		input  RepoData
		want   RepoData
	}{
		{
			name:   "valid input unchanged",
			input:  RepoData{Owner: "jpvelasco", RepoName: "fundamentum", DefaultBranch: "main", Visibility: "private"},
			want:   RepoData{Owner: "jpvelasco", RepoName: "fundamentum", DefaultBranch: "main", Visibility: "private"},
		},
		{
			name:   "owner with special chars stripped",
			input:  RepoData{Owner: "jp<script>alert(1)</script>", RepoName: "repo", DefaultBranch: "main", Visibility: "public"},
			want:   RepoData{Owner: "jpscriptalert1script", RepoName: "repo", DefaultBranch: "main", Visibility: "public"},
		},
		{
			name:   "empty owner falls back",
			input:  RepoData{Owner: "", RepoName: "repo", DefaultBranch: "main", Visibility: "public"},
			want:   RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public"},
		},
		{
			name:   "empty repo falls back",
			input:  RepoData{Owner: "owner", RepoName: "", DefaultBranch: "main", Visibility: "public"},
			want:   RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public"},
		},
		{
			name:   "empty branch falls back",
			input:  RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "", Visibility: "public"},
			want:   RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public"},
		},
		{
			name:   "invalid visibility falls back",
			input:  RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "secret"},
			want:   RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public"},
		},
		{
			name:   "visibility case normalized",
			input:  RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "PRIVATE"},
			want:   RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"},
		},
		{
			name:   "branch with slash preserved",
			input:  RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "feature/my-branch_1", Visibility: "public"},
			want:   RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "feature/my-branch_1", Visibility: "public"},
		},
		{
			name:   "branch with special chars stripped",
			input:  RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "feat/<test>", Visibility: "public"},
			want:   RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "feat/test", Visibility: "public"},
		},
		{
			name:   "repo with dots preserved",
			input:  RepoData{Owner: "owner", RepoName: "my.repo.name", DefaultBranch: "main", Visibility: "public"},
			want:   RepoData{Owner: "owner", RepoName: "my.repo.name", DefaultBranch: "main", Visibility: "public"},
		},
		{
			name:   "repo with underscores preserved",
			input:  RepoData{Owner: "owner", RepoName: "my_repo_name", DefaultBranch: "main", Visibility: "public"},
			want:   RepoData{Owner: "owner", RepoName: "my_repo_name", DefaultBranch: "main", Visibility: "public"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.input.sanitize()
			if got != tt.want {
				t.Errorf("sanitize() = %+v, want %+v", got, tt.want)
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
	files, err := Render(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.Content, "<script>") {
			t.Errorf("unescaped <script> tag found in %s", f.Path)
		}
	}
}

func TestRender(t *testing.T) {
	data := RepoData{Owner: "jpvelasco", RepoName: "fundamentum", DefaultBranch: "main", Visibility: "private"}
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
		if strings.HasPrefix(f.Path, "dotcodacy") {
			t.Errorf("path %q still has dotcodacy prefix, expected .codacy.yml", f.Path)
		}
	}
	if !found {
		t.Error(".github/CONTRIBUTING.md not found in rendered files")
	}
	if !codacyFound {
		t.Error(".codacy.yml not found in rendered files")
	}
}

func TestRenderVisibilityFiltering(t *testing.T) {
	tests := []struct {
		name            string
		visibility      string
		wantPublicFiles []string
		wantPrivateFiles []string
	}{
		{
			name:       "public repo",
			visibility: "public",
			wantPublicFiles: []string{
				".github/workflows/ci.yml",
				"codecov.yml",
				".github/workflows/octopus.yml",
				".github/workflows/codeql.yml",
				".github/codeql/codeql-config.yml",
				"socket.yml",
			},
		},
		{
			name:       "private repo",
			visibility: "private",
			wantPrivateFiles: []string{
				".github/workflows/ci.yml",
				".github/workflows/octocov.yml",
				"socket.yml",
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

			for _, want := range tt.wantPublicFiles {
				if !pathSet[want] {
					t.Errorf("missing %q in rendered files", want)
				}
			}
			for _, want := range tt.wantPrivateFiles {
				if !pathSet[want] {
					t.Errorf("missing %q in rendered files", want)
				}
			}

			// Verify opposite visibility files are excluded.
			// Note: ci.yml and socket.yml render for BOTH visibilities (from
			// public_ci.yml / private_ci.yml and the shared socket.yml), so they
			// must not appear in either exclude list.
			var excludeFiles []string
			if tt.visibility == "public" {
				excludeFiles = []string{".github/workflows/octocov.yml"}
			} else {
				excludeFiles = []string{
					"codecov.yml",
					".github/workflows/octopus.yml",
					".github/workflows/codeql.yml",
				}
			}
			for _, exclude := range excludeFiles {
				if pathSet[exclude] {
					t.Errorf("%s: %q should not be rendered for %s repos", tt.visibility, exclude, tt.visibility)
				}
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
			name:   "replace all four fields",
			in:     "{{.Owner}}/{{.RepoName}} on {{.DefaultBranch}} ({{.Visibility}})",
			data:   RepoData{Owner: "jpvelasco", RepoName: "fundamentum", DefaultBranch: "main", Visibility: "public"},
			want:   "jpvelasco/fundamentum on main (public)",
		},
		{
			name:   "unknown placeholders preserved",
			in:     "{{.Owner}}/{{.Unknown}}",
			data:   RepoData{Owner: "alice"},
			want:   "alice/{{.Unknown}}",
		},
		{
			name:   "no placeholders",
			in:     "hello world",
			data:   RepoData{},
			want:   "hello world",
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
	// Verify that raw HTML tags and script content are stripped from rendered output.
	data := RepoData{
		Owner:         "<img onerror=alert(1) src=x>",
		RepoName:      "\x22><script>alert('xss')</script>",
		DefaultBranch: "main{{.Owner}}", // template injection attempt
		Visibility:    "public",
	}
	files, err := Render(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check for raw HTML tags that should not survive sanitization.
	htmlTags := []string{
		"<img", "<script>", "<script", "</script>",
		"onerror=", "alert(", "javascript:",
	}
	for _, f := range files {
		for _, tag := range htmlTags {
			if strings.Contains(f.Content, tag) {
				t.Errorf("HTML tag %q found in %s", tag, f.Path)
			}
		}
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
		{"your-username preserved", "git@github.com:<your-username>/repo.git", "git@github.com:<your-username>/repo.git"},
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
