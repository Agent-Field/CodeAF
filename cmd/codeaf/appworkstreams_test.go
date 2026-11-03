package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func workstreamPtr[T any](value T) *T { return &value }

func workstreamBackend(t *testing.T) (*appBackend, *appRecorder, *int, appwire.Agent) {
	t.Helper()
	b, rec, origin, _ := collectionsBackend(t)
	calls := new(int)
	b.workstreams.createAgent = func(ask appwire.CreateAsk) (appwire.Agent, error) {
		*calls++
		if ask.Text != "" || ask.Workspace != "" || ask.CreatedBy != "workstream" || len(ask.Attachments) != 0 {
			t.Errorf("agent must be a bare default-workspace open: %+v", ask)
		}
		return appwire.Agent{ID: fmt.Sprintf("workstream-agent-%d", *calls)}, nil
	}
	b.workstreams.spend = func(string) *float64 { return workstreamPtr(0.0) }
	b.workstreams.queue = func(context.Context, string) (api.Queue, error) {
		return api.Queue{}, appwire.Errorf(appwire.CodeNotFound, "no Factory attached")
	}
	return b, rec, calls, origin
}

func workstreamCreateForTest(t *testing.T, b *appBackend, name string, refs ...appwire.WorkstreamRef) appwire.Workstream {
	t.Helper()
	v, err := b.WorkstreamCreate(appwire.WorkstreamCreateParams{Name: name, Refs: refs})
	if err != nil {
		t.Fatal(err)
	}
	return v.Workstream
}
func workstreamJobRef(number string) appwire.WorkstreamRef {
	return appwire.WorkstreamRef{Kind: appwire.RefFactoryJob, ID: number, Factory: workstreamPtr("f")}
}
func workstreamEvents(rec *appRecorder) []appwire.WorkstreamChanged {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := []appwire.WorkstreamChanged{}
	for _, data := range rec.data {
		if event, ok := data.(appwire.WorkstreamChanged); ok {
			out = append(out, event)
		}
	}
	return out
}
func workstreamCheckError(t *testing.T, err error, code string) *appwire.Error {
	t.Helper()
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
	return wire
}

// (c): the public create/list/page/collection interfaces expose one identity,
// one Overview and one agent, including simultaneous retries with one client id.
func TestAppWorkstreamCreateIsIdempotent(t *testing.T) {
	b, rec, calls, origin := workstreamBackend(t)
	ask := appwire.WorkstreamCreateParams{Name: "Launch", OriginChatID: &origin.ID, ClientID: workstreamPtr("create-one"), Model: workstreamPtr("test-model")}
	originalCreate := b.workstreams.createAgent
	b.workstreams.createAgent = func(p appwire.CreateAsk) (appwire.Agent, error) {
		if p.Model != "test-model" {
			t.Errorf("model = %q", p.Model)
		}
		return originalCreate(p)
	}
	var wg sync.WaitGroup
	results := make(chan appwire.WorkstreamCreateResult, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		guard.Go("app/workstream-create-test", func() {
			defer wg.Done()
			v, err := b.WorkstreamCreate(ask)
			results <- v
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var id, agentID string
	for v := range results {
		if id == "" {
			id, agentID = v.Workstream.ID, v.Workstream.AgentID
		}
		if v.Workstream.ID != id || v.Workstream.AgentID != agentID || v.Workstream.Stage != "shaping" {
			t.Fatalf("create: %+v", v)
		}
	}
	if *calls != 1 {
		t.Fatalf("created %d agents", *calls)
	}
	rows, err := b.WorkstreamList(appwire.WorkstreamListParams{})
	if err != nil || len(rows.Workstreams) != 1 {
		t.Fatalf("rows %+v, %v", rows, err)
	}
	collections, err := b.ListCollections(nil, "", 200)
	if err != nil || len(collections.Collections) != 1 || collections.Collections[0].ID != id || !collections.Collections[0].Workstream {
		t.Fatalf("collections %+v, %v", collections, err)
	}
	pages, err := b.WorkstreamPageList(appwire.WorkstreamPageListParams{WorkstreamID: id})
	if err != nil || len(pages.Pages) != 1 || pages.Pages[0].Title != "Overview" || pages.Pages[0].Version != 1 {
		t.Fatalf("pages %+v, %v", pages, err)
	}
	opened, err := b.OpenCollection(id, 1, 200, "")
	if err != nil || len(opened.Members) != 2 {
		t.Fatalf("members %+v, %v", opened, err)
	}
	ids := []string{}
	for _, node := range opened.Members {
		ids = append(ids, node.ID)
	}
	if !slices.Contains(ids, origin.ID) || !slices.Contains(ids, agentID) {
		t.Fatalf("chat members: %v", ids)
	}
	if len(workstreamEvents(rec)) != 1 {
		t.Fatalf("retry emitted: %+v", workstreamEvents(rec))
	}
}

// (b): both doors refuse the whole request and retain both timestamps and all
// memberships. A plain collection remains free to reference the same job.
func TestAppWorkstreamOwnershipRefusal(t *testing.T) {
	b, rec, calls, _ := workstreamBackend(t)
	a := workstreamCreateForTest(t, b, "Owner", workstreamJobRef("7"))
	other := workstreamCreateForTest(t, b, "Other")
	beforeEvents := len(workstreamEvents(rec))
	beforeCalls := *calls
	refs := []appwire.Member{{Ref: appwire.Ref{Kind: appwire.RefConversation, ID: "extra"}}, {Ref: appwire.Ref{Kind: appwire.RefFactoryJob, ID: "7", Factory: "f"}}}
	_, err := b.AddToCollection(other.ID, refs)
	wire := workstreamCheckError(t, err, appwire.CodeInOtherWorkstream)
	want := appwire.WorkstreamTaken{Factory: "f", Job: 7, WorkstreamID: a.ID, WorkstreamName: a.Name}
	if !reflect.DeepEqual(wire.Detail, want) {
		t.Fatalf("detail %+v", wire.Detail)
	}
	_, err = b.WorkstreamCreate(appwire.WorkstreamCreateParams{Name: "Refused", Refs: []appwire.WorkstreamRef{{Kind: appwire.RefConversation, ID: "extra"}, workstreamJobRef("7")}})
	wire = workstreamCheckError(t, err, appwire.CodeInOtherWorkstream)
	if !reflect.DeepEqual(wire.Detail, want) || *calls != beforeCalls {
		t.Fatalf("preflight: %+v, %d agents", wire, *calls)
	}
	for _, before := range []appwire.Workstream{a, other} {
		after, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: before.ID})
		if err != nil || after.Workstream.UpdatedAt != before.UpdatedAt || after.Workstream.Total != before.Total {
			t.Fatalf("refusal changed %+v: %+v, %v", before, after, err)
		}
	}
	opened, err := b.OpenCollection(other.ID, 1, 200, "")
	if err != nil || len(opened.Members) != 1 {
		t.Fatalf("partial membership: %+v, %v", opened, err)
	}
	if len(workstreamEvents(rec)) != beforeEvents {
		t.Fatal("refusal emitted a workstream event")
	}
	plain, err := b.DuplicateCollection(a.ID, "Plain copy")
	if err != nil || plain.Workstream {
		t.Fatalf("duplicate %+v, %v", plain, err)
	}
	if _, err := b.AddToCollection(plain.ID, refs); err != nil {
		t.Fatal(err)
	}
	wire = workstreamCheckError(t, b.DeleteCollection(a.ID), appwire.CodeBadParams)
	if wire.Message != "mark a workstream done instead" {
		t.Fatal(wire)
	}
}

// (d), (j): job state comes from recorded Factory data; an observed transition
// is persisted and announced once. Issue numbers cannot alias document ids.
func TestAppWorkstreamStageAndMeasuredNumbers(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	state := "backlog"
	reads := 0
	b.workstreams.queue = func(context.Context, string) (api.Queue, error) {
		reads++
		job := api.Job{ID: 7, Num: 1107, State: state, Cost: 0, Title: "Ship", Repo: "acme/api"}
		if state == "done" {
			at := time.Now()
			job.PR = &api.PR{MergedAt: &at}
		}
		return api.Queue{Jobs: []api.Job{job}}, nil
	}
	w := workstreamCreateForTest(t, b, "Stages", workstreamJobRef("7"))
	got, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || got.Workstream.Stage != "shaping" || got.Workstream.SpendUSD == nil || *got.Workstream.SpendUSD != 0 {
		t.Fatalf("backlog %+v, %v", got, err)
	}
	encoded, err := json.Marshal(got)
	if err != nil || !bytes.Contains(encoded, []byte(`"spend_usd":0`)) || !bytes.Contains(encoded, []byte(`"merged":0`)) || !bytes.Contains(encoded, []byte(`"needs_you":0`)) {
		t.Fatalf("measured zero omitted: %s, %v", encoded, err)
	}
	before := len(workstreamEvents(rec))
	state = "queued"
	func() {
		b.workstreams.cacheMu.Lock()
		defer b.workstreams.cacheMu.Unlock()
		b.workstreams.cache = nil
	}()
	got, err = b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || got.Workstream.Stage != "building" || got.Workstream.StartedAt.IsZero() {
		t.Fatalf("queued %+v, %v", got, err)
	}
	if len(workstreamEvents(rec)) != before+1 {
		t.Fatal("first observation did not emit exactly once")
	}
	got, err = b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || len(workstreamEvents(rec)) != before+1 || reads != 2 {
		t.Fatalf("cached read: %+v, %v, reads %d", got, err, reads)
	}
	state = "done"
	b.workstreams.cache = nil
	got, err = b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || got.Workstream.Stage != "building" || got.Workstream.Merged == nil || *got.Workstream.Merged != 1 {
		t.Fatalf("all merged %+v, %v", got, err)
	}
	done, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: w.ID})
	if err != nil || done.Workstream.Stage != "done" {
		t.Fatalf("done %+v, %v", done, err)
	}
	state = "backlog"
	b.workstreams.cache = nil
	reopened, err := b.WorkstreamReopen(appwire.WorkstreamReopenParams{ID: w.ID})
	if err != nil || reopened.Workstream.Stage != "building" {
		t.Fatalf("reopen %+v, %v", reopened, err)
	}
}

