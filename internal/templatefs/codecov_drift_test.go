package templatefs

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestParseCodecovFunctional_LiveShape(t *testing.T) {
	const live = `
permissions:
  contents: read
  id-token: write
steps:
  - run: go test ./... -coverprofile=coverage -coverpkg=./... -covermode=atomic
  - uses: codecov/codecov-action@04b047e8bb82a0c002c8312c1c880fbc6a999d45  # v5
    with:
      files: coverage
      fail_ci_if_error: true
      use_oidc: true
      use_pypi: true
`
	got := ParseCodecovFunctional(live)
	want := CodecovFunctional{
		IDTokenWrite:    true,
		UseOIDC:         true,
		UsePyPI:         true,
		FailCIIfError:   true,
		CoverageFiles:   "coverage",
		Coverprofile:    "coverage",
		HasCoverpkgAll:  true,
		CovermodeAtomic: true,
		CodecovSHAPin:   true,
	}
	if got != want {
		t.Fatalf("ParseCodecovFunctional:\n got %+v\nwant %+v", got, want)
	}
}

func TestParseCodecovFunctional_BrokenTemplate(t *testing.T) {
	const broken = `
permissions:
  contents: read
steps:
  - run: go test -coverprofile=coverage.out ./...
  - uses: codecov/codecov-action@v5
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
	if got.HasCoverpkgAll || got.CovermodeAtomic {
		t.Fatalf("expected missing coverpkg/covermode, got %+v", got)
	}
	if got.CodecovSHAPin {
		t.Fatal("floating @v5 must not count as SHA-pinned")
	}
}

func TestDiffCodecovFunctional_Parity(t *testing.T) {
	a := CodecovFunctional{
		IDTokenWrite: true, UseOIDC: true, UsePyPI: true, FailCIIfError: true,
		CoverageFiles: "coverage", Coverprofile: "coverage",
		HasCoverpkgAll: true, CovermodeAtomic: true, CodecovSHAPin: true,
	}
	b := a
	if diffs := DiffCodecovFunctional(a, b); len(diffs) != 0 {
		t.Fatalf("expected no diffs, got %v", diffs)
	}
}

func TestDiffCodecovFunctional_ReportsEachField(t *testing.T) {
	live := CodecovFunctional{
		IDTokenWrite: true, UseOIDC: true, UsePyPI: true, FailCIIfError: true,
		CoverageFiles: "coverage", Coverprofile: "coverage",
		HasCoverpkgAll: true, CovermodeAtomic: true, CodecovSHAPin: true,
	}
	tpl := CodecovFunctional{
		CoverageFiles: "coverage.out", Coverprofile: "coverage.out",
	}
	diffs := DiffCodecovFunctional(live, tpl)
	if len(diffs) < 7 {
		t.Fatalf("expected multiple drifts, got %d: %v", len(diffs), diffs)
	}
	joined := strings.Join(diffs, "\n")
	for _, need := range []string{
		"id-token: write",
		"use_oidc",
		"use_pypi",
		"fail_ci_if_error",
		"files",
		"coverprofile",
		"coverpkg=./...",
		"covermode=atomic",
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
}

// liveCodecovWorkflow is a fixed relative path from this package directory
// (go test sets cwd to the package dir). Kept constant so path scanners do not
// flag dynamic path construction from inputs.
const liveCodecovWorkflow = "../../.github/workflows/codecov.yml"

// TestCodecovTemplateDrift is the gate used by pre-commit and CI.
// Live workflow is source of truth for functional settings; the embed template
// that fundamentum init ships must match. Action SHAs may differ.
func TestCodecovTemplateDrift(t *testing.T) {
	liveBytes, err := os.ReadFile(liveCodecovWorkflow)
	if err != nil {
		t.Fatalf("read live workflow %s: %v", liveCodecovWorkflow, err)
	}

	tplBytes, err := fs.ReadFile(FS, "dotgithub/workflows/public_codecov.yml")
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
	live := CodecovFunctional{
		IDTokenWrite: true, UseOIDC: true, UsePyPI: true, FailCIIfError: true,
		CoverageFiles: "coverage", Coverprofile: "coverage",
		HasCoverpkgAll: true, CovermodeAtomic: true, CodecovSHAPin: false,
	}
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
