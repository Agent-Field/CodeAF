package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestIdentityReviewsWireCLIParity(t *testing.T) {
	backend, _ := dataBackend(t)
	t.Setenv(home.EnvVar, backend.data.home)
	wire := wireOver(t, backend)
	stop, err := backend.serveAppCommands()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli := func(verb string, args any, result any) {
		t.Helper()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.CreateTemp(t.TempDir(), "cli-*.json")
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		stdout := os.Stdout
		os.Stdout = out
		defer func() { os.Stdout = stdout }()
		if err := runApp([]string{verb, string(raw)}); err != nil {
			t.Fatal(err)
		}
		reply, err := os.ReadFile(out.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(reply, result); err != nil {
			t.Fatal(err)
		}
	}
	wire.must("apps.bind_common_type", appwire.BindCommonTypeParams{Common: "person", Type: "contact"}, &appwire.BindCommonTypeResult{})
	pair := func(name, email string) (string, string) {
		var left, right appwire.DataWriteResult
		wire.must("data.create", appwire.DataCreateParams{Type: "contact", Set: map[string]any{"name": name, "email": email}}, &left)
		wire.must("data.create", appwire.DataCreateParams{Type: "contact", Set: map[string]any{"name": name, "email": email}}, &right)
		return left.Rows[0]["id"].(string), right.Rows[0]["id"].(string)
	}
	list := func() []appwire.IdentityDecisionParams {
		var w, c appwire.IdentityCandidatesResult
		wire.must("apps.match_candidates", appwire.Empty{}, &w)
		cli("match-candidates", appwire.Empty{}, &c)
		if !reflect.DeepEqual(w, c) {
			t.Fatalf("wire = %+v, CLI = %+v", w, c)
		}
		params := []appwire.IdentityDecisionParams{}
		for _, m := range w.Candidates {
			params = append(params, appwire.IdentityDecisionParams{ID: m.ID, Survivor: m.Left})
		}
		return params
	}
	left, right := pair("Amina Clough", "amina@example.org")
	cli("match-scan", appwire.IdentityScanParams{Type: "contact", ID: left}, &appwire.Empty{})
	p := list()
	if len(p) != 1 {
		t.Fatalf("reviews = %+v", p)
	}
	item := appwire.DataItemsResult{}
	wire.must("work_items.list", appwire.DataScope{}, &item)
	if len(item.Items) != 1 || item.Items[0]["title"] != "Are these the same person?" {
		t.Fatalf("work item = %+v", item)
	}
	var cliResult, wireResult appwire.IdentityDecisionResult
	wire.must("apps.match_accept", p[0], &wireResult)
	cli("match-accept", p[0], &cliResult)
	if !reflect.DeepEqual(wireResult, cliResult) || wireResult.Merge == nil || wireResult.Merge.Alias != right {
		t.Fatalf("accept wire = %+v, CLI = %+v", wireResult.Merge, cliResult.Merge)
	}
	cli("match-unmerge", appwire.IdentityUnmergeParams{ID: cliResult.Merge.ID, Revision: cliResult.Merge.Revision}, &appwire.Empty{})

	_, other := pair("Simone Arden", "simone@example.net")
	wire.must("apps.match_scan", appwire.IdentityScanParams{Type: "contact", ID: other}, &appwire.Empty{})
	p = list()
	if len(p) != 1 {
		t.Fatalf("reviews = %+v", p)
	}
	reject := appwire.IdentityDecisionParams{ID: p[0].ID}
	cli("match-reject", reject, &cliResult)
	wire.must("apps.match_reject", reject, &wireResult)
	if !reflect.DeepEqual(wireResult, cliResult) {
		t.Fatalf("reject wire = %+v, CLI = %+v", wireResult, cliResult)
	}
	wire.mustFail("apps.match_accept", p[0], appwire.CodeStale)
	if pending := list(); len(pending) != 0 {
		t.Fatalf("decision did not close review: %+v", pending)
	}
}
