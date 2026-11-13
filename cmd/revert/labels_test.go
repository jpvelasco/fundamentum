package revert

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
)

func TestLabelSnapshotCompatibilityAndValidation(t *testing.T) {
	old, err := DecodeSnapshot([]byte(`{"version":1,"files":[],"settings":{},"security":{},"rulesets":{}}`))
	if err != nil || len(old.Labels) != 0 {
		t.Fatalf("old snapshot = %+v, %v", old, err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("old snapshot must never call label API") }))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	if err := old.CaptureLabels(c, "o", "r", nil); err != nil {
		t.Fatal(err)
	}
	if err := restoreLabels(c, "o", "r", old.Labels, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"version":1,"labels":[{"name":"bug","present":false}]}`,
		`{"version":1,"labels":[{"name":"priority:low"}]}`,
		`{"version":1,"labels":[{"name":"priority:low","present":"wrong"}]}`,
		`{"version":1,"labels":[{"name":"priority:low","present":false},{"name":"PRIORITY:LOW","present":true}]}`,
	} {
		if _, err := DecodeSnapshot([]byte(raw)); err == nil {
			t.Fatal("invalid label snapshot authorized mutation")
		}
	}
	snap := &Snapshot{Version: 1, Labels: []LabelState{{Name: "priority:high", Present: true, Color: "abc123", Description: "old"}, {Name: "priority:low", Present: false}}}
	raw, err := snap.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSnapshot(raw)
	if err != nil || len(got.Labels) != 2 || got.Labels[0] != snap.Labels[0] || got.Labels[1].Present {
		t.Fatalf("roundtrip = %+v, %v", got, err)
	}
	for _, mode := range []Mode{ModeFull, ModeFiles, ModeSettings} {
		rows, err := plan(c, "o", "r", snap, mode)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Join(rows, "\n")
		if (mode == ModeFull) != strings.Contains(text, "restore label priority:high") {
			t.Fatalf("plan %s = %s", mode, text)
		}
		if mode == ModeFull && !strings.Contains(text, "removes issue assignments") {
			t.Fatal("delete plan must disclose assignment loss")
		}
	}
}

func TestRestoreLabelsMetadataPresenceAndIdempotence(t *testing.T) {
	labels := []github.Label{{Name: "priority:high", Color: "ffffff", Description: "canonical now"}, {Name: "priority:low", Color: "ffffff"}, {Name: "bug", Color: "00ff00"}}
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(labels)
			return
		}
		writes++
		if strings.HasSuffix(r.URL.Path, "/bug") {
			t.Error("hand-added label touched")
		}
		if r.Method == http.MethodDelete {
			name := strings.TrimPrefix(r.URL.Path, "/repos/o/r/labels/")
			for i, label := range labels {
				if label.Name == name {
					labels = append(labels[:i], labels[i+1:]...)
					break
				}
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var wanted github.Label
		if err := json.NewDecoder(r.Body).Decode(&wanted); err != nil {
			t.Fatal(err)
		}
		found := false
		for i, label := range labels {
			if label.Name == wanted.Name {
				labels[i] = wanted
				found = true
			}
		}
		if !found {
			labels = append(labels, wanted)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	states := []LabelState{{Name: "priority:high", Present: true, Color: "abc123", Description: "original"}, {Name: "priority:low", Present: false}, {Name: "priority:medium", Present: true, Color: "fedcba", Description: "deleted since apply"}}
	for i := 0; i < 2; i++ {
		if err := restoreLabels(c, "o", "r", states, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if writes != 3 {
		t.Fatalf("revert not idempotent, writes=%d", writes)
	}
	if got, ok := github.FindLabel(labels, "bug"); !ok || got.Color != "00ff00" {
		t.Fatal("unrelated label changed")
	}
	if got, ok := github.FindLabel(labels, "priority:high"); !ok || got.Description != "original" {
		t.Fatal("original metadata lost")
	}
}

func TestLabelRestoreErrors(t *testing.T) {
	for _, operation := range []string{"list", "create", "delete"} {
		t.Run(operation, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && operation != "list" {
					labels := []github.Label{}
					if operation == "delete" {
						labels = github.PriorityLabels()
					}
					_ = json.NewEncoder(w).Encode(labels)
					return
				}
				w.WriteHeader(http.StatusForbidden)
			}))
			defer srv.Close()
			state := LabelState{Name: "priority:low", Present: operation != "delete", Color: "abcdef"}
			if err := restoreLabels(github.NewClient("t", false).WithBaseURL(srv.URL), "o", "r", []LabelState{state}, io.Discard); err == nil {
				t.Fatal("restore errors must fail")
			}
		})
	}
}

func TestRevertLabelModesAndDryRun(t *testing.T) {
	for _, mode := range []Mode{ModeFull, ModeFiles, ModeSettings} {
		for _, dry := range []bool{true, false} {
			t.Run(string(mode)+fmt.Sprint(dry), func(t *testing.T) {
				snap := testSnapshot()
				snap.Labels = []LabelState{{Name: "priority:low", Present: false}}
				fallback := mockRevertServer(t, snap)
				defer fallback.Close()
				calls, writes := 0, 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.Contains(r.URL.Path, "/labels") {
						calls++
						if r.Method == http.MethodGet {
							_ = json.NewEncoder(w).Encode(github.PriorityLabels())
							return
						}
						writes++
						w.WriteHeader(http.StatusNoContent)
						return
					}
					fallback.Config.Handler.ServeHTTP(w, r)
				}))
				defer srv.Close()
				if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", mode, dry, io.Discard); err != nil {
					t.Fatal(err)
				}
				expected := mode == ModeFull && !dry
				if expected && writes != 1 {
					t.Fatalf("full revert writes=%d", writes)
				}
				if !expected && calls != 0 {
					t.Fatalf("mode %s dry=%v called label API %d times", mode, dry, calls)
				}
			})
		}
	}
}

func TestSnapshotCaptureSelectedLabels(t *testing.T) {
	prior := github.Label{Name: "PRIORITY:HIGH", Color: "abc123", Description: "custom before apply"}
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet {
					t.Error("capture mutated labels")
				}
				if fail {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				_ = json.NewEncoder(w).Encode([]github.Label{prior, {Name: "bug"}, github.PriorityLabels()[0]})
			}))
			defer srv.Close()
			snap := &Snapshot{Version: 1}
			err := snap.CaptureLabels(github.NewClient("t", false).WithBaseURL(srv.URL), "o", "r", []string{"priority:high", "priority:low"})
			if fail {
				if err == nil || len(snap.Labels) != 0 {
					t.Fatalf("failed capture partially populated snapshot: %+v, %v", snap, err)
				}
				return
			}
			if err != nil || calls != 1 || len(snap.Labels) != 2 {
				t.Fatalf("capture=%+v, calls=%d, %v", snap, calls, err)
			}
			got := snap.Labels[0]
			if !got.Present || got.Name != prior.Name || got.Color != prior.Color || got.Description != prior.Description {
				t.Fatalf("lost original metadata: %+v", got)
			}
			if snap.Labels[1].Name != "priority:low" || snap.Labels[1].Present {
				t.Fatalf("missing label record = %+v", snap.Labels[1])
			}
		})
	}
}
