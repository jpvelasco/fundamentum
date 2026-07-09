package templatefs

import (
	"fmt"
	"regexp"
	"strings"
)

// CodecovFunctional is the subset of Codecov workflow settings that must stay in
// sync between the live workflow (.github/workflows/codecov.yml) and the embed
// template (templates/dotgithub/workflows/public_codecov.yml).
//
// Action SHAs for checkout/setup-go/codecov and branch names are intentionally
// excluded — those may differ between this repo and what we ship to new repos.
type CodecovFunctional struct {
	IDTokenWrite    bool
	UseOIDC         bool
	UsePyPI         bool
	FailCIIfError   bool
	CoverageFiles   string // value of `files:`
	Coverprofile    string // value of `-coverprofile=`
	HasCoverpkgAll  bool   // -coverpkg=./...
	CovermodeAtomic bool   // -covermode=atomic
	CodecovSHAPin   bool   // codecov-action@<40-char hex>
}

var (
	reIDTokenWrite    = regexp.MustCompile(`(?m)^\s*id-token:\s*write\s*$`)
	reUseOIDC         = regexp.MustCompile(`(?m)^\s*use_oidc:\s*true\s*$`)
	reUsePyPI         = regexp.MustCompile(`(?m)^\s*use_pypi:\s*true\s*$`)
	reFailCIIfError   = regexp.MustCompile(`(?m)^\s*fail_ci_if_error:\s*true\s*$`)
	reCoverageFiles   = regexp.MustCompile(`(?m)^\s*files:\s*(\S+)\s*$`)
	reCoverprofile    = regexp.MustCompile(`-coverprofile=(\S+)`)
	reCoverpkgAll     = regexp.MustCompile(`-coverpkg=\./\.\.\.`)
	reCovermodeAtomic = regexp.MustCompile(`-covermode=atomic`)
	reCodecovSHAPin   = regexp.MustCompile(`codecov/codecov-action@[0-9a-fA-F]{40}`)
)

// ParseCodecovFunctional extracts comparable functional settings from a
// Codecov workflow YAML body. Parsing is line/regex based (stdlib only).
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
	f.HasCoverpkgAll = reCoverpkgAll.MatchString(content)
	f.CovermodeAtomic = reCovermodeAtomic.MatchString(content)
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
	if live.HasCoverpkgAll != template.HasCoverpkgAll {
		add("coverpkg=./...", live.HasCoverpkgAll, template.HasCoverpkgAll)
	}
	if live.CovermodeAtomic != template.CovermodeAtomic {
		add("covermode=atomic", live.CovermodeAtomic, template.CovermodeAtomic)
	}
	// SHA pins need not be identical, but both sides must pin by commit SHA.
	if !live.CodecovSHAPin {
		diffs = append(diffs, "live codecov-action is not SHA-pinned (40-char commit hash required; floating tags like @v5 are not allowed)")
	}
	if !template.CodecovSHAPin {
		diffs = append(diffs, "template codecov-action is not SHA-pinned (40-char commit hash required; floating tags like @v5 are not allowed)")
	}
	return diffs
}

// FormatCodecovDrift builds a multi-line error message for CI/pre-commit output.
func FormatCodecovDrift(diffs []string) string {
	var b strings.Builder
	b.WriteString("codecov template drift: live workflow and embed template functional settings differ\n")
	b.WriteString("  live:     .github/workflows/codecov.yml\n")
	b.WriteString("  template: internal/templatefs/templates/dotgithub/workflows/public_codecov.yml\n")
	b.WriteString("  (action SHAs for checkout/setup-go/codecov and branch names may differ intentionally)\n")
	for _, d := range diffs {
		b.WriteString("  - ")
		b.WriteString(d)
		b.WriteByte('\n')
	}
	b.WriteString("Sync functional flags (OIDC, use_pypi, fail_ci_if_error, coverage filename/flags, SHA pin) before merging.")
	return b.String()
}
