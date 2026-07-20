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
				".github/workflows/codecov.yml",
				".github/workflows/octopus.yml",
				".github/workflows/codeql.yml",
				".github/codeql/codeql-config.yml",
			},
		},
		{
			name:       "private repo",
			visibility: "private",
			wantPrivateFiles: []string{
				".github/workflows/octocov.yml",
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
			var excludeFiles []string
			if tt.visibility == "public" {
				excludeFiles = []string{".github/workflows/octocov.yml"}
			} else {
				excludeFiles = []string{
					".github/workflows/codecov.yml",
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

func TestSafeFuncMap_NoUnsafeFunctions(t *testing.T) {
	// Verify the safe func map is empty — our templates only need
	// field access on RepoData, which is provided by default.
	// If new functions are added, ensure they do not accept user input
	// that could control execution flow.
	fm := safeFuncMap()
	if len(fm) > 0 {
		t.Errorf("safe func map should be empty, got %d functions", len(fm))
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
