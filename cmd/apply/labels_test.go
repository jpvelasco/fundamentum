package apply

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/revert"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/steps"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

func TestLabelPlanSelectorsAndErrors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	for _, selector := range []steps.Selector{steps.New([]string{"labels"}, nil), steps.New(nil, []string{"files"})} {
		items, err := labelItems(c, "o", "r", selector, io.Discard)
		if err != nil || len(items) != 1 || items[0].Action != wizard.ActionSkip {
			t.Fatalf("excluded = %v, %v", items, err)
		}
	}
	if calls != 0 {
		t.Fatal("excluded labels made API calls")
	}
	if _, err := labelItems(c, "o", "r", steps.New(nil, nil), io.Discard); err == nil {
		t.Fatal("status errors must abort")
	}
	items, err := labelItems(nil, "o", "r", steps.New(nil, nil), io.Discard)
	if err != nil || len(items) != 4 {
		t.Fatalf("init plan = %v, %v", items, err)
	}
	for _, item := range items {
		if item.Action != wizard.ActionCreate {
			t.Fatalf("init item = %+v", item)
		}
	}
}

func TestApplyLabelsSelectedSnapshotAndIdempotence(t *testing.T) {
	t.Cleanup(func() { globals.DryRun = false })
	globals.DryRun = false
	baseline := github.PriorityLabels()
	live := []github.Label{baseline[0], {Name: baseline[1].Name, Color: "abcdef", Description: "hand tuned"}, {Name: "bug", Color: "ffffff"}, {Name: "P0-quick-win"}}
	var stored *revert.Snapshot
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/labels" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(live)
		case strings.Contains(r.URL.Path, "/labels"):
			if stored == nil {
				t.Error("label mutation preceded snapshot write")
			}
			var label github.Label
			if err := json.NewDecoder(r.Body).Decode(&label); err != nil {
				t.Fatal(err)
			}
			if label.Name == "bug" || label.Name == "P0-quick-win" {
				t.Error("unmanaged label touched")
			}
			writes++
			found := false
			for i, old := range live {
				if old.Name == label.Name {
					live[i] = label
					found = true
				}
			}
			if !found {
				live = append(live, label)
			}
			w.WriteHeader(http.StatusCreated)
		case r.URL.Path == "/repos/o/r/contents/"+revert.BaselinePath && r.Method == http.MethodPut:
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			raw, err := base64.StdEncoding.DecodeString(body.Content)
			if err != nil {
				t.Fatal(err)
			}
			stored, err = revert.DecodeSnapshot(raw)
			if err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.URL.Path == "/repos/o/r":
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		case strings.HasSuffix(r.URL.Path, "/rulesets"):
			_, _ = w.Write([]byte(`[]`))
		case strings.Contains(r.URL.Path, "/vulnerability-alerts"), strings.Contains(r.URL.Path, "/automated-security-fixes"):
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/code-scanning/default-setup"):
			_, _ = w.Write([]byte(`{"state":"not-configured"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	var out strings.Builder
	items, err := labelItems(c, "o", "r", steps.New(nil, nil), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "effort") {
		t.Fatal("plan missing migration guidance")
	}
	// Existing matching critical skips; accept changed high and missing medium,
	// decline missing low using the actual wizard selection path.
	wizard.SelectInteractive(items, strings.NewReader("y\ny\nn\n"), io.Discard)
	if err := applyItems(c, "o", "r", "main", items, false, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if writes != 2 || stored == nil || len(stored.Labels) != 2 {
		t.Fatalf("writes=%d snapshot=%+v", writes, stored)
	}
	if !stored.Labels[0].Present || stored.Labels[0].Description != "hand tuned" || stored.Labels[1].Present {
		t.Fatalf("prior states = %+v", stored.Labels)
	}
	items, err = labelItems(c, "o", "r", steps.New(nil, nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// Decline the one still absent label. All others are idempotent skips.
	wizard.SelectInteractive(items, strings.NewReader("n\n"), io.Discard)
	if err := applyItems(c, "o", "r", "main", items, false, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if writes != 2 {
		t.Fatalf("reapply mutated labels: %d", writes)
	}
}

func TestLabelMutationRequiresSnapshot(t *testing.T) {
	for _, fail := range []string{"capture", "labels", "write"} {
		t.Run(fail, func(t *testing.T) {
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/o/r" && fail != "capture":
					_, _ = w.Write([]byte(`{"default_branch":"main"}`))
				case r.URL.Path == "/repos/o/r/labels" && r.Method == http.MethodGet && fail != "labels":
					_, _ = w.Write([]byte(`[]`))
				case strings.Contains(r.URL.Path, "/labels") && r.Method != http.MethodGet:
					writes++
					w.WriteHeader(http.StatusCreated)
				case strings.Contains(r.URL.Path, "/vulnerability-alerts"), strings.Contains(r.URL.Path, "/automated-security-fixes"):
					w.WriteHeader(http.StatusNoContent)
				case strings.Contains(r.URL.Path, "/default-setup"):
					_, _ = w.Write([]byte(`{"state":"not-configured"}`))
				case strings.HasSuffix(r.URL.Path, "/rulesets"):
					_, _ = w.Write([]byte(`[]`))
				default:
					w.WriteHeader(http.StatusForbidden)
				}
			}))
			defer srv.Close()
			c := github.NewClient("t", false).WithBaseURL(srv.URL)
			label := github.PriorityLabels()[0]
			item := wizard.Item{Name: "Label " + label.Name, LabelName: label.Name, Action: wizard.ActionCreate, Apply: func() error { return c.WriteLabel("o", "r", label, false) }}
			if err := applyItems(c, "o", "r", "main", []wizard.Item{item}, false, nil, io.Discard); err == nil {
				t.Fatal("snapshot failure must fail apply")
			}
			if writes != 0 {
				t.Fatalf("irreversible mutation: %d", writes)
			}
		})
	}
}

func TestLabelSnapshotPersistenceAcrossCommitAndPRPaths(t *testing.T) {
	for _, scenario := range []string{"direct", "pr", "conflict", "pr-failure", "empty-pr"} {
		t.Run(scenario, func(t *testing.T) {
			var snapshot *revert.Snapshot
			persisted, prOpened := false, false
			mutations := 0
			flow := runFlowHandler()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/owner/repo/labels" && r.Method == http.MethodGet:
					_ = json.NewEncoder(w).Encode([]github.Label{})
				case strings.Contains(r.URL.Path, "/labels"):
					if !persisted || snapshot == nil {
						t.Error("label mutated before snapshot persistence")
					}
					mutations++
					w.WriteHeader(http.StatusCreated)
				case r.URL.Path == "/repos/owner/repo/git/ref/heads/main":
					_, _ = w.Write([]byte(`{"object":{"sha":"abc123"}}`))
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/blobs"):
					var body struct {
						Content string `json:"content"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					raw, err := base64.StdEncoding.DecodeString(body.Content)
					if err != nil {
						t.Fatal(err)
					}
					snapshot, err = revert.DecodeSnapshot(raw)
					if err != nil {
						t.Fatal(err)
					}
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"sha":"blobsha"}`))
				case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/"):
					if strings.HasSuffix(r.URL.Path, "/main") && scenario == "conflict" {
						w.WriteHeader(http.StatusConflict)
						return
					}
					persisted = true
					w.WriteHeader(http.StatusOK)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
					if scenario == "pr-failure" {
						persisted = false
						w.WriteHeader(http.StatusForbidden)
						return
					}
					prOpened = true
					w.WriteHeader(http.StatusCreated)
					if scenario == "empty-pr" {
						_, _ = w.Write([]byte(`{"number":0}`))
						return
					}
					_, _ = w.Write([]byte(`{"number":42}`))
				default:
					flow(w, r)
				}
			}))
			defer srv.Close()
			c := github.NewClient("t", false).WithBaseURL(srv.URL)
			items, err := labelItems(c, "owner", "repo", steps.New(nil, nil), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			viaPR := scenario != "direct" && scenario != "conflict"
			err = applyItems(c, "owner", "repo", "main", items[:1], viaPR, nil, io.Discard)
			failure := scenario == "pr-failure" || scenario == "empty-pr"
			if failure {
				if err == nil || mutations != 0 {
					t.Fatalf("failed snapshot PR allowed mutation: %v, %d", err, mutations)
				}
				return
			}
			if err != nil || mutations != 1 {
				t.Fatalf("apply = %v, writes=%d", err, mutations)
			}
			if len(snapshot.Labels) != 1 || snapshot.Labels[0].Present || snapshot.Labels[0].Name != "priority:critical" {
				t.Fatalf("snapshot = %+v", snapshot)
			}
			if (scenario != "direct") != prOpened {
				t.Fatalf("PR routing = %v", prOpened)
			}
		})
	}
}

func TestLabelUpdatePreservesExistingName(t *testing.T) {
	prior := github.PriorityLabels()[0]
	prior.Name = "PRIORITY:CRITICAL"
	prior.Color = "ffffff"
	var updated github.Label
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]github.Label{prior})
			return
		}
		if r.Method != http.MethodPatch || !strings.HasSuffix(r.URL.Path, "/"+prior.Name) {
			t.Errorf("update renamed label: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	items, err := labelItems(github.NewClient("t", false).WithBaseURL(srv.URL), "o", "r", steps.New(nil, nil), io.Discard)
	if err != nil || items[0].Action != wizard.ActionUpdate {
		t.Fatalf("plan = %v, %v", items, err)
	}
	if err := items[0].Apply(); err != nil {
		t.Fatal(err)
	}
	if updated.Name != prior.Name || github.LabelDrift(updated, github.PriorityLabels()[0]) != "" {
		t.Fatalf("update = %+v", updated)
	}
}
