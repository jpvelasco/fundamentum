package templatefs

import (
	"fmt"
	"regexp"
	"strings"
)

// CodecovFunctional is the subset of Codecov upload settings that must stay in
// sync between the live workflow (.github/workflows/ci.yml) and the embed
// template (templates/dotgithub/workflows/public_ci.yml). Codecov is folded
// into the Test job of ci.yml (fabrica standard), so both live and template are
// the CI workflow.
//
// Action SHAs for checkout/setup-go/codecov and branch names are intentionally
// excluded — those may differ between this repo and what we ship to new repos.
type CodecovFunctional struct {
	IDTokenWrite      bool
	UseOIDC           bool   // use_oidc: true OR use_oidc: ${{ ... }} (XOR-with-token expr)
	UsePyPI           bool   // use_pypi: true
	FailCIIfError     bool   // >=1 fail_ci_if_error: true (coverage upload leg)
	CoverageFiles     string // value of the first `files:` (coverage upload)
	Coverprofile      string // value of `-coverprofile=`
	CovermodeAtomic   bool   // -covermode=atomic
	HasOverrideCommit bool   // override_commit: set (base report on push)
	HasOverrideBranch bool   // override_branch: set
	HasOverridePR     bool   // override_pr: set
	HasSlug           bool   // slug: set
	HasTestResults    bool   // report_type: test_results (Test Analytics upload)
	CodecovSHAPin     bool   // codecov-action@<40-char hex>
}

var (
	reIDTokenWrite = regexp.MustCompile(`(?m)^\s*id-token:\s*write\s*$`)
	// use_oidc must be either literal true or the exact XOR-auth expression
	// ${{ secrets.CODECOV_TOKEN == '' }}. A bare ${{ ... }} blob is NOT accepted:
	// expressions like ${{ false }} or the inverted token check would silently
	// disable OIDC while still passing the drift gate.
	reUseOIDC = regexp.MustCompile(`(?m)^\s*use_oidc:\s*(true|\$\{\{\s*secrets\.CODECOV_TOKEN\s*==\s*''\s*\}\})\s*$`)
	reUsePyPI         = regexp.MustCompile(`(?m)^\s*use_pypi:\s*true\s*$`)
	reFailCIIfError   = regexp.MustCompile(`(?m)^\s*fail_ci_if_error:\s*true\s*$`)
	// Pin to the coverage filename specifically so a two-upload workflow
	// (coverage + test_results) can't have CoverageFiles capture ./junit.xml
	// if the upload steps are ever reordered.
	reCoverageFiles   = regexp.MustCompile(`(?m)^\s*files:\s*(\./coverage\.out|coverage\.out|coverage)\s*$`)
	reCoverprofile    = regexp.MustCompile(`-coverprofile=(\S+)`)
	reCovermodeAtomic = regexp.MustCompile(`-covermode=atomic`)
	reOverrideCommit  = regexp.MustCompile(`(?m)^\s*override_commit:\s*\S`)
	reOverrideBranch  = regexp.MustCompile(`(?m)^\s*override_branch:\s*\S`)
	reOverridePR      = regexp.MustCompile(`(?m)^\s*override_pr:\s*\S`)
	reSlug            = regexp.MustCompile(`(?m)^\s*slug:\s*\S`)
	reTestResults     = regexp.MustCompile(`(?m)^\s*report_type:\s*test_results\s*$`)
	reCodecovSHAPin   = regexp.MustCompile(`codecov/codecov-action@[0-9a-fA-F]{40}`)
)

