package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// A busy matrix has more statuses than GitHub's default page and more check
// runs than one page of 100: every page is read, and a failure on the last
// page still decides the commit.
func TestCheckRunsReadsEveryPage(t *testing.T) {
	const statuses, runs = 130, 150
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		if per < 1 {
			per = 30
		}
		slice := func(total int) (int, int) {
			from := min((page-1)*per, total)
			return from, min(from+per, total)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/status"):
			from, to := slice(statuses)
			rows := []map[string]any{}
			for i := from; i < to; i++ {
				rows = append(rows, map[string]any{"context": fmt.Sprintf("status-%d", i), "state": "success"})
			}
			json.NewEncoder(w).Encode(map[string]any{"state": "success", "total_count": statuses, "statuses": rows})
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			from, to := slice(runs)
			rows := []map[string]any{}
			for i := from; i < to; i++ {
				conclusion := "success"
				if i == runs-1 {
					conclusion = "failure"
				}
				rows = append(rows, map[string]any{"name": fmt.Sprintf("run-%d", i), "status": "completed", "conclusion": conclusion, "app": map[string]int{"id": 15368}})
			}
			json.NewEncoder(w).Encode(map[string]any{"total_count": runs, "check_runs": rows})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	state, out, err := testClient(server.URL, "tok").CheckRuns(context.Background(), "o", "r", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if state != ChecksFailed || len(out) != statuses+runs {
		t.Fatalf("state=%v runs=%d", state, len(out))
	}
}