// (j): mutation results retain the same measured job and agent numbers as get.
func TestAppWorkstreamMutationNumbersMatchGet(t *testing.T) {
	for _, method := range []string{"update", "mark_done", "reopen", "mark_done_again"} {
		t.Run(method, func(t *testing.T) {
			b, rec, _, _ := workstreamBackend(t)
			reads := 0
			mergedAt := time.Now()
			b.workstreams.queue = func(context.Context, string) (api.Queue, error) {
				reads++
				return api.Queue{Jobs: []api.Job{{ID: 7, Num: 1107, State: "needs_you", Cost: 1.25, PR: &api.PR{MergedAt: &mergedAt}}}}, nil
			}
			b.workstreams.spend = func(string) *float64 { return workstreamPtr(2.5) }
			w := workstreamCreateForTest(t, b, "Mutation numbers", workstreamJobRef("7"))
			if _, err := b.WorkstreamRecordPropose(appwire.WorkstreamRecordProposeParams{WorkstreamID: w.ID, Kind: "question", Text: "Ready?"}); err != nil {
				t.Fatal(err)
			}
			before := w
			if method == "reopen" || method == "mark_done_again" {
				done, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: w.ID, HowItWent: workstreamPtr("First report")})
				if err != nil {
					t.Fatal(err)
				}
				before = done.Workstream
			}
			events := len(workstreamEvents(rec))
			var result appwire.Workstream
			switch method {
			case "update":
				v, err := b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID, Patch: appwire.WorkstreamPatch{Why: workstreamPtr("Updated")}})
				result = v.Workstream
				if err != nil {
					t.Fatal(err)
				}
			case "mark_done", "mark_done_again":
				v, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: w.ID, HowItWent: workstreamPtr("New report")})
				result = v.Workstream
				if err != nil {
					t.Fatal(err)
				}
			case "reopen":
				v, err := b.WorkstreamReopen(appwire.WorkstreamReopenParams{ID: w.ID})
				result = v.Workstream
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
			if err != nil || !reflect.DeepEqual(result, got.Workstream) {
				t.Fatalf("mutation %+v differs from get %+v: %v", result, got.Workstream, err)
			}
			if result.Merged == nil || *result.Merged != 1 || result.NeedsYou == nil || *result.NeedsYou != 2 || result.SpendUSD == nil || *result.SpendUSD != 3.75 {
				t.Fatalf("measured numbers: %+v", result)
			}
			if reads != 1 {
				t.Fatalf("queue reads = %d, want one cached read", reads)
			}
			if method == "reopen" && result.Stage != "building" {
				t.Fatalf("reopen stage = %q", result.Stage)
			}
			if method == "mark_done_again" && (!reflect.DeepEqual(result, before) || len(workstreamEvents(rec)) != events) {
				t.Fatal("marking an already done workstream changed it or emitted an event")
			}
		})
	}
}

