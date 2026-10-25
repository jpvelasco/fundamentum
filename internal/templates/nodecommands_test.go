package templates

import "testing"

func TestNodeCommands(t *testing.T) {
	tests := []struct {
		name        string
		packageJSON string
		wantLint    string
		wantTest    string
	}{
		{"empty body falls back to failing commands", "", defaultNodeLintCommand, defaultNodeTestCommand},
		{"malformed json falls back", "{not json", defaultNodeLintCommand, defaultNodeTestCommand},
		{"no scripts falls back", `{"name":"x"}`, defaultNodeLintCommand, defaultNodeTestCommand},
		{"lint script detected", `{"scripts":{"lint":"eslint ."}}`, "npm run lint", defaultNodeTestCommand},
		{"test script detected", `{"scripts":{"test":"jest"}}`, defaultNodeLintCommand, "npm test"},
		{"both scripts detected", `{"scripts":{"lint":"eslint .","test":"jest"}}`, "npm run lint", "npm test"},
	}
	for _, tt := range tests {
		gotLint, gotTest := NodeCommands([]byte(tt.packageJSON))
		if gotLint != tt.wantLint || gotTest != tt.wantTest {
			t.Errorf("NodeCommands(%q) = (%q, %q), want (%q, %q)", tt.packageJSON, gotLint, gotTest, tt.wantLint, tt.wantTest)
		}
	}
}

func TestDetectDocData(t *testing.T) {
	exists := func(path string) (bool, error) {
		switch path {
		case "README.rst", "AGENTS.md":
			return true, nil
		case "package.json":
			return true, nil
		default:
			return false, nil
		}
	}
	read := func() ([]byte, bool, error) {
		return []byte(`{"scripts":{"lint":"eslint .","test":"jest"}}`), true, nil
	}
	readme, guide, lint, test, err := DetectDocData(exists, read, CIPackNode)
	if err != nil {
		t.Fatalf("DetectDocData() error: %v", err)
	}
	if readme != "README.rst" {
		t.Errorf("readme = %q, want README.rst", readme)
	}
	if guide != agentGuideLine("AGENTS.md") {
		t.Errorf("guide = %q, want AGENTS.md line", guide)
	}
	if lint != "npm run lint" || test != "npm test" {
		t.Errorf("commands = (%q, %q), want npm run lint / npm test", lint, test)
	}

	// Non-node pack: no package.json read.
	readCalled := false
	_, _, _, _, err = DetectDocData(exists, func() ([]byte, bool, error) {
		readCalled = true
		return nil, false, nil
	}, CIPackGo)
	if err != nil {
		t.Fatalf("DetectDocData(go) error: %v", err)
	}
	if readCalled {
		t.Error("package.json must not be read for non-node packs")
	}

	// Legacy CLAUDE.md repo.
	claudeExists := func(path string) (bool, error) { return path == "CLAUDE.md", nil }
	_, guide, _, _, err = DetectDocData(claudeExists, read, CIPackGo)
	if err != nil {
		t.Fatalf("DetectDocData(claude) error: %v", err)
	}
	if guide != agentGuideLine("CLAUDE.md") {
		t.Errorf("guide = %q, want CLAUDE.md legacy line", guide)
	}

	// Neither exists: standard default.
	noneExists := func(string) (bool, error) { return false, nil }
	_, guide, _, _, err = DetectDocData(noneExists, read, CIPackGo)
	if err != nil {
		t.Fatalf("DetectDocData(none) error: %v", err)
	}
	if guide != agentGuideLine("AGENTS.md") {
		t.Errorf("guide = %q, want AGENTS.md default line", guide)
	}

	// Errors propagate.
	_, _, _, _, err = DetectDocData(func(string) (bool, error) { return false, errProbe }, read, CIPackGo)
	if err != errProbe {
		t.Errorf("exists error must propagate: %v", err)
	}
}

var errProbe = &probeError{}

type probeError struct{}

func (*probeError) Error() string { return "probe" }
