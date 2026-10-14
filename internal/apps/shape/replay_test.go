package shape

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func replayFiles(t *testing.T, s *Service, scope svc.Scope) spec.Files {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	files := cloneFiles(s.drafts[draftKey(scope)].BaseFiles)
	files["agent.md"] = append(files["agent.md"], []byte("\nnew live base")...)
	return files
}

func TestReplayAllPassOrderedAndDismiss(t *testing.T) {
	s, _, scope, shaper, events := pipelineRig(t)
	for _, request := range []string{"first request", "second request"} {
		c, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: request})
		if err != nil {
			t.Fatal(err)
		}
		waitChange(t, events, c.ID, "passed")
	}
	s.replayOn(scope.App, 2, "raj", replayFiles(t, s, scope))
	seen := waitShape(t, events, func(e bridge.ShapeEvent) bool { return e.Kind == "replayed" })
	got := seen[len(seen)-1].Draft
	if got.Base != 2 || got.State != "ready" || got.Replayed.From != 1 || got.Replayed.To != 2 || got.Replayed.By != "raj" || got.Replayed.Requests != 2 || got.Replayed.Failed != 0 {
		t.Fatalf("replay=%+v", got)
	}
	calls := shaper.calls()
	if len(calls) != 4 || calls[2].Request != "first request" || calls[3].Request != "second request" || !strings.Contains(string(calls[3].Files["agent.md"]), "first request") || !strings.Contains(string(calls[2].Files["agent.md"]), "new live base") {
		t.Fatalf("replayed calls=%+v", calls)
	}
	dismissed, err := s.DismissReplay(context.Background(), scope, bridge.Empty{})
	if err != nil || !dismissed.Replayed.Dismissed {
		t.Fatalf("dismiss=%+v %v", dismissed, err)
	}
}

func TestReplayQuestionAnswerAndDrop(t *testing.T) {
	for _, answer := range []string{"stage", "drop", "free text"} {
		t.Run(answer, func(t *testing.T) {
			s, all, scope, shaper, events := pipelineRig(t)
			requests := []string{"rename Value to Amount", "group by source", "add footer"}
			ids := []string{}
			for _, request := range requests {
				c, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: request})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, c.ID)
				waitChange(t, events, c.ID, "passed")
			}
			shaper.shape = func(in svc.ShapeInput) (svc.ShapeOutput, error) {
				if in.Request == "group by source" && strings.Contains(string(in.Files["agent.md"]), "new live base") {
					return svc.ShapeOutput{Question: &bridge.ShapeQuestion{Text: "Source was removed. How should the pipeline be grouped?", Options: []bridge.ShapeQuestionOption{{ID: "owner", Label: "By owner"}, {ID: "stage", Label: "By stage"}, {ID: "drop", Label: "Drop this request"}}}}, nil
				}
				return svc.ShapeOutput{Files: spec.Files{"agent.md": append(in.Files["agent.md"], []byte("\n"+in.Request)...)}}, nil
			}
			s.replayOn(scope.App, 2, "raj", replayFiles(t, s, scope))
			seen := waitShape(t, events, func(e bridge.ShapeEvent) bool {
				return e.Draft != nil && e.Draft.State == "question" && e.Draft.Replayed != nil && e.Draft.Replayed.Failed == 1
			})
			question := seen[len(seen)-1].Draft
			if question.Changes[1].State != "question" || question.Changes[1].Question == nil {
				t.Fatalf("question=%+v", question)
			}
			items := all.Approvals.(*fakeShapeApprovals).list()
			if len(items) != 1 || items[0].Kind != "shape_question" || items[0].Who != scope.Caller.Member || items[0].Title != "CRM · Builder needs you" {
				t.Fatalf("inbox=%+v", items)
			}
			if _, err := s.Answer(context.Background(), scope, bridge.ShapeAnswerParams{Change: ids[1], Option: "bad"}); err == nil {
				t.Fatal("unknown option accepted")
			}
			params := bridge.ShapeAnswerParams{Change: ids[1], Option: answer}
			if answer == "free text" {
				params.Option = ""
				params.Text = "group by owner"
			}
			c, err := s.Answer(context.Background(), scope, params)
			if err != nil {
				t.Fatal(err)
			}
			if answer == "drop" {
				if c.State != "cancelled" {
					t.Fatalf("drop=%+v", c)
				}
				got, _ := s.Get(context.Background(), scope, bridge.Empty{})
				if len(got.Draft.Changes) != 2 || got.Draft.State != "ready" {
					t.Fatalf("drop draft=%+v", got)
				}
			} else {
				waitShape(t, events, func(e bridge.ShapeEvent) bool { return e.Kind == "replayed" })
				got, _ := s.Get(context.Background(), scope, bridge.Empty{})
				want := "By stage"
				if answer == "free text" {
					want = "group by owner"
				}
				if got.Draft.State != "ready" || got.Draft.Changes[1].Request != want || got.Draft.Changes[1].Question != nil {
					t.Fatalf("answered=%+v", got)
				}
				s.mu.Lock()
				files, err := s.drafts[draftKey(scope)].Files()
				s.mu.Unlock()
				if err != nil || strings.Count(string(files["agent.md"]), "add footer") != 1 {
					t.Fatalf("later change was not replayed after answer: %s %v", files["agent.md"], err)
				}
			}
		})
	}
}

