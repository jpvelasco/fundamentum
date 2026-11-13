package audit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/steps"
)

func TestLabelAuditSeverityAndSelectors(t *testing.T) {
	labels := github.PriorityLabels()
	labels[1].Color = "ffffff"
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _ = json.NewEncoder(w).Encode(labels[:3]) }))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	for _, selector := range []steps.Selector{steps.New([]string{"labels"}, nil), steps.New(nil, []string{"settings"})} {
		rows, err := labelChecks(c, "o", "r", selector)
		if err != nil || len(rows) != 1 || rows[0].Status != statusSkip {
			t.Fatalf("skip = %v, %v", rows, err)
		}
	}
	if calls != 0 {
		t.Fatal("excluded audit called labels API")
	}
	rows, err := labelChecks(c, "o", "r", steps.New(nil, nil))
	if err != nil || len(rows) != 4 {
		t.Fatalf("audit = %v, %v", rows, err)
	}
	if rows[0].Status != statusPass || rows[1].Status != statusWarn || rows[3].Status != statusFail {
		t.Fatalf("severity = %v", rows)
	}
	t.Cleanup(func() { globals.Strict = false })
	globals.Strict = false
	if failedRequired(rows[:3]) {
		t.Fatal("metadata warnings should be soft")
	}
	globals.Strict = true
	if !failedRequired(rows[:3]) {
		t.Fatal("strict should fail warnings")
	}
	if !failedRequired(rows) {
		t.Fatal("missing labels must fail")
	}
}

func TestLabelAuditErrorAndMigration(t *testing.T) {
	for _, forbidden := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if forbidden {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_ = json.NewEncoder(w).Encode(append(github.PriorityLabels(), github.Label{Name: "P1-medium"}))
		}))
		rows, err := labelChecks(github.NewClient("t", false).WithBaseURL(srv.URL), "o", "r", steps.New(nil, nil))
		srv.Close()
		if forbidden && err == nil {
			t.Fatal("audit must surface API failure")
		}
		if !forbidden && (err != nil || len(rows) != 5 || rows[4].Status != statusWarn) {
			t.Fatalf("migration = %v, %v", rows, err)
		}
	}
}