// (d), (i), (j): a mutation observes the first running job before any get,
// publishes one combined receipt, and keeps that start through Factory failure.
func TestAppWorkstreamMutationPersistsFirstStart(t *testing.T) {
	for _, method := range []string{"update", "mark_done", "reopen"} {
		t.Run(method, func(t *testing.T) {
			b, rec, _, _ := workstreamBackend(t)
			var clock atomic.Int64
			clock.Store(time.Now().UnixMilli())
			b.now = func() time.Time { return time.UnixMilli(clock.Load()) }
			reads := 0
			b.workstreams.queue = func(context.Context, string) (api.Queue, error) {
				reads++
				if reads > 1 {
					return api.Queue{}, errors.New("unreachable")
				}
				return api.Queue{Jobs: []api.Job{{ID: 7, State: "running", Cost: 1.25}}}, nil
			}
			w := workstreamCreateForTest(t, b, "First start", workstreamJobRef("7"))
			events := len(workstreamEvents(rec))
			var result appwire.Workstream
			switch method {
			case "update":
				v, err := b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID, Patch: appwire.WorkstreamPatch{Why: workstreamPtr("Updated before get")}})
				if err != nil {
					t.Fatal(err)
				}
				result = v.Workstream
			case "mark_done":
				v, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: w.ID})
				if err != nil {
					t.Fatal(err)
				}
				result = v.Workstream
			case "reopen":
				// Reopening an already open workstream still observes its jobs.
				v, err := b.WorkstreamReopen(appwire.WorkstreamReopenParams{ID: w.ID})
				if err != nil {
					t.Fatal(err)
				}
				result = v.Workstream
			}
			stage := "building"
			if method == "mark_done" {
				stage = "done"
			}
			if result.Stage != stage || result.StartedAt.IsZero() || !result.UpdatedAt.After(w.UpdatedAt) {
				t.Fatalf("first observation: %+v", result)
			}
			changed := workstreamEvents(rec)[events:]
			if len(changed) != 1 || changed[0].ID != w.ID || !reflect.DeepEqual(changed[0].Parts, []string{"workstream"}) {
				t.Fatalf("mutation events: %+v", changed)
			}
			got, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
			if err != nil || !reflect.DeepEqual(result, got.Workstream) || len(workstreamEvents(rec)) != events+1 || reads != 1 {
				t.Fatalf("get after mutation: %+v, %v; events %d, reads %d", got, err, len(workstreamEvents(rec))-events, reads)
			}
			clock.Add(workstreamFactoryFresh.Milliseconds())
			got, err = b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
			if err != nil || got.Workstream.Stage != stage || !got.Workstream.StartedAt.Equal(result.StartedAt) || !got.Workstream.UpdatedAt.Equal(result.UpdatedAt) || got.Workstream.Merged != nil || got.Workstream.NeedsYou != nil || got.Workstream.SpendUSD != nil || reads != 2 || len(workstreamEvents(rec)) != events+1 {
				t.Fatalf("unreachable Factory lost durable observation: %+v, %v; reads %d", got, err, reads)
			}
			updated, err := b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID, Patch: appwire.WorkstreamPatch{Why: workstreamPtr("Committed while unreachable")}})
			if err != nil || updated.Workstream.Why != "Committed while unreachable" || updated.Workstream.Stage != stage || updated.Workstream.SpendUSD != nil || len(workstreamEvents(rec)) != events+2 {
				t.Fatalf("unreachable Factory failed mutation: %+v, %v", updated, err)
			}
		})
	}
}