// ParseCodecovFunctional extracts comparable functional settings from a CI
// workflow YAML body. Parsing is line/regex based (stdlib only).
func ParseCodecovFunctional(content string) CodecovFunctional {
	var f CodecovFunctional
	f.IDTokenWrite = reIDTokenWrite.MatchString(content)
	f.UseOIDC = reUseOIDC.MatchString(content)
	f.UsePyPI = reUsePyPI.MatchString(content)
	f.FailCIIfError = reFailCIIfError.MatchString(content)
	if m := reCoverageFiles.FindStringSubmatch(content); len(m) == 2 {
		f.CoverageFiles = m[1]
	}
	if m := reCoverprofile.FindStringSubmatch(content); len(m) == 2 {
		f.Coverprofile = m[1]
	}
	f.CovermodeAtomic = reCovermodeAtomic.MatchString(content)
	f.HasOverrideCommit = reOverrideCommit.MatchString(content)
	f.HasOverrideBranch = reOverrideBranch.MatchString(content)
	f.HasOverridePR = reOverridePR.MatchString(content)
	f.HasSlug = reSlug.MatchString(content)
	f.HasTestResults = reTestResults.MatchString(content)
	f.CodecovSHAPin = reCodecovSHAPin.MatchString(content)
	return f
}

// DiffCodecovFunctional reports functional drift between live and template.
// Empty slice means functional parity (plus both SHA-pinned).
func DiffCodecovFunctional(live, template CodecovFunctional) []string {
	var diffs []string
	add := func(field string, liveVal, tplVal any) {
		diffs = append(diffs, fmt.Sprintf("%s: live=%v template=%v", field, liveVal, tplVal))
	}
	if live.IDTokenWrite != template.IDTokenWrite {
		add("id-token: write", live.IDTokenWrite, template.IDTokenWrite)
	}
	if live.UseOIDC != template.UseOIDC {
		add("use_oidc", live.UseOIDC, template.UseOIDC)
	}
	if live.UsePyPI != template.UsePyPI {
		add("use_pypi", live.UsePyPI, template.UsePyPI)
	}
	if live.FailCIIfError != template.FailCIIfError {
		add("fail_ci_if_error", live.FailCIIfError, template.FailCIIfError)
	}
	if live.CoverageFiles != template.CoverageFiles {
		add("files", live.CoverageFiles, template.CoverageFiles)
	}
	if live.Coverprofile != template.Coverprofile {
		add("coverprofile", live.Coverprofile, template.Coverprofile)
	}
	if live.CovermodeAtomic != template.CovermodeAtomic {
		add("covermode=atomic", live.CovermodeAtomic, template.CovermodeAtomic)
	}
	if live.HasOverrideCommit != template.HasOverrideCommit {
		add("override_commit", live.HasOverrideCommit, template.HasOverrideCommit)
	}
	if live.HasOverrideBranch != template.HasOverrideBranch {
		add("override_branch", live.HasOverrideBranch, template.HasOverrideBranch)
	}
	if live.HasOverridePR != template.HasOverridePR {
		add("override_pr", live.HasOverridePR, template.HasOverridePR)
	}
	if live.HasSlug != template.HasSlug {
		add("slug", live.HasSlug, template.HasSlug)
	}
	if live.HasTestResults != template.HasTestResults {
		add("report_type: test_results", live.HasTestResults, template.HasTestResults)
	}
	// SHA pins need not be identical, but both sides must pin by commit SHA.
	if !live.CodecovSHAPin {
		diffs = append(diffs, "live codecov-action is not SHA-pinned (40-char commit hash required; floating tags like @v7 are not allowed)")
	}
	if !template.CodecovSHAPin {
		diffs = append(diffs, "template codecov-action is not SHA-pinned (40-char commit hash required; floating tags like @v7 are not allowed)")
	}
	return diffs
}

// FormatCodecovDrift builds a multi-line error message for CI/pre-commit output.
func FormatCodecovDrift(diffs []string) string {
	var b strings.Builder
	b.WriteString("codecov template drift: live workflow and embed template functional settings differ\n")
	b.WriteString("  live:     .github/workflows/ci.yml\n")
	b.WriteString("  template: internal/templatefs/templates/dotgithub/workflows/public_ci.yml\n")
	b.WriteString("  (action SHAs for checkout/setup-go/codecov and branch names may differ intentionally)\n")
	for _, d := range diffs {
		b.WriteString("  - ")
		b.WriteString(d)
		b.WriteByte('\n')
	}
	b.WriteString("Sync Codecov upload settings (OIDC, use_pypi, fail_ci_if_error, override_*, slug, test_results, coverage flags, SHA pin) before merging.")
	return b.String()
}
