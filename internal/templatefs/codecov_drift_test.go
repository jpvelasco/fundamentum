package templatefs

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// fullParity is a CodecovFunctional with every flag at the fabrica standard.
// Reused across tests so a field added to the struct forces an update here.
var fullParity = CodecovFunctional{
	IDTokenWrite:      true,
	UseOIDC:           true,
	UsePyPI:           true,
	FailCIIfError:     true,
	CoverageFiles:     "./coverage.out",
	Coverprofile:      "coverage.out",
	CovermodeAtomic:   true,
	HasOverrideCommit: true,
	HasOverrideBranch: true,
	HasOverridePR:     true,
	HasSlug:           true,
	HasTestResults:    true,
	CodecovSHAPin:     true,
}

func TestParseCodecovFunctional_LiveShape(t *testing.T) {
	// Mirrors the fabrica-standard Test job: XOR-auth use_oidc expression,
	// override_*, slug, a coverage upload and a test_results upload.
	const live = `
permissions:
  contents: read
  id-token: write
steps:
  - run: |
      gotestsum --junitfile junit.xml --format pkgname -- \
        -race -coverprofile=coverage.out -covermode=atomic ./...
  - uses: codecov/codecov-action@fb8b3582c8e4def4969c97caa2f19720cb33a72f  # v7.0.0
    with:
      use_oidc: ${{ secrets.CODECOV_TOKEN == '' }}
      token: ${{ secrets.CODECOV_TOKEN }}
      use_pypi: true
      files: ./coverage.out
      slug: owner/repo
      override_commit: ${{ github.event.pull_request.head.sha || github.sha }}
      override_branch: ${{ github.head_ref || github.ref_name }}
      override_pr: ${{ github.event.pull_request.number }}
      fail_ci_if_error: true
  - uses: codecov/codecov-action@fb8b3582c8e4def4969c97caa2f19720cb33a72f  # v7.0.0
    with:
      use_oidc: ${{ secrets.CODECOV_TOKEN == '' }}
      token: ${{ secrets.CODECOV_TOKEN }}
      use_pypi: true
      report_type: test_results
      files: ./junit.xml
      slug: owner/repo
      fail_ci_if_error: false
`
	got := ParseCodecovFunctional(live)
	if got != fullParity {
		t.Fatalf("ParseCodecovFunctional:\n got %+v\nwant %+v", got, fullParity)
	}
}

func TestParseCodecovFunctional_BrokenTemplate(t *testing.T) {
	// A regressed workflow: no OIDC/pypi, floating tag, no override_*, no slug,
	// no test_results upload.
	const broken = `
permissions:
  contents: read
steps:
  - run: go test -coverprofile=coverage.out ./...
  - uses: codecov/codecov-action@v7
    with:
      files: coverage.out
`
	got := ParseCodecovFunctional(broken)
	if got.IDTokenWrite || got.UseOIDC || got.UsePyPI || got.FailCIIfError {
		t.Fatalf("expected missing OIDC/pypi/fail flags, got %+v", got)
	}
	if got.CoverageFiles != "coverage.out" || got.Coverprofile != "coverage.out" {
		t.Fatalf("expected coverage.out, got files=%q coverprofile=%q", got.CoverageFiles, got.Coverprofile)
	}
	if got.CovermodeAtomic {
		t.Fatalf("expected missing covermode, got %+v", got)
	}
	if got.HasOverrideCommit || got.HasOverrideBranch || got.HasOverridePR {
		t.Fatalf("expected missing override_*, got %+v", got)
	}
	if got.HasSlug {
		t.Fatal("expected missing slug")
	}
	if got.HasTestResults {
		t.Fatal("expected missing report_type: test_results")
	}
	if got.CodecovSHAPin {
		t.Fatal("floating @v7 must not count as SHA-pinned")
	}
}

