package serve_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// A PERSON'S START STAMPS THE RULES THE JOB STARTS UNDER: the repository's
// ask-first and never lines (here the defaults, which name none) go on the job
// so its plan can be checked against them wherever it runs, and the factory's
// own rules ride along when it is queued.
func TestAStartStampsTheRulesTheJobStartsUnder(t *testing.T) {
	f := newV2Jobs(t)
	if _, err := f.st.ChangeHouseRules(func(h *factory.HouseRules) error {
		h.Rules = []string{"no new dependencies without asking"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", RuleHold: "left over"})
	// What an earlier start by the factory itself found, before this person's.
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.SetStartCheck(factory.CheckPassed); return nil }); err != nil {
		t.Fatal(err)
	}
	f.post(t, it, "run", "", 200)
	cur, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.StartRules == nil || cur.StartRules.AskFirst == nil || cur.StartRules.Never == nil {
		t.Fatalf("start rules %#v", cur.StartRules)
	}
	if cur.RuleHold != "" {
		t.Fatalf("an earlier hold survived the start: %q", cur.RuleHold)
	}
	if cur.RuleChecks != nil && cur.RuleChecks.Start != "" {
		t.Fatalf("the factory's earlier start finding survived a person's start: %+v", cur.RuleChecks)
	}
	if cur.House == nil || !reflect.DeepEqual(cur.House.Rules, []string{"no new dependencies without asking"}) {
		t.Fatalf("house rules %#v", cur.House)
	}
}

// A REPOSITORY FILE THAT CANNOT BE READ DOES NOT STOP A PERSON'S START: the
// read's defaults, with the factory's rules, go on the job, marked unread, so
// its plan waits for a person. Only the factory's own rules unread refuse it,
// in plain words.
func TestAStartGoesOnWhenOnlyTheRepositoryFileIsUnread(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	unread := errors.New(".codeaf/factory.yaml: github is not connected")
	f.cfg.Pipeline = func(context.Context, string, string) (pipeline.File, error) {
		p := pipeline.Defaults()
		p.Start.AskFirst = []string{"touch auth/"}
		return p, unread
	}
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Cap: 3})
	f.post(t, it, "run", "", 200)
	cur, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.StartRules == nil || !reflect.DeepEqual(cur.StartRules.AskFirst, []string{"touch auth/"}) || !cur.StartRules.Unread {
		t.Fatalf("start rules %#v", cur.StartRules)
	}
	// ITS PLAN IS A PERSON'S, whatever files it names: the repository's own
	// lines may be missing from what the job carries.
	if hold := pipeline.PlanHold(cur.StartRules, []string{"cmd/main.go"}); hold != factory.StartRulesUnreadHold {
		t.Fatalf("a plan of a job started with its rules unread holds %q", hold)
	}
	f.h.Close()
	f.cfg.Pipeline = func(context.Context, string, string) (pipeline.File, error) {
		return pipeline.Defaults(), fmt.Errorf("%w: disk", pipeline.ErrHouseRulesUnread)
	}
	f.h = serve.New(f.seam, f.cfg)
	other := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Cap: 3, Num: 2})
	w := f.post(t, other, "run", "", 503)
	if !strings.Contains(w.Body.String(), "factory's own rules could not be read") {
		t.Fatalf("refusal %s", w.Body.String())
	}
}