func TestReplayViaRealApplyHookAndPersonalApply(t *testing.T) {
	s, all, scope, _, events := pipelineRig(t)
	// Real apply auto-applies a personal change and rebases the other member's draft.
	all.Shape = s
	other := scope
	other.Caller.Member = "raj"
	openDraft(t, s, other)
	otherEvents, cancel := all.Hub.Subscribe(bridge.TopicShape(other.App, other.Caller.Member))
	defer cancel()
	c, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "agent should suggest the next step"})
	if err != nil {
		t.Fatal(err)
	}
	_ = c
	seen := waitShape(t, events, func(e bridge.ShapeEvent) bool { return e.Kind == "applied" })
	if seen[len(seen)-1].Version.N != 2 {
		t.Fatal("personal change did not create v2")
	}
	replayed := waitShape(t, otherEvents, func(e bridge.ShapeEvent) bool { return e.Kind == "replayed" })
	got := replayed[len(replayed)-1].Draft
	if got.Base != 2 || got.Replayed.By != scope.Caller.Member {
		t.Fatalf("real replay=%+v", got)
	}
	detail, err := all.Store.Get(context.Background(), scope.App)
	if err != nil || detail.Version != 2 {
		t.Fatalf("definition=%+v %v", detail, err)
	}
	current, err := s.Get(context.Background(), scope, bridge.Empty{})
	if err != nil || current.Draft != nil {
		t.Fatalf("applied draft remains: %+v %v", current, err)
	}
}

func TestAskTargetsLastEditorAndHandsOverWords(t *testing.T) {
	s, all, scope, _, events := pipelineRig(t)
	// Record the last Editor using the real apply path.
	all.Shape = s
	c, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "advice"})
	if err != nil {
		t.Fatal(err)
	}
	_ = c
	waitShape(t, events, func(e bridge.ShapeEvent) bool { return e.Kind == "applied" })
	user := scope
	user.Caller = svc.Caller{Member: "raj", Role: svc.RoleUser}
	result, err := s.Ask(context.Background(), user, bridge.ShapeAskParams{Text: "add a renewal date column"})
	if err != nil || result.To != scope.Caller.Member {
		t.Fatalf("ask=%+v %v", result, err)
	}
	items := all.Approvals.(*fakeShapeApprovals).list()
	if len(items) != 1 || items[0].Title != `@raj asks for a change to CRM “add a renewal date column”` || items[0].Payload["action"] != "shape_this" || items[0].Payload["request"] != "add a renewal date column" {
		t.Fatalf("ask inbox=%+v", items)
	}
	if got, _ := s.Get(context.Background(), scope, bridge.Empty{}); got.Draft != nil {
		t.Fatal("ask started writing instead of handover")
	}
}

func TestPipelineQueueUsesPreviousPassedFiles(t *testing.T) {
	s, _, scope, shaper, events := pipelineRig(t)
	entered, release := make(chan struct{}), make(chan struct{})
	shaper.shape = func(in svc.ShapeInput) (svc.ShapeOutput, error) {
		if in.Request == "one" {
			close(entered)
			<-release
		}
		return svc.ShapeOutput{Files: spec.Files{"agent.md": append(in.Files["agent.md"], []byte("\n"+in.Request)...)}}, nil
	}
	_, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "one"})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	second, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "two"})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	waitChange(t, events, second.ID, "passed")
	calls := shaper.calls()
	if len(calls) != 2 || !strings.Contains(string(calls[1].Files["agent.md"]), "one") {
		t.Fatalf("queue did not compose files: %v", calls)
	}
}