func TestParseCodecovFunctional_OIDCExpression(t *testing.T) {
	// The XOR-auth expression form must count as OIDC enabled (not just literal true).
	const expr = `
    with:
      use_oidc: ${{ secrets.CODECOV_TOKEN == '' }}
`
	if !ParseCodecovFunctional(expr).UseOIDC {
		t.Fatal("use_oidc expression form should count as enabled")
	}
	const literal = `
    with:
      use_oidc: true
`
	if !ParseCodecovFunctional(literal).UseOIDC {
		t.Fatal("use_oidc: true should count as enabled")
	}
	// Only literal true or the exact XOR expression count — an arbitrary
	// expression must NOT satisfy the gate (would silently disable OIDC).
	for _, bad := range []string{
		"\n    with:\n      use_oidc: ${{ false }}\n",
		"\n    with:\n      use_oidc: ${{ secrets.CODECOV_TOKEN != '' }}\n",
		"\n    with:\n      use_oidc: false\n",
	} {
		if ParseCodecovFunctional(bad).UseOIDC {
			t.Errorf("unsupported use_oidc form should not count as enabled: %q", bad)
		}
	}
}

func TestDiffCodecovFunctional_Parity(t *testing.T) {
	a := fullParity
	b := a
	if diffs := DiffCodecovFunctional(a, b); len(diffs) != 0 {
		t.Fatalf("expected no diffs, got %v", diffs)
	}
}

func TestDiffCodecovFunctional_ReportsEachField(t *testing.T) {
	live := fullParity
	tpl := CodecovFunctional{
		CoverageFiles: "coverage.out", Coverprofile: "coverage.out",
	}
	diffs := DiffCodecovFunctional(live, tpl)
	// Sanity floor: the fully-mismatched template should surface at least the
	// distinct fields listed below (guards against Diff silently collapsing).
	if len(diffs) < 12 {
		t.Fatalf("expected >=12 drifts, got %d: %v", len(diffs), diffs)
	}
	joined := strings.Join(diffs, "\n")
	for _, need := range []string{
		"id-token: write",
		"use_oidc",
		"use_pypi",
		"fail_ci_if_error",
		"files",
		"covermode=atomic",
		"override_commit",
		"override_branch",
		"override_pr",
		"slug",
		"report_type: test_results",
		"template codecov-action is not SHA-pinned",
	} {
		if !strings.Contains(joined, need) {
			t.Errorf("diff missing %q in:\n%s", need, joined)
		}
	}
}

func TestFormatCodecovDrift(t *testing.T) {
	msg := FormatCodecovDrift([]string{"use_oidc: live=true template=false"})
	if !strings.Contains(msg, "codecov template drift") {
		t.Fatalf("missing header: %s", msg)
	}
	if !strings.Contains(msg, "use_oidc") {
		t.Fatalf("missing detail: %s", msg)
	}
	if !strings.Contains(msg, "ci.yml") {
		t.Fatalf("expected ci.yml paths in message: %s", msg)
	}
}

// liveCIWorkflow is a fixed relative path from this package directory (go test
// sets cwd to the package dir). Kept constant so path scanners do not flag
// dynamic path construction from inputs.
const liveCIWorkflow = "../../.github/workflows/ci.yml"

// TestCodecovTemplateDrift is the gate used by pre-commit and CI.
// The live CI workflow is source of truth for Codecov upload settings; the embed
// template that fundamentum ships (public_ci.yml) must match. Action SHAs may differ.
func TestCodecovTemplateDrift(t *testing.T) {
	liveBytes, err := os.ReadFile(liveCIWorkflow)
	if err != nil {
		t.Fatalf("read live workflow %s: %v", liveCIWorkflow, err)
	}

	tplBytes, err := fs.ReadFile(FS, "dotgithub/workflows/public_ci.yml")
	if err != nil {
		t.Fatalf("read embed template: %v", err)
	}

	live := ParseCodecovFunctional(string(liveBytes))
	tpl := ParseCodecovFunctional(string(tplBytes))
	diffs := DiffCodecovFunctional(live, tpl)
	if len(diffs) > 0 {
		t.Fatal(FormatCodecovDrift(diffs))
	}
}

func TestDiffCodecovFunctional_UnpinnedBothSides(t *testing.T) {
	live := fullParity
	live.CodecovSHAPin = false
	tpl := live
	diffs := DiffCodecovFunctional(live, tpl)
	joined := strings.Join(diffs, "\n")
	if !strings.Contains(joined, "live codecov-action is not SHA-pinned") {
		t.Fatalf("expected live pin error, got %v", diffs)
	}
	if !strings.Contains(joined, "template codecov-action is not SHA-pinned") {
		t.Fatalf("expected template pin error, got %v", diffs)
	}
}