// (e): marking done ends grants of all three scopes and archives member chats
// through the existing archive door, excluding the workstream's agent record.
func TestAppWorkstreamDoneEndsGoAheadsAndArchivesChats(t *testing.T) {
	b, _, _, origin := workstreamBackend(t)
	made, err := b.WorkstreamCreate(appwire.WorkstreamCreateParams{Name: "Done", OriginChatID: &origin.ID, Refs: []appwire.WorkstreamRef{workstreamJobRef("7"), {Kind: appwire.RefConversation, ID: "second-chat"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := made.Workstream.ID
	_, err = b.WorkstreamPlanSet(appwire.WorkstreamPlanSetParams{ID: id, Milestones: []appwire.WorkstreamMilestone{{ID: "m", Title: "Ship"}}, Waves: []appwire.WorkstreamWave{{N: 1, MilestoneID: workstreamPtr("m"), Items: []appwire.WorkstreamPlanItem{}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []appwire.WorkstreamGoAheadGrantParams{
		{Scope: "job", Factory: workstreamPtr("f"), Job: workstreamPtr(7)},
		{Scope: "wave", Wave: workstreamPtr(1)},
		{Scope: "milestone", MilestoneID: workstreamPtr("m")},
	} {
		p.WorkstreamID = id
		p.CapUSD = 40
		p.EndsAt = time.Now().Add(time.Hour)
		if _, err := b.WorkstreamGoAheadGrant(p); err != nil {
			t.Fatal(err)
		}
	}
	var archived []string
	b.workstreams.archiveChats = func(ids []string) error { archived = append(archived, ids...); return nil }
	if _, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: id, ArchiveChats: workstreamPtr(true)}); err != nil {
		t.Fatal(err)
	}
	grants, err := b.WorkstreamGoAheadList(appwire.WorkstreamGoAheadListParams{WorkstreamID: id, Ended: workstreamPtr(true)})
	if err != nil || len(grants.GoAheads) != 3 {
		t.Fatalf("grants %+v, %v", grants, err)
	}
	for _, g := range grants.GoAheads {
		if g.State != "ended" || workstreamValue(g.EndReason) != "workstream_done" || g.EndedAt.IsZero() {
			t.Fatalf("unended %+v", g)
		}
	}
	if slices.Contains(archived, made.Workstream.AgentID) || len(archived) != 2 || !slices.Contains(archived, origin.ID) || !slices.Contains(archived, "second-chat") {
		t.Fatalf("archived %v", archived)
	}
}

// (f): stale saves preserve the document and history; restore creates a new
// version instead of modifying an old one, with complete typed stale detail.
func TestAppWorkstreamPageVersions(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Pages")
	pages, err := b.WorkstreamPageList(appwire.WorkstreamPageListParams{WorkstreamID: w.ID})
	if err != nil {
		t.Fatal(err)
	}
	id := pages.Pages[0].ID
	doc := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Saved"}]}]}`)
	saved, err := b.WorkstreamPageSave(appwire.WorkstreamPageSaveParams{ID: id, BaseVersion: 1, Doc: doc})
	if err != nil || saved.Page.Version != 2 {
		t.Fatalf("save %+v, %v", saved, err)
	}
	before := len(workstreamEvents(rec))
	_, err = b.WorkstreamPageSave(appwire.WorkstreamPageSaveParams{ID: id, BaseVersion: 1, Doc: json.RawMessage(`{}`)})
	wire := workstreamCheckError(t, err, appwire.CodeVersionConflict)
	detail, ok := wire.Detail.(appwire.WorkstreamVersionConflict)
	if !ok || detail.ID != id || detail.Version != 2 || detail.UpdatedAt != saved.Page.UpdatedAt || detail.UpdatedBy != "you" {
		t.Fatalf("detail %+v", wire.Detail)
	}
	read, err := b.WorkstreamPageGet(appwire.WorkstreamPageGetParams{ID: id, Markdown: workstreamPtr(true)})
	if err != nil || !bytes.Equal(read.Page.Doc, doc) || read.Page.Markdown == nil || !strings.Contains(*read.Page.Markdown, "Saved") {
		t.Fatalf("read %+v, %v", read, err)
	}
	history, err := b.WorkstreamPageHistory(appwire.WorkstreamPageHistoryParams{ID: id})
	if err != nil || len(history.Versions) != 2 || history.Versions[0].Version != 2 {
		t.Fatalf("history %+v, %v", history, err)
	}
	if len(workstreamEvents(rec)) != before {
		t.Fatal("stale save or reads emitted")
	}
	restored, err := b.WorkstreamPageRestore(appwire.WorkstreamPageRestoreParams{ID: id, Version: workstreamPtr(1), BaseVersion: workstreamPtr(2)})
	if err != nil || restored.Page.Version != 3 || string(restored.Page.Doc) != `{"type":"doc","content":[]}` {
		t.Fatalf("restore %+v, %v", restored, err)
	}
	history, err = b.WorkstreamPageHistory(appwire.WorkstreamPageHistoryParams{ID: id})
	if err != nil || len(history.Versions) != 3 || history.Versions[0].Version != 3 {
		t.Fatalf("restored history %+v, %v", history, err)
	}
	_, err = b.WorkstreamPageMove(appwire.WorkstreamPageMoveParams{ID: id})
	workstreamCheckError(t, err, appwire.CodeBadParams)
}

// (g): keep/drop support undo; a replacement links both records and closes
// every later mutation of the superseded record with conflict.
func TestAppWorkstreamRecordLifecycle(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Records")
	proposed, err := b.WorkstreamRecordPropose(appwire.WorkstreamRecordProposeParams{WorkstreamID: w.ID, Kind: "question", Text: "Ship?", Source: &appwire.WorkstreamSource{Kind: "chat", ID: "chat"}})
	if err != nil || proposed.Record.State != "proposed" || proposed.Record.By != "agent" {
		t.Fatalf("propose %+v, %v", proposed, err)
	}
	id := proposed.Record.ID
	kept, err := b.WorkstreamRecordKeep(appwire.WorkstreamRecordKeepParams{ID: id})
	if err != nil || kept.Record.State != "kept" {
		t.Fatalf("keep %+v, %v", kept, err)
	}
	dropped, err := b.WorkstreamRecordDrop(appwire.WorkstreamRecordDropParams{ID: id})
	if err != nil || dropped.Record.State != "dropped" {
		t.Fatalf("drop %+v, %v", dropped, err)
	}
	if _, err := b.WorkstreamRecordKeep(appwire.WorkstreamRecordKeepParams{ID: id}); err != nil {
		t.Fatal(err)
	}
	replacement, err := b.WorkstreamRecordSupersede(appwire.WorkstreamRecordSupersedeParams{ID: id, Text: "Ship it", Kind: workstreamPtr("decision")})
	if err != nil || replacement.Record.State != "kept" || replacement.Record.Kind != "decision" || workstreamValue(replacement.Record.Supersedes) != id || replacement.Superseded.State != "superseded" || workstreamValue(replacement.Superseded.SupersededBy) != replacement.Record.ID {
		t.Fatalf("supersede %+v, %v", replacement, err)
	}
	_, err = b.WorkstreamRecordKeep(appwire.WorkstreamRecordKeepParams{ID: id})
	workstreamCheckError(t, err, appwire.CodeConflict)
	_, err = b.WorkstreamRecordDrop(appwire.WorkstreamRecordDropParams{ID: id})
	workstreamCheckError(t, err, appwire.CodeConflict)
	_, err = b.WorkstreamRecordSupersede(appwire.WorkstreamRecordSupersedeParams{ID: id, Text: "Again"})
	workstreamCheckError(t, err, appwire.CodeConflict)
}

// (h): missing scope, cap or end date, and clock-dependent invalid ends, store
// nothing and emit nothing, through the backend's own checked entry point.
func TestAppWorkstreamInvalidGoAheads(t *testing.T) {
	for _, name := range []string{"scope", "cap", "end", "past", "too far", "too much"} {
		t.Run(name, func(t *testing.T) {
			b, rec, _, _ := workstreamBackend(t)
			w := workstreamCreateForTest(t, b, "Invalid", workstreamJobRef("7"))
			p := appwire.WorkstreamGoAheadGrantParams{WorkstreamID: w.ID, Scope: "job", Factory: workstreamPtr("f"), Job: workstreamPtr(7), CapUSD: 1, EndsAt: time.Now().Add(time.Hour)}
			switch name {
			case "scope":
				p.Scope = ""
			case "cap":
				p.CapUSD = 0
			case "end":
				p.EndsAt = time.Time{}
			case "past":
				p.EndsAt = time.Now().Add(-time.Second)
			case "too far":
				p.EndsAt = time.Now().Add(91 * 24 * time.Hour)
			case "too much":
				p.CapUSD = 10001
			}
			before := len(workstreamEvents(rec))
			_, err := b.WorkstreamGoAheadGrant(p)
			workstreamCheckError(t, err, appwire.CodeBadParams)
			list, err := b.WorkstreamGoAheadList(appwire.WorkstreamGoAheadListParams{WorkstreamID: w.ID, Ended: workstreamPtr(true)})
			if err != nil || len(list.GoAheads) != 0 || len(workstreamEvents(rec)) != before {
				t.Fatalf("invalid grant stored or emitted: %+v, %v", list, err)
			}
		})
	}
}

// (j): encode the actual handler results so absence cannot pass as null or 0.
func TestAppWorkstreamUnknownNumbersAreAbsent(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Unknown", workstreamJobRef("7"))
	for _, method := range []string{"get", "list"} {
		var result any
		var err error
		if method == "get" {
			result, err = b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
		} else {
			result, err = b.WorkstreamList(appwire.WorkstreamListParams{})
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{`"merged":`, `"needs_you":`, `"spend_usd":`} {
			if bytes.Contains(data, []byte(field)) {
				t.Fatalf("%s fabricated %s: %s", method, field, data)
			}
		}
		if !bytes.Contains(data, []byte(`"total":1`)) {
			t.Fatalf("total absent: %s", data)
		}
	}
	opened, err := b.OpenCollection(w.ID, 1, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range opened.Members {
		if node.Kind == appwire.RefFactoryJob && node.Target.State != appwire.TargetUnknown {
			t.Fatalf("unattached Factory fabricated target state: %+v", node.Target)
		}
	}
}

func TestAppWorkstreamCreatePreflightAndCleanup(t *testing.T) {
	for _, name := range []string{"name", "origin", "ref", "bounds"} {
		t.Run(name, func(t *testing.T) {
			b, rec, calls, _ := workstreamBackend(t)
			p := appwire.WorkstreamCreateParams{Name: "Refused"}
			code := appwire.CodeBadParams
			switch name {
			case "name":
				if _, err := b.CreateCollection("Refused"); err != nil {
					t.Fatal(err)
				}
				code = appwire.CodeConflict
			case "origin":
				p.OriginChatID = workstreamPtr("no-chat")
				code = appwire.CodeNotFound
			case "ref":
				p.Refs = []appwire.WorkstreamRef{{Kind: appwire.RefCollection, ID: "no-collection"}}
				code = appwire.CodeNotFound
			case "bounds":
				p.Why = workstreamPtr(strings.Repeat("x", 4001))
			}
			before := len(workstreamEvents(rec))
			_, err := b.WorkstreamCreate(p)
			workstreamCheckError(t, err, code)
			if *calls != 0 || len(workstreamEvents(rec)) != before {
				t.Fatal("preflight created or emitted")
			}
			if name != "name" {
				if _, err := os.Stat(filepath.Dir(b.collectionsPath)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("preflight created store folder: %v", err)
				}
			}
		})
	}
	t.Run("peer name race closes unused agent", func(t *testing.T) {
		b, rec, calls, _ := workstreamBackend(t)
		create := b.workstreams.createAgent
		b.workstreams.createAgent = func(p appwire.CreateAsk) (appwire.Agent, error) {
			if _, err := b.CreateCollection("Raced"); err != nil {
				return appwire.Agent{}, err
			}
			return create(p)
		}
		closed := ""
		b.workstreams.closeAgent = func(id string) error { closed = id; return nil }
		_, err := b.WorkstreamCreate(appwire.WorkstreamCreateParams{Name: "Raced"})
		workstreamCheckError(t, err, appwire.CodeConflict)
		list, err := b.WorkstreamList(appwire.WorkstreamListParams{})
		if err != nil || len(list.Workstreams) != 0 || *calls != 1 || closed != "workstream-agent-1" || len(workstreamEvents(rec)) != 0 {
			t.Fatalf("cleanup: %+v, %v, closed %q", list, err, closed)
		}
	})
	t.Run("agent refusal creates no store", func(t *testing.T) {
		b, rec, _, _ := workstreamBackend(t)
		b.workstreams.createAgent = func(appwire.CreateAsk) (appwire.Agent, error) {
			return appwire.Agent{}, appwire.Errorf(appwire.CodeBadParams, "agent refused")
		}
		_, err := b.WorkstreamCreate(appwire.WorkstreamCreateParams{Name: "Refused"})
		workstreamCheckError(t, err, appwire.CodeBadParams)
		if _, err := os.Stat(filepath.Dir(b.collectionsPath)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("agent refusal created store folder: %v", err)
		}
		if len(workstreamEvents(rec)) != 0 {
			t.Fatal("agent refusal emitted")
		}
	})
}

// A fresh workstream list remains a non-migrating, non-creating read.
func TestAppWorkstreamFreshReadsDoNotCreate(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	list, err := b.WorkstreamList(appwire.WorkstreamListParams{})
	if err != nil || list.Workstreams == nil || len(list.Workstreams) != 0 {
		t.Fatalf("fresh list %+v, %v", list, err)
	}
	_, err = b.WorkstreamGet(appwire.WorkstreamGetParams{ID: "missing"})
	workstreamCheckError(t, err, appwire.CodeNotFound)
	if _, err := os.Stat(filepath.Dir(b.collectionsPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fresh read made store folder: %v", err)
	}
}

// The shared hello hook must recognize the real backend's capability.
func TestAppWorkstreamHelloCarriesFeature(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	var output bytes.Buffer
	server := appwire.NewServer(&output)
	if err := server.Serve(context.Background(), strings.NewReader(""), appwire.Hello{}, workstreamHelloEngine{Engine: b, Workstreamer: b}); err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Event string        `json:"event"`
		Data  appwire.Hello `json:"data"`
	}
	if err := json.NewDecoder(&output).Decode(&frame); err != nil {
		t.Fatal(err)
	}
	if frame.Event != appwire.EventHello || !slices.Contains(frame.Data.Features, appwire.FeatureWorkstreams) {
		t.Fatalf("hello %+v", frame)
	}
}

// Only the tested capability is exposed, leaving the real backend's unrelated
// process warm-up outside this protocol test.
type workstreamHelloEngine struct {
	appwire.Engine
	appwire.Workstreamer
}

// Session metadata is the recorded spend source, including a fresh bare open
// excluded from ordinary agents.list. Missing/corrupt metadata stays unknown.
func TestAppWorkstreamRecordedAgentSpend(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Spend")
	b.workstreams.spend = nil
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	a := b.agent(w.AgentID)
	b.locked(func() { a.transcript = transcript })
	if err := os.WriteFile(transcript, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID}); err != nil || got.Workstream.SpendUSD != nil {
		t.Fatalf("missing metadata: %+v, %v", got, err)
	}
	for _, cost := range []float64{0, 2.5} {
		meta := session.Meta{ID: w.AgentID, SpentUSD: cost}
		if cost > 0 {
			meta.LastUserAt = time.Now()
		}
		data, err := json.Marshal(meta)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		got, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
		if err != nil || got.Workstream.SpendUSD == nil || *got.Workstream.SpendUSD != cost {
			t.Fatalf("recorded %v, got %+v, %v", cost, got, err)
		}
	}
}

// (i): every mutation is observed through its public handler and produces one
// event for the changed workstream, containing every part its write changed.
func TestAppWorkstreamEveryWriteEmitsOnce(t *testing.T) {
	type fixture struct {
		b                               *appBackend
		id, page, parent, record, grant string
		base                            int
	}
	cases := []struct {
		method string
		parts  []string
		done   bool
		call   func(*fixture) error
	}{
		{"workstream.create", []string{"workstream", "pages", "jobs"}, false, func(f *fixture) error {
			v, err := f.b.WorkstreamCreate(appwire.WorkstreamCreateParams{Name: "Second"})
			f.id = v.Workstream.ID
			return err
		}},
		{"workstream.update", []string{"workstream"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: f.id, Patch: appwire.WorkstreamPatch{Why: workstreamPtr("New why")}})
			return err
		}},
		{"workstream.mark_done", []string{"workstream", "goaheads"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: f.id})
			return err
		}},
		{"workstream.reopen", []string{"workstream"}, true, func(f *fixture) error {
			_, err := f.b.WorkstreamReopen(appwire.WorkstreamReopenParams{ID: f.id})
			return err
		}},
		{"workstream.plan_set", []string{"workstream", "plan"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamPlanSet(appwire.WorkstreamPlanSetParams{ID: f.id, Milestones: []appwire.WorkstreamMilestone{}, Waves: []appwire.WorkstreamWave{}})
			return err
		}},
		{"page.create", []string{"workstream", "pages"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamPageCreate(appwire.WorkstreamPageCreateParams{WorkstreamID: f.id, Title: "New"})
			return err
		}},
		{"page.save", []string{"workstream", "pages"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamPageSave(appwire.WorkstreamPageSaveParams{ID: f.page, BaseVersion: f.base, Doc: json.RawMessage(`{"type":"doc","content":[]}`)})
			return err
		}},
		{"page.update", []string{"workstream", "pages"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamPageUpdate(appwire.WorkstreamPageUpdateParams{ID: f.page, Title: workstreamPtr("Renamed")})
			return err
		}},
		{"page.move", []string{"workstream", "pages"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamPageMove(appwire.WorkstreamPageMoveParams{ID: f.page})
			return err
		}},
		{"page.delete", []string{"workstream", "pages"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamPageDelete(appwire.WorkstreamPageDeleteParams{ID: f.parent})
			return err
		}},
		{"page.restore", []string{"workstream", "pages"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamPageRestore(appwire.WorkstreamPageRestoreParams{ID: f.page, Version: workstreamPtr(1), BaseVersion: workstreamPtr(f.base)})
			return err
		}},
		{"record.propose", []string{"workstream", "records"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamRecordPropose(appwire.WorkstreamRecordProposeParams{WorkstreamID: f.id, Kind: "decision", Text: "New"})
			return err
		}},
		{"record.keep", []string{"workstream", "records"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamRecordKeep(appwire.WorkstreamRecordKeepParams{ID: f.record})
			return err
		}},
		{"record.drop", []string{"workstream", "records"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamRecordDrop(appwire.WorkstreamRecordDropParams{ID: f.record})
			return err
		}},
		{"record.supersede", []string{"workstream", "records"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamRecordSupersede(appwire.WorkstreamRecordSupersedeParams{ID: f.record, Text: "Replaced"})
			return err
		}},
		{"goahead.grant", []string{"workstream", "goaheads"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamGoAheadGrant(appwire.WorkstreamGoAheadGrantParams{WorkstreamID: f.id, Scope: "job", Factory: workstreamPtr("f"), Job: workstreamPtr(7), CapUSD: 2, EndsAt: time.Now().Add(time.Hour)})
			return err
		}},
		{"goahead.revoke", []string{"workstream", "goaheads"}, false, func(f *fixture) error {
			_, err := f.b.WorkstreamGoAheadRevoke(appwire.WorkstreamGoAheadRevokeParams{ID: f.grant})
			return err
		}},
		{"collections.add", []string{"workstream", "jobs"}, false, func(f *fixture) error {
			_, err := f.b.AddToCollection(f.id, []appwire.Member{{Ref: appwire.Ref{Kind: appwire.RefFactoryJob, ID: "8", Factory: "f"}}})
			return err
		}},
		{"collections.remove", []string{"workstream", "jobs"}, false, func(f *fixture) error {
			_, err := f.b.RemoveFromCollection(f.id, []appwire.Ref{{Kind: appwire.RefFactoryJob, ID: "7", Factory: "f"}})
			return err
		}},
		{"collections.rename", []string{"workstream"}, false, func(f *fixture) error { _, err := f.b.RenameCollection(f.id, "Renamed"); return err }},
		{"collections.relink", []string{"workstream", "jobs"}, false, func(f *fixture) error {
			_, err := f.b.RelinkCollection(f.id, appwire.Ref{Kind: appwire.RefFactoryJob, ID: "7", Factory: "f"}, appwire.Ref{Kind: appwire.RefFactoryJob, ID: "8", Factory: "f"})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			b, rec, _, _ := workstreamBackend(t)
			b.workstreams.queue = func(context.Context, string) (api.Queue, error) {
				return api.Queue{Jobs: []api.Job{{ID: 7, State: "backlog"}, {ID: 8, State: "backlog"}}}, nil
			}
			w := workstreamCreateForTest(t, b, "Events", workstreamJobRef("7"))
			parent, err := b.WorkstreamPageCreate(appwire.WorkstreamPageCreateParams{WorkstreamID: w.ID, Title: "Parent"})
			if err != nil {
				t.Fatal(err)
			}
			page, err := b.WorkstreamPageCreate(appwire.WorkstreamPageCreateParams{WorkstreamID: w.ID, ParentID: &parent.Page.ID, Title: "Child"})
			if err != nil {
				t.Fatal(err)
			}
			saved, err := b.WorkstreamPageSave(appwire.WorkstreamPageSaveParams{ID: page.Page.ID, BaseVersion: 1, Doc: json.RawMessage(`{"type":"doc"}`)})
			if err != nil {
				t.Fatal(err)
			}
			record, err := b.WorkstreamRecordPropose(appwire.WorkstreamRecordProposeParams{WorkstreamID: w.ID, Kind: "question", Text: "What?"})
			if err != nil {
				t.Fatal(err)
			}
			grant, err := b.WorkstreamGoAheadGrant(appwire.WorkstreamGoAheadGrantParams{WorkstreamID: w.ID, Scope: "job", Factory: workstreamPtr("f"), Job: workstreamPtr(7), CapUSD: 1, EndsAt: time.Now().Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			f := fixture{b: b, id: w.ID, page: page.Page.ID, parent: parent.Page.ID, record: record.Record.ID, grant: grant.GoAhead.ID, base: saved.Page.Version}
			if tc.done {
				if _, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: w.ID}); err != nil {
					t.Fatal(err)
				}
			}
			before := len(workstreamEvents(rec))
			collectionsBefore := len(changedEvents(rec))
			if err := tc.call(&f); err != nil {
				t.Fatal(err)
			}
			events := workstreamEvents(rec)[before:]
			if len(events) != 1 || events[0].ID != f.id || !reflect.DeepEqual(events[0].Parts, tc.parts) {
				t.Fatalf("%s: %+v, want %s %v", tc.method, events, f.id, tc.parts)
			}
			if strings.HasPrefix(tc.method, "collections.") && len(changedEvents(rec)) != collectionsBefore+1 {
				t.Fatal("membership change lost collection.changed")
			}
		})
	}
}

// (i): the read methods and idempotent mutations never manufacture a receipt.
func TestAppWorkstreamReadsAndUnchangedWritesEmitNothing(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Quiet", workstreamJobRef("7"))
	b.workstreams.queue = func(context.Context, string) (api.Queue, error) {
		return api.Queue{Jobs: []api.Job{{ID: 7, State: "backlog"}}}, nil
	}
	pages, err := b.WorkstreamPageList(appwire.WorkstreamPageListParams{WorkstreamID: w.ID})
	if err != nil {
		t.Fatal(err)
	}
	page := pages.Pages[0]
	r, err := b.WorkstreamRecordPropose(appwire.WorkstreamRecordProposeParams{WorkstreamID: w.ID, Kind: "decision", Text: "Keep", Kept: workstreamPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	g, err := b.WorkstreamGoAheadGrant(appwire.WorkstreamGoAheadGrantParams{WorkstreamID: w.ID, Scope: "job", Factory: workstreamPtr("f"), Job: workstreamPtr(7), CapUSD: 1, EndsAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.WorkstreamGoAheadRevoke(appwire.WorkstreamGoAheadRevokeParams{ID: g.GoAhead.ID}); err != nil {
		t.Fatal(err)
	}
	calls := []struct {
		name string
		call func() error
	}{
		{"workstream.list", func() error { _, err := b.WorkstreamList(appwire.WorkstreamListParams{}); return err }},
		{"workstream.get", func() error { _, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID}); return err }},
		{"workstream.plan_get", func() error { _, err := b.WorkstreamPlanGet(appwire.WorkstreamPlanGetParams{ID: w.ID}); return err }},
		{"page.list", func() error {
			_, err := b.WorkstreamPageList(appwire.WorkstreamPageListParams{WorkstreamID: w.ID})
			return err
		}},
		{"page.get", func() error { _, err := b.WorkstreamPageGet(appwire.WorkstreamPageGetParams{ID: page.ID}); return err }},
		{"page.history", func() error {
			_, err := b.WorkstreamPageHistory(appwire.WorkstreamPageHistoryParams{ID: page.ID})
			return err
		}},
		{"record.list", func() error {
			_, err := b.WorkstreamRecordList(appwire.WorkstreamRecordListParams{WorkstreamID: w.ID})
			return err
		}},
		{"goahead.list", func() error {
			_, err := b.WorkstreamGoAheadList(appwire.WorkstreamGoAheadListParams{WorkstreamID: w.ID, Ended: workstreamPtr(true)})
			return err
		}},
		{"workstream.update empty", func() error { _, err := b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID}); return err }},
		{"workstream.reopen open", func() error { _, err := b.WorkstreamReopen(appwire.WorkstreamReopenParams{ID: w.ID}); return err }},
		{"page.update same", func() error {
			_, err := b.WorkstreamPageUpdate(appwire.WorkstreamPageUpdateParams{ID: page.ID, Title: &page.Title})
			return err
		}},
		{"record.keep kept", func() error {
			_, err := b.WorkstreamRecordKeep(appwire.WorkstreamRecordKeepParams{ID: r.Record.ID})
			return err
		}},
		{"goahead.revoke ended", func() error {
			_, err := b.WorkstreamGoAheadRevoke(appwire.WorkstreamGoAheadRevokeParams{ID: g.GoAhead.ID})
			return err
		}},
		{"collections.add already", func() error {
			_, err := b.AddToCollection(w.ID, []appwire.Member{{Ref: appwire.Ref{Kind: appwire.RefFactoryJob, ID: "7", Factory: "f"}}})
			return err
		}},
		{"collections.remove absent", func() error {
			_, err := b.RemoveFromCollection(w.ID, []appwire.Ref{{Kind: appwire.RefFactoryJob, ID: "99", Factory: "f"}})
			return err
		}},
		{"collections.rename same", func() error { _, err := b.RenameCollection(w.ID, w.Name); return err }},
	}
	before := len(workstreamEvents(rec))
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err != nil {
				t.Fatal(err)
			}
			if len(workstreamEvents(rec)) != before {
				t.Fatal("unchanged operation emitted")
			}
		})
	}
	if _, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: w.ID}); err != nil {
		t.Fatal(err)
	}
	before = len(workstreamEvents(rec))
	if _, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: w.ID}); err != nil {
		t.Fatal(err)
	}
	if len(workstreamEvents(rec)) != before {
		t.Fatal("mark_done again emitted")
	}
}