func TestReplayNeverAppliesQuestion(t *testing.T) {
	s, all, scope, shaper, events := pipelineRig(t)
	shaper.shape = func(svc.ShapeInput) (svc.ShapeOutput, error) {
		return svc.ShapeOutput{Question: &bridge.ShapeQuestion{Text: "choose", Options: []bridge.ShapeQuestionOption{{ID: "stage", Label: "By stage"}, {ID: "drop", Label: "Drop this request"}}}}, nil
	}
	c, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "ambiguous"})
	if err != nil {
		t.Fatal(err)
	}
	waitShape(t, events, func(e bridge.ShapeEvent) bool { return e.Kind == "question" })
	_, err = s.Apply(context.Background(), scope, bridge.ShapeApplyParams{Base: 1})
	needCode(t, err, bridge.CodeConflict)
	select {
	case v := <-all.Shape.(*fakeShapeApply).applied:
		t.Fatal(fmt.Sprint(v))
	default:
	}
	got, _ := s.Get(context.Background(), scope, bridge.Empty{})
	if got.Draft.Changes[0].ID != c.ID {
		t.Fatal("question lost")
	}
}

func TestPersonalQueuedRequestsBecomeSeparateVersions(t *testing.T) {
	s, all, scope, shaper, events := pipelineRig(t)
	all.Shape = s
	entered, release := make(chan struct{}), make(chan struct{})
	shaper.shape = func(in svc.ShapeInput) (svc.ShapeOutput, error) {
		if in.Request == "first advice" {
			close(entered)
			<-release
		}
		return svc.ShapeOutput{Files: spec.Files{"agent.md": append(in.Files["agent.md"], []byte("\n"+in.Request)...)}}, nil
	}
	if _, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "first advice"}); err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "second advice"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	seen := waitShape(t, events, func(e bridge.ShapeEvent) bool { return e.Kind == "applied" && e.Version.N == 3 })
	versions := []int{}
	for _, e := range seen {
		if e.Kind == "applied" {
			versions = append(versions, e.Version.N)
		}
	}
	if len(versions) != 2 || versions[0] != 2 || versions[1] != 3 {
		t.Fatalf("queued personal versions=%v", versions)
	}
	live, err := all.Store.Definition(context.Background(), scope.App)
	if err != nil || !strings.Contains(string(live.Files["agent.md"]), "first advice") || !strings.Contains(string(live.Files["agent.md"]), "second advice") {
		t.Fatalf("queued changes lost: %+v %v", live, err)
	}
	rows, err := s.ListVersions(context.Background(), scope, bridge.Empty{})
	if err != nil || len(rows.Versions) != 3 || len(rows.Versions[0].Requests) != 1 || len(rows.Versions[1].Requests) != 1 {
		t.Fatalf("versions=%+v %v", rows, err)
	}
}

func TestPipelineFailedChangeCannotApply(t *testing.T) {
	s, all, scope, _, events := pipelineRig(t)
	all.Check = fakeShapeCheck{run: func(spec.Files, int) (bridge.CheckReport, error) {
		return bridge.CheckReport{Issues: []spec.Issue{{Message: "broken run"}}}, nil
	}}
	c, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "create invoice"})
	if err != nil {
		t.Fatal(err)
	}
	waitChange(t, events, c.ID, "failed")
	_, err = s.Apply(context.Background(), scope, bridge.ShapeApplyParams{Base: 1})
	needCode(t, err, bridge.CodeConflict)
	live, err := all.Store.Get(context.Background(), scope.App)
	if err != nil || live.Version != 1 {
		t.Fatalf("failed change published: %+v %v", live, err)
	}
}

func TestPipelineResumesCheckingJournalAfterRestart(t *testing.T) {
	s, all, scope, shaper, events := pipelineRig(t)
	// A checking entry is the durable queue; construct the stopped-server state
	// directly so the test needs neither a sleep nor an interrupted model call.
	s.mu.Lock()
	d := s.drafts[draftKey(scope)]
	err := d.AddChange(bridge.Change{ID: "resumed", Request: "advice after restart", State: "checking"}, nil)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(s.cfg, all)
	t.Cleanup(func() { waitWorkers(t, restarted) })
	all.Shape = &fakeShapeApply{Shape: restarted, s: restarted, migration: []bridge.MigrationLine{{Op: "add"}}, applied: make(chan int, 1)}
	if _, err := restarted.Get(context.Background(), scope, bridge.Empty{}); err != nil {
		t.Fatal(err)
	}
	waitChange(t, events, "resumed", "passed")
	if len(shaper.calls()) != 1 {
		t.Fatalf("resume calls=%d", len(shaper.calls()))
	}
}

