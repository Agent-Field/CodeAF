package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// The search reads only issue jobs that are not dropped or dismissed, in the
// order the bar sent (ids it was not given are skipped), each with the words
// the judge reads off its desk.
func TestAISearchReadsTheCallersIssueJobsInTheBarsOrder(t *testing.T) {
	q := api.Queue{Jobs: []api.Job{
		{ID: 1, Repo: "acme/api", Num: 1, Kind: "issue", Title: "Ledger total is wrong", Desk: &api.JobDesk{Body: "the sum", Labels: []string{"bug"}, Triage: api.Triage{Area: "ledger"}}},
		{ID: 2, Repo: "acme/api", Num: 2, Kind: "issue", Title: "Dropped", State: api.StateDropped},
		{ID: 3, Repo: "acme/api", Num: 3, Kind: "issue", Title: "Dismissed", Dismissed: true},
		{ID: 4, Repo: "acme/api", Num: 4, Title: "No kind is an issue"},
		{ID: 5, Repo: "acme/api", Num: 5, Kind: "pull", Title: "A pull request"},
	}}
	all := appAISearchItems(q, nil)
	if len(all) != 2 || all[0].ID != 1 || all[1].ID != 4 {
		t.Fatalf("read %+v", all)
	}
	if all[0].Body != "the sum" || all[0].Labels[0] != "bug" || all[0].Triage.Area != "ledger" {
		t.Fatalf("words %+v", all[0])
	}
	if some := appAISearchItems(q, []int{4, 2, 9, 1}); len(some) != 2 || some[0].ID != 4 || some[1].ID != 1 {
		t.Fatalf("scoped %+v", some)
	}
}