// B3: distinct factories are read concurrently, once per list, and reused by
// get and collections.open. A barrier tests work rather than elapsed time.
func TestAppWorkstreamFactoryBatchCacheAndCollectionTargets(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	a := workstreamCreateForTest(t, b, "One", workstreamJobRef("7"))
	second := workstreamJobRef("9")
	second.Factory = workstreamPtr("g")
	workstreamCreateForTest(t, b, "Two", second)
	entered := make(chan string, 2)
	release := make(chan struct{})
	var mu sync.Mutex
	calls := map[string]int{}
	workstreamRecordCall := func(factory string) {
		mu.Lock()
		defer mu.Unlock()
		calls[factory]++
	}
	b.workstreams.queue = func(ctx context.Context, factory string) (api.Queue, error) {
		workstreamRecordCall(factory)
		entered <- factory
		select {
		case <-release:
		case <-ctx.Done():
			return api.Queue{}, ctx.Err()
		}
		id := 7
		if factory == "g" {
			id = 9
		}
		return api.Queue{Jobs: []api.Job{{ID: id, Num: 1107, Title: "Recorded title", State: "backlog", Repo: "acme/api", Cost: 0}}}, nil
	}
	done := make(chan error, 1)
	guard.Go("app/workstream-list-test", func() { _, err := b.WorkstreamList(appwire.WorkstreamListParams{}); done <- err })
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("factories were not read concurrently")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: a.ID}); err != nil {
		t.Fatal(err)
	}
	open, err := b.OpenCollection(a.ID, 1, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range open.Members {
		if node.Kind == appwire.RefFactoryJob {
			found = true
			if node.Target.State != appwire.TargetLive || node.Target.Title != "Recorded title" || node.Target.Status != "backlog" || node.Target.Repo != "acme/api" {
				t.Fatalf("target %+v", node.Target)
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !found || calls["f"] != 1 || calls["g"] != 1 {
		t.Fatalf("target found %v, queue calls %v", found, calls)
	}
}

// workstreamQueueWaitContext signals when a coalesced reader selects on Done,
// so the panic cannot race ahead of the waiter entering the flight branch.
type workstreamQueueWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *workstreamQueueWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

// (j), B3: a guarded queue panic releases concurrent waiters, caches an
// unavailable reading for the normal window, and permits a successful retry.
func TestAppWorkstreamFactoryPanicFinishesFlight(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	b.now = func() time.Time { return time.UnixMilli(clock.Load()) }
	w := workstreamCreateForTest(t, b, "Queue panic", workstreamJobRef("7"))
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var calls atomic.Int32
	b.workstreams.queue = func(ctx context.Context, _ string) (api.Queue, error) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
				panic("queue reader fault")
			case <-ctx.Done():
				return api.Queue{}, ctx.Err()
			}
		}
		return api.Queue{Jobs: []api.Job{{ID: 7, State: "backlog", Cost: 1.25}}}, nil
	}
	b.workstreams.job = func(context.Context, string, int) (api.Job, error) {
		t.Error("unavailable queue triggered a job fallback")
		return api.Job{}, errors.New("unexpected job fallback")
	}
	type workstreamGetRead struct {
		result appwire.WorkstreamGetResult
		err    error
	}
	owner := make(chan workstreamGetRead, 1)
	guard.Go("app/workstream-panic-owner-test", func() {
		result, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
		owner <- workstreamGetRead{result: result, err: err}
	})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("queue reader did not enter")
	}
	_, flight, owns := b.workstreams.workstreamQueueStart("f", b.now())
	if flight == nil || owns {
		t.Fatal("queue reader did not own a shared flight")
	}
	ctx, cancel := context.WithTimeout(context.Background(), workstreamFactoryTimeout)
	defer cancel()
	waitCtx := &workstreamQueueWaitContext{Context: ctx, waiting: make(chan struct{})}
	waiter := make(chan workstreamQueueRead, 1)
	guard.Go("app/workstream-panic-waiter-test", func() { waiter <- b.workstreamQueue(waitCtx, "f") })
	select {
	case <-waitCtx.waiting:
	case <-time.After(time.Second):
		t.Fatal("concurrent reader did not wait on the flight")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case read := <-owner:
		if read.err != nil {
			t.Fatal(read.err)
		}
		encoded, err := json.Marshal(read.result)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"merged", "needs_you", "spend_usd"} {
			if bytes.Contains(encoded, []byte(`"`+key+`"`)) {
				t.Fatalf("panic exposed a number in %s", encoded)
			}
		}
		if read.result.Workstream.Total != 1 {
			t.Fatalf("panic lost total: %+v", read.result)
		}
	case <-time.After(time.Second):
		t.Fatal("guarded queue owner did not finish")
	}
	select {
	case <-flight:
	default:
		t.Fatal("panic did not close the flight")
	}
	select {
	case read := <-waiter:
		if read.err == nil || errors.Is(read.err, context.DeadlineExceeded) || !read.at.Equal(b.now()) {
			t.Fatalf("waiter did not receive the cached failure: %+v", read)
		}
	case <-time.After(time.Second):
		t.Fatal("panic stranded the concurrent waiter")
	}
	func() {
		b.workstreams.cacheMu.Lock()
		defer b.workstreams.cacheMu.Unlock()
		if _, ok := b.workstreams.flights["f"]; ok {
			t.Fatal("panic left an in-flight entry")
		}
	}()
	entry, flight, owns := b.workstreams.workstreamQueueStart("f", b.now())
	if entry.err == nil || flight != nil || owns || calls.Load() != 1 {
		t.Fatalf("panic did not remove and cache the failed flight: %+v, %v, %v", entry, flight, owns)
	}
	clock.Add((workstreamFactoryFresh - time.Millisecond).Milliseconds())
	got, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || got.Workstream.Merged != nil || got.Workstream.NeedsYou != nil || got.Workstream.SpendUSD != nil || calls.Load() != 1 {
		t.Fatalf("failure did not stay cached: %+v, %v; calls %d", got, err, calls.Load())
	}
	clock.Add(1)
	got, err = b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || calls.Load() != 2 || got.Workstream.Merged == nil || *got.Workstream.Merged != 0 || got.Workstream.NeedsYou == nil || *got.Workstream.NeedsYou != 0 || got.Workstream.SpendUSD == nil || *got.Workstream.SpendUSD != 1.25 {
		t.Fatalf("retry after failure-cache expiry: %+v, %v; calls %d", got, err, calls.Load())
	}
}