func TestPipelineRenameRetainsAliasWithRealCheck(t *testing.T) {
	s, all, scope, shaper, events := pipelineRig(t)
	all.Shape = s
	all.Check = check.New(s.cfg, all)
	s.mu.Lock()
	d := s.drafts[draftKey(scope)]
	// The golden CRM starts with amount; use Value as this draft's base spelling.
	for name, data := range d.BaseFiles {
		d.BaseFiles[name] = bytes.ReplaceAll(data, []byte("amount"), []byte("value"))
	}
	d.BaseFiles[spec.AgentFile] = bytes.ReplaceAll(d.BaseFiles[spec.AgentFile], []byte("records.update,"), []byte("records.update, records.delete,"))
	versions, err := s.versionStore()
	if err == nil {
		_, err = versions.CommitShape(context.Background(), scope.App, 1, d.BaseFiles, []string{"fixture starts with Value"}, scope.Caller.Member)
	}
	if err == nil {
		live, loadErr := all.Store.Definition(context.Background(), scope.App)
		err = loadErr
		if err == nil {
			err = d.Rebase(live.App.Version, live.Files)
		}
	}
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	shaper.shape = func(in svc.ShapeInput) (svc.ShapeOutput, error) {
		files := cloneFiles(in.Files)
		for name, data := range files {
			files[name] = bytes.ReplaceAll(data, []byte(`"value"`), []byte(`"amount"`))
			files[name] = bytes.ReplaceAll(files[name], []byte("value *"), []byte("amount *"))
			files[name] = bytes.ReplaceAll(files[name], []byte("$input.value"), []byte("$input.amount"))
		}
		var app map[string]any
		if err := json.Unmarshal(files["app.json"], &app); err != nil {
			return svc.ShapeOutput{}, err
		}
		fields := app["records"].(map[string]any)["deal"].(map[string]any)["fields"].(map[string]any)
		fields["amount"].(map[string]any)["alias"] = "value"
		raw, marshalErr := json.Marshal(app)
		files["app.json"] = raw
		return svc.ShapeOutput{Files: files, Summary: "Amount replaces Value"}, marshalErr
	}
	c, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "rename Value to Amount"})
	if err != nil {
		t.Fatal(err)
	}
	seen := waitShape(t, events, func(e bridge.ShapeEvent) bool {
		return e.Kind == "change" && e.Change != nil && e.Change.ID == c.ID && (e.Change.State == "passed" || e.Change.State == "failed")
	})
	passed := seen[len(seen)-1].Change
	if !passed.Check.Pass || passed.Round != 1 || len(passed.Check.Rendered) != 3 {
		t.Fatalf("real Check=%+v", passed)
	}
	preview, err := s.Preview(context.Background(), scope, bridge.Empty{})
	if err != nil || len(preview.Migration) != 1 || preview.Migration[0].Op != "rename" || preview.Migration[0].Field != "value" || preview.Migration[0].To != "amount" {
		t.Fatalf("rename preview=%+v %v", preview, err)
	}
	got, err := s.Get(context.Background(), scope, bridge.Empty{})
	if err != nil || got.Draft == nil || got.Draft.State != "ready" {
		t.Fatalf("rename auto-applied: %+v %v", got, err)
	}
}

func TestAskApprovalYesCarriesShapeHandover(t *testing.T) {
	s, all, scope, _, _ := pipelineRig(t)
	all.Approvals = runs.NewApprovals(s.cfg, all)
	user := scope
	user.Caller = svc.Caller{Member: "raj", Role: svc.RoleUser}
	asked, err := s.Ask(context.Background(), user, bridge.ShapeAskParams{Text: "add a renewal date"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := all.Approvals.List(context.Background(), scope, bridge.ListApprovalsParams{})
	if err != nil || len(items.Approvals) != 1 {
		t.Fatalf("approvals=%+v %v", items, err)
	}
	editor := scope
	editor.Caller.Member = asked.To
	item, err := all.Approvals.Decide(context.Background(), editor, bridge.DecideApprovalParams{ApprovalID: items.Approvals[0].ID, Answer: "yes"})
	if err != nil || item.State != bridge.ApprovalApproved || item.Payload["action"] != "shape_this" || item.Payload["request"] != "add a renewal date" {
		t.Fatalf("handover=%+v %v", item, err)
	}
	got, err := s.Get(context.Background(), editor, bridge.Empty{})
	if err != nil || got.Draft == nil || len(got.Draft.Changes) != 0 {
		t.Fatalf("ask wrote without Editor sending: %+v %v", got, err)
	}
}
