package exportcmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/steps"
)

func TestNewCmd(t *testing.T) {
	cmd := NewCmd()
	if cmd.Use != "export" {
		t.Errorf("Use = %q, want export", cmd.Use)
	}
	if cmd.Flags().Lookup("output") == nil {
		t.Error("expected --output flag")
	}
}

func TestRun_Stdout(t *testing.T) {
	t.Cleanup(func() {
		globals.Preset = ""
		globals.Strict = false
		globals.AdvancedSecurity = false
	})
	globals.Preset = "oss"
	var out strings.Builder
	if err := run("", &out); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"version": 1`) || !strings.Contains(got, `"preset": "oss"`) {
		t.Errorf("expected oss baseline JSON, got:\n%s", got)
	}
}

func TestRun_File(t *testing.T) {
	t.Cleanup(func() { globals.Preset = "" })
	globals.Preset = "strict"
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	var out strings.Builder
	if err := run(path, &out); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	if !strings.Contains(out.String(), "wrote baseline") {
		t.Errorf("expected write confirmation, got:\n%s", out.String())
	}
	globals.Preset = ""
	if err := globals.LoadBaselineFile(path); err != nil {
		t.Fatal(err)
	}
	if globals.Preset != "strict" {
		t.Errorf("loaded preset = %q, want strict", globals.Preset)
	}
}

func TestRun_InvalidPreset(t *testing.T) {
	t.Cleanup(func() { globals.Preset = "" })
	globals.Preset = "enterprise"
	if err := run("", &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "invalid --preset") {
		t.Fatalf("error = %v, want invalid --preset", err)
	}
}

type fakeSink struct {
	writeErr error
	closeErr error
	closed   bool
}

func (s *fakeSink) Write(p []byte) (int, error) {
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return len(p), nil
}

func (s *fakeSink) Close() error {
	s.closed = true
	return s.closeErr
}

func TestWriteFile_CloseErrorIsReturnedNotReported(t *testing.T) {
	sink := &fakeSink{closeErr: errors.New("disk full")}
	var out strings.Builder
	err := writeFile(sink, "baseline.json", globals.Baseline{}, &out)
	if err == nil || !strings.Contains(err.Error(), "close baseline.json") || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v, want close error naming the path", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want no success message on close failure", out.String())
	}
}

func TestWriteFile_WriteErrorStillClosesSink(t *testing.T) {
	sink := &fakeSink{writeErr: errors.New("short write")}
	var out strings.Builder
	if err := writeFile(sink, "baseline.json", globals.Baseline{}, &out); err == nil {
		t.Fatal("expected write error")
	}
	if !sink.closed {
		t.Error("sink was not closed after write failure")
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want no success message on write failure", out.String())
	}
}

func TestWriteFile_Success(t *testing.T) {
	sink := &fakeSink{}
	var out strings.Builder
	if err := writeFile(sink, "baseline.json", globals.Baseline{}, &out); err != nil {
		t.Fatalf("writeFile() error: %v", err)
	}
	if !sink.closed {
		t.Error("sink was not closed")
	}
	if out.String() != "wrote baseline baseline.json\n" {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestRun_CreateError(t *testing.T) {
	err := run(filepath.Join(t.TempDir(), "missing", "baseline.json"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "create") {
		t.Fatalf("error = %v, want create", err)
	}
}

func TestLabelsSelectorBaselineRoundTrip(t *testing.T) {
	t.Cleanup(func() { globals.SkipSteps = nil; globals.OnlySteps = nil })
	for _, skip := range []bool{false, true} {
		globals.SkipSteps = nil
		globals.OnlySteps = nil
		if skip {
			globals.SkipSteps = []string{"labels"}
		} else {
			globals.OnlySteps = []string{"labels"}
		}
		path := filepath.Join(t.TempDir(), "baseline.json")
		if err := run(path, &strings.Builder{}); err != nil {
			t.Fatal(err)
		}
		globals.SkipSteps = nil
		globals.OnlySteps = nil
		if err := globals.LoadBaselineFile(path); err != nil {
			t.Fatal(err)
		}
		selector := steps.New(globals.SkipTokens(), globals.OnlySteps)
		if selector.IncludeStep(steps.Labels) == skip {
			t.Fatalf("labels selection lost in roundtrip (skip=%v)", skip)
		}
		if !skip && selector.IncludeStep(steps.Settings) {
			t.Fatal("only labels expanded to settings")
		}
		if len(selector.Unknown(nil)) != 0 {
			t.Fatal("labels key unrecognized")
		}
	}
}
