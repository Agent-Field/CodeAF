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

// Pagination must fail closed when a list is incomplete or changes while being
// read. A partial successful page must never authorize a readiness transition.
func TestCheckRunsRejectsIncompletePaging(t *testing.T) {
	for _, area := range []string{"status", "check-runs"} {
		for _, mode := range []string{"short", "shrunk", "grew", "error", "limit"} {
			t.Run(area+"/"+mode, func(t *testing.T) {
				pages := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !strings.HasSuffix(r.URL.Path, "/"+area) {
						fmt.Fprint(w, `{"total_count":0,"statuses":[],"check_runs":[]}`)
						return
					}
					pages++
					if r.URL.Query().Get("per_page") != "100" {
						t.Error("CI page size was not requested")
					}
					page, _ := strconv.Atoi(r.URL.Query().Get("page"))
					total, count := 150, 100
					if page > 1 {
						count = 50
						switch mode {
						case "short":
							count = 1
						case "shrunk":
							total, count = 100, 0
						case "grew":
							total = 151
						case "error":
							http.Error(w, "access denied", http.StatusForbidden)
							return
						}
					}
					if mode == "limit" {
						total, count = 1001, 100
					}
					rows := make([]map[string]any, 0, count)
					for i := 0; i < count; i++ {
						rows = append(rows, map[string]any{"context": fmt.Sprintf("status-%d-%d", page, i), "state": "success", "name": fmt.Sprintf("run-%d-%d", page, i), "status": "completed", "conclusion": "success"})
					}
					key := "statuses"
					if area == "check-runs" {
						key = "check_runs"
					}
					json.NewEncoder(w).Encode(map[string]any{"total_count": total, key: rows})
				}))
				defer server.Close()
				state, rows, err := testClient(server.URL, "tok").CheckRuns(context.Background(), "o", "r", "abc")
				if err == nil || state != ChecksNone || len(rows) != 0 {
					t.Fatalf("partial read was accepted: state=%v rows=%d err=%v", state, len(rows), err)
				}
				wantPages := 2
				if mode == "limit" {
					wantPages = checkPagesMost
				}
				if pages != wantPages {
					t.Fatalf("read %d pages, want %d", pages, wantPages)
				}
			})
		}
	}
}
