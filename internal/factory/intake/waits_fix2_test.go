package intake_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// W8: waits set after the list still refuse automatic start and ask-first admission.
func TestIntakeWaitsSetDuringDecision(t *testing.T) {
	for _, ask := range []bool{false, true} {
		t.Run(fmt.Sprintf("ask=%v", ask), func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetRepos([]string{"owner/api"}); err != nil {
				t.Fatal(err)
			}
			parent, err := st.Create(factory.Item{Repo: "owner/api", Kind: factory.KindIssue, State: factory.StateNew})
			if err != nil {
				t.Fatal(err)
			}
			child, err := st.Create(factory.Item{Repo: "owner/api", Kind: factory.KindIssue, State: factory.StateNew, Tier: factory.TierOwner, Labels: []string{"agent-ready", "size:S"}})
			if err != nil {
				t.Fatal(err)
			}
			history, err := record.Open(st, record.Options{})
			if err != nil {
				t.Fatal(err)
			}
			policy := "default_budget: 1\nstart:\n  labels: [agent-ready]\n"
			if ask {
				policy += "  ask_first: [changes secrets]\n"
			}
			file, err := pipeline.Parse(policy)
			if err != nil {
				t.Fatal(err)
			}
			h := serve.New(factory.Seam{Load: func() (factory.Snapshot, error) {
				items, err := st.List()
				return factory.Snapshot{Items: items}, err
			}}, serve.Config{Mode: api.FactorySolo, AccessToken: "test-secret", Store: st, Record: history})
			t.Cleanup(h.Close)
			loaded, resume := make(chan struct{}), make(chan struct{})
			launched := 0
			in := intake.New(st, intake.Options{Record: history, Load: func(string) (pipeline.File, error) {
				close(loaded)
				<-resume
				return file, nil
			}, Launch: func(int) error { launched++; return nil }})
			done := make(chan error, 1)
			finished := make(chan struct{})
			defer func() { close(resume); <-finished }()
			go func() { defer close(finished); done <- in.Once(context.Background()) }()
			<-loaded
			r := httptest.NewRequest("PUT", fmt.Sprintf("/v1/jobs/%d/waits_on", child.ID), strings.NewReader(fmt.Sprintf(`{"waits_on":[%d]}`, parent.ID)))
			r.Header.Set("Authorization", "Bearer test-secret")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("set waits = %d %s", w.Code, w.Body)
			}
			before, err := st.Get(child.ID)
			if err != nil {
				t.Fatal(err)
			}
			resume <- struct{}{}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			after, err := st.Get(child.ID)
			if err != nil {
				t.Fatal(err)
			}
			events, err := history.Since(context.Background(), time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if launched != 0 || after.State != factory.StateNew || after.QKind != "" || after.AskedFirstReason != "" || !reflect.DeepEqual(after, before) || len(events) != 0 {
				t.Fatalf("pending waits admitted: launches=%d job=%+v events=%+v", launched, after, events)
			}
		})
	}
}