// B3: only successful queue reads allow missing-job fallbacks, each distinct
// address is asked once, and work over the per-call allowance remains unknown.
func TestAppWorkstreamMissingJobReadsAreBounded(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	refs := []appwire.WorkstreamRef{}
	for i := 1; i <= 25; i++ {
		refs = append(refs, workstreamJobRef(fmt.Sprint(i)))
	}
	w := workstreamCreateForTest(t, b, "Missing", refs...)
	queuedReads, jobReads := 0, 0
	b.workstreams.queue = func(ctx context.Context, _ string) (api.Queue, error) {
		queuedReads++
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > workstreamFactoryTimeout {
			t.Error("queue read has no bounded deadline")
		}
		return api.Queue{}, nil
	}
	b.workstreams.job = func(ctx context.Context, _ string, job int) (api.Job, error) {
		jobReads++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("job read has no deadline")
		}
		return api.Job{ID: job, Num: 999, State: "backlog", Cost: 0}, nil
	}
	got, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || queuedReads != 1 || jobReads != 20 || got.Workstream.Total != 25 || got.Workstream.Merged != nil || got.Workstream.NeedsYou != nil || got.Workstream.SpendUSD != nil {
		t.Fatalf("bounded fallback %+v, %v; queue %d jobs %d", got, err, queuedReads, jobReads)
	}
	b.workstreams.cache = nil
	jobReads = 0
	b.workstreams.queue = func(context.Context, string) (api.Queue, error) { return api.Queue{}, errors.New("unreachable") }
	if _, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID}); err != nil {
		t.Fatal(err)
	}
	if jobReads != 0 {
		t.Fatal("unreachable queue triggered per-job reads")
	}
	open, err := b.OpenCollection(w.ID, 1, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range open.Members {
		if node.Kind == appwire.RefFactoryJob && (node.Target.State != appwire.TargetUnknown || node.Target.Status != "unknown") {
			t.Fatalf("unavailable target %+v", node.Target)
		}
	}
}

