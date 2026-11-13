package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLabelsPaginationAndReconcile(t *testing.T) {
	labels := PriorityLabels()
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("page") == "2" {
				_ = json.NewEncoder(w).Encode(labels)
				return
			}
			w.Header().Set("Link", `</repos/o/r/labels?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"name":"bug","color":"ffffff"}]`))
		case http.MethodPost, http.MethodPatch:
			writes++
			var got Label
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got != labels[0] {
				t.Errorf("body = %+v", got)
			}
			w.WriteHeader(http.StatusCreated)
		case http.MethodDelete:
			writes++
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := NewClient("t", false).WithBaseURL(srv.URL)
	got, err := c.ListLabels("o", "r")
	if err != nil || len(got) != 5 {
		t.Fatalf("list = %+v, %v", got, err)
	}
	if err := c.WriteLabel("o", "r", labels[0], false); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteLabel("o", "r", labels[0], true); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteLabel("o", "r", labels[0].Name); err != nil {
		t.Fatal(err)
	}
	if writes != 3 {
		t.Fatalf("writes = %d", writes)
	}
	same := labels[0]
	same.Color = "#8B0000"
	if LabelDrift(same, labels[0]) != "" {
		t.Fatal("color normalization drifted")
	}
	same.Description = "custom"
	if LabelDrift(same, labels[0]) != "description" {
		t.Fatal("description drift undetected")
	}
	same.Color = "ffffff"
	if LabelDrift(same, labels[0]) != "color, description" {
		t.Fatal("combined drift undetected")
	}
	if _, ok := FindLabel(got, "PRIORITY:CRITICAL"); !ok {
		t.Fatal("case-insensitive name lookup")
	}
}

func TestLabelAPIFailures(t *testing.T) {
	for _, body := range []string{"forbidden", "{"} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if body == "forbidden" {
					w.WriteHeader(http.StatusForbidden)
				}
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			c := NewClient("t", false).WithBaseURL(srv.URL)
			if _, err := c.ListLabels("o", "r"); err == nil {
				t.Fatal("list must fail closed")
			}
			if body == "forbidden" {
				if err := c.WriteLabel("o", "r", PriorityLabels()[0], false); err == nil {
					t.Fatal("create must fail")
				}
				if err := c.DeleteLabel("o", "r", "priority:low"); err == nil {
					t.Fatal("delete must fail")
				}
			}
		})
	}
	c := NewClient("t", false).WithBaseURL("http://127.0.0.1:1")
	if _, err := c.ListLabels("o", "r"); err == nil {
		t.Fatal("transport list")
	}
	if err := c.WriteLabel("o", "r", PriorityLabels()[0], true); err == nil {
		t.Fatal("transport write")
	}
	if err := c.DeleteLabel("o", "r", "priority:low"); err == nil {
		t.Fatal("transport delete")
	}
}

func TestPriorityMigrationAdvice(t *testing.T) {
	if got := PriorityMigrationAdvice(PriorityLabels()); len(got) != 0 {
		t.Fatalf("canonical advice = %v", got)
	}
	got := PriorityMigrationAdvice([]Label{{Name: "P0-quick-win"}, {Name: "priority:urgent"}, {Name: "bug"}})
	if len(got) != 2 || !strings.Contains(got[0], "effort") || !strings.Contains(got[0], "priority:critical") || !strings.Contains(got[1], "importance") {
		t.Fatalf("advice = %v", got)
	}
}