func TestAppWorkstreamPlanConflictDetailAndDoneRefusal(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Plan")
	p := appwire.WorkstreamPlanSetParams{ID: w.ID, Milestones: []appwire.WorkstreamMilestone{}, Waves: []appwire.WorkstreamWave{}}
	first, err := b.WorkstreamPlanSet(p)
	if err != nil {
		t.Fatal(err)
	}
	before := len(workstreamEvents(rec))
	_, err = b.WorkstreamPlanSet(p)
	wire := workstreamCheckError(t, err, appwire.CodeVersionConflict)
	detail, ok := wire.Detail.(appwire.WorkstreamVersionConflict)
	if !ok || detail.ID != w.ID || detail.Version != 1 || detail.UpdatedAt != first.Plan.UpdatedAt || detail.UpdatedBy != "you" {
		t.Fatalf("plan conflict %+v", wire)
	}
	if len(workstreamEvents(rec)) != before {
		t.Fatal("stale plan emitted")
	}
	if _, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: w.ID}); err != nil {
		t.Fatal(err)
	}
	p.BaseVersion = 1
	_, err = b.WorkstreamPlanSet(p)
	workstreamCheckError(t, err, appwire.CodeConflict)
}

// (c): a bare workstream agent survives the existing cleanup path and can be
// reopened after workstream.get restores it into a fresh backend's inventory.
func TestAppWorkstreamBareAgentSurvivesRestart(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	b, _, _, _ := workstreamBackend(t)
	b.collectionsPath = home.Join("v3", "collections.db")
	b.workstreams.spend = nil
	w := workstreamCreateForTest(t, b, "Persistent")
	dir := filepath.Join(session.PlacesRoot(), "project", w.AgentID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(transcript, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{ID: w.AgentID, Workspace: t.TempDir(), Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	a := b.agent(w.AgentID)
	b.locked(func() { a.transcript = transcript; a.minted = true })
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	restarted, _, _, _ := workstreamBackend(t)
	restarted.collectionsPath = home.Join("v3", "collections.db")
	restarted.workstreams.spend = nil
	got, err := restarted.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || got.Workstream.SpendUSD == nil || *got.Workstream.SpendUSD != 0 {
		t.Fatalf("restore %+v, %v", got, err)
	}
	if _, err := restarted.OpenAgent(w.AgentID); err != nil {
		t.Fatalf("agent could not be reopened: %v", err)
	}
	listed, err := restarted.ListAgents("")
	if err != nil {
		t.Fatal(err)
	}
	if _, found := listsAgent(listed, w.AgentID); found {
		t.Fatal("restoring the bare agent changed agents.list")
	}
}

// (i): chat archiving is an external owner action after the committed done
// transaction. Its failure cannot hide that change or duplicate it on retry.
func TestAppWorkstreamDoneArchiveFailureStillAnnouncesCommit(t *testing.T) {
	b, rec, _, origin := workstreamBackend(t)
	made, err := b.WorkstreamCreate(appwire.WorkstreamCreateParams{Name: "Archive retry", OriginChatID: &origin.ID})
	if err != nil {
		t.Fatal(err)
	}
	b.workstreams.archiveChats = func([]string) error { return appwire.Errorf(appwire.CodeBusy, "chat is busy") }
	p := appwire.WorkstreamMarkDoneParams{ID: made.Workstream.ID, ArchiveChats: workstreamPtr(true)}
	before := len(workstreamEvents(rec))
	_, err = b.WorkstreamMarkDone(p)
	workstreamCheckError(t, err, appwire.CodeBusy)
	events := workstreamEvents(rec)
	if len(events) != before+1 || events[before].ID != made.Workstream.ID {
		t.Fatalf("lost done notification: %+v", events)
	}
	got, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: made.Workstream.ID})
	if err != nil || got.Workstream.Stage != "done" {
		t.Fatalf("done not committed: %+v, %v", got, err)
	}
	b.workstreams.archiveChats = func([]string) error { return nil }
	if _, err := b.WorkstreamMarkDone(p); err != nil {
		t.Fatal(err)
	}
	if len(workstreamEvents(rec)) != before+1 {
		t.Fatal("archive retry duplicated workstream event")
	}
}
