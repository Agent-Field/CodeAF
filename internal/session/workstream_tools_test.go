package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

type workstreamFakeDoor struct {
	brief       WorkstreamBrief
	briefError  error
	read        json.RawMessage
	readAsk     WorkstreamReadAsk
	proposals   []WorkstreamProposal
	plan        WorkstreamPlanState
	reads       int
	saves       []WorkstreamPlanState
	bases       []int
	conflicts   int
	pageWrites  []WorkstreamPageWrite
	pageError   error
	otherCalls  int
	briefBound  bool
	briefExpiry time.Duration
}

func (d *workstreamFakeDoor) Brief(ctx context.Context) (WorkstreamBrief, error) {
	if deadline, ok := ctx.Deadline(); ok {
		d.briefBound = true
		d.briefExpiry = time.Until(deadline)
	}
	return d.brief, d.briefError
}

func (d *workstreamFakeDoor) Read(_ context.Context, ask WorkstreamReadAsk) (json.RawMessage, error) {
	d.otherCalls++
	d.readAsk = ask
	return d.read, nil
}

func (d *workstreamFakeDoor) Propose(_ context.Context, p WorkstreamProposal) (WorkstreamProposed, error) {
	d.otherCalls++
	d.proposals = append(d.proposals, p)
	return WorkstreamProposed{ID: "record-1"}, nil
}

func (d *workstreamFakeDoor) Plan(context.Context) (WorkstreamPlanState, error) {
	d.reads++
	return d.plan, nil
}

func (d *workstreamFakeDoor) SetPlan(_ context.Context, base int, p WorkstreamPlanState) (WorkstreamPlanState, error) {
	d.saves = append(d.saves, p)
	d.bases = append(d.bases, base)
	if len(d.saves) <= d.conflicts {
		d.plan.Version++
		d.plan.Waves = append(d.plan.Waves, workstream.Wave{N: 9, Title: "Person's wave", AutoStart: true, Items: []workstream.PlanItem{}})
		return WorkstreamPlanState{}, ErrWorkstreamVersion
	}
	d.plan = p
	d.plan.Version++
	return d.plan, nil
}

func (d *workstreamFakeDoor) WritePage(_ context.Context, p WorkstreamPageWrite) (WorkstreamPageWritten, error) {
	d.otherCalls++
	d.pageWrites = append(d.pageWrites, p)
	return WorkstreamPageWritten{ID: "page-1", Title: "Notes", Version: 2}, d.pageError
}

func workstreamToolForTest(t *testing.T, d *workstreamFakeDoor, name string) bare.Tool {
	t.Helper()
	a := &Agent{config: Config{Workstream: d}}
	now := func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }
	for _, tool := range a.workstreamToolsAt(now) {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("missing %s", name)
	return bare.Tool{}
}

func workstreamCallForTest(t *testing.T, d *workstreamFakeDoor, name, args string, refused bool) string {
	t.Helper()
	got, failed, err := workstreamToolForTest(t, d, name).Execute(context.Background(), json.RawMessage(args))
	if err != nil || failed != refused {
		t.Fatalf("%s: %q failed=%v err=%v", name, got, failed, err)
	}
	return got
}

// Contract (a) preserves the ordinary page and belt byte for byte.
func TestWorkstreamAgentToolsAndPromptAreScoped(t *testing.T) {
	cfg := Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	plain := &Agent{config: cfg}
	belt := plain.belt()
	page := renderSystemAt(cfg, now)
	d := &workstreamFakeDoor{brief: WorkstreamBrief{Name: "Usage", Why: "Explain recorded usage"}}
	cfg.Workstream = d
	with := &Agent{config: cfg}
	if len(with.belt()) != len(belt)+6 || len(with.workstreamTools()) != 6 {
		t.Fatal("the workstream must add exactly six tools")
	}
	if !reflect.DeepEqual(workstreamOrdinaryBelt(belt), workstreamOrdinaryBelt(with.belt())) {
		t.Fatal("the workstream changed the ordinary belt's bytes")
	}
	section := "\n\n" + workstreamPage(cfg)
	rendered := renderSystemAt(cfg, now)
	if !strings.Contains(rendered, `workstream "Usage"`) || !strings.Contains(rendered, "Explain recorded usage") || strings.Replace(rendered, section, "", 1) != page {
		t.Fatal("the gated section changed the ordinary page")
	}
	if !d.briefBound || d.briefExpiry > 2*time.Second {
		t.Fatalf("brief timeout: %v", d.briefExpiry)
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.InTask = true },
		func(c *Config) { c.Errand = true },
		func(c *Config) { c.taskControl = &TaskNode{spec: taskSpec{quick: &quickTaskSpec{reading: true}}} },
		func(c *Config) { c.Workstream = nil },
	} {
		child := cfg
		mutate(&child)
		a := &Agent{config: child}
		if len(a.workstreamTools()) != 0 || strings.Contains(renderSystemAt(child, now), "WORKSTREAM\n") {
			t.Fatal("a child or plain agent inherited the workstream")
		}
	}
	if workstreamWithoutPage(rendered) != page {
		t.Fatal("a child prompt retained the workstream section")
	}
	quick := &quickTaskSpec{system: rendered}
	quick.seed = append(quick.seed, textMessage("system", rendered))
	child := &Agent{}
	adoptSeed(child, quick)
	if child.system != promotedSystem(page) || messageContentText(quick.seed[0]) != rendered {
		t.Fatal("a promoted worker retained the workstream section or changed its parent's seed")
	}
	d.briefError = errors.New("not linked yet")
	if !strings.Contains(workstreamPage(cfg), "Read it with workstream_read before you answer.") {
		t.Fatal("the early prompt must remain usable")
	}
	d.briefError = nil
	d.brief = WorkstreamBrief{Name: strings.Repeat(`"`, 256), Why: strings.Repeat("why ", 1000)}
	if len(workstreamPage(cfg)) > 900 {
		t.Fatal("the workstream prompt section exceeded its budget")
	}
}

func workstreamOrdinaryBelt(tools []bare.Tool) []string {
	names := []string{"workstream_read", "propose_decision", "propose_question", "propose_jobs", "propose_goahead", "write_agent_page"}
	var out []string
	for _, tool := range tools {
		if !workstreamHasWord(names, tool.Name) {
			out = append(out, tool.Name+"\n"+tool.Description+"\n"+string(tool.Schema))
		}
	}
	return out
}

// Contract (b) appends only drafts and preserves an existing wave's settings.
func TestWorkstreamJobsAppendToExistingWave(t *testing.T) {
	d := &workstreamFakeDoor{plan: WorkstreamPlanState{Version: 3, Waves: []workstream.Wave{{N: 2, AutoStart: true, Items: []workstream.PlanItem{{Kind: "job", Factory: "f", Job: 7}}}}}}
	got := workstreamCallForTest(t, d, "propose_jobs", `{"wave":2,"jobs":[{"title":"One"},{"title":"Two"}]}`, false)
	wave := d.saves[0].Waves[0]
	if got != "Added 2 draft jobs to wave 2 of the plan. They are drafts: nothing runs until the person launches them." || !wave.AutoStart || len(wave.Items) != 3 || wave.Items[0].Job != 7 || wave.Items[1].DraftID == wave.Items[2].DraftID {
		t.Fatalf("appended wave: %q %+v", got, wave)
	}
}

// Contract (g) labels unknown readings and caps text without splitting UTF-8.
func TestWorkstreamReadCapsTextAndNamesUnknownNumbers(t *testing.T) {
	d := &workstreamFakeDoor{read: json.RawMessage(`{"name":"Usage"}`)}
	got := workstreamCallForTest(t, d, "workstream_read", `{"what":"workstream"}`, false)
	if got != "Numbers absent below are unknown; say so and never estimate them.\n"+string(d.read) {
		t.Fatalf("unknown numbers: %q", got)
	}
	d.read = json.RawMessage(`{"markdown":"` + strings.Repeat("é", 40000) + `"}`)
	got = workstreamCallForTest(t, d, "workstream_read", `{"what":"page","page_id":"p1"}`, false)
	if len(got) > 60000 || !strings.HasSuffix(got, "\nThe workstream read was cut.") || d.readAsk.PageID != "p1" {
		t.Fatalf("capped read: %d %+v", len(got), d.readAsk)
	}
}

// Contract (e) returns attribution; a page version conflict is refused without retry.
func TestWorkstreamPageWritesAndConflictsHaveExactAnswers(t *testing.T) {
	d := &workstreamFakeDoor{}
	got := workstreamCallForTest(t, d, "write_agent_page", `{"title":"Notes","markdown":"Words","updated_from":"Discussion"}`, false)
	if got != `Wrote Notes (page-1, version 2), marked as written by the agent from "Discussion".` || len(d.pageWrites) != 1 || d.pageWrites[0].Source != nil {
		t.Fatalf("write receipt: %q %+v", got, d.pageWrites)
	}
	d.pageError = ErrWorkstreamVersion
	got = workstreamCallForTest(t, d, "write_agent_page", `{"page_id":"p1","markdown":"Words","updated_from":"Discussion"}`, true)
	if got != "The page changed while you were writing. Read it again before writing." || len(d.pageWrites) != 2 {
		t.Fatalf("conflict receipt: %q %+v", got, d.pageWrites)
	}
}

// Contract (g) also applies to nested objects and fields for another operation.
func TestWorkstreamArgumentsRefuseNestedFactsAndScopeMixing(t *testing.T) {
	cases := []struct {
		name string
		args string
		key  string
	}{
		{"propose_decision", `{"text":"Words","source":{"kind":"url","id":"https://example.com","kept":true}}`, "kept"},
		{"propose_jobs", `{"jobs":[{"title":"Draft","job":7}]}`, "job"},
		{"propose_jobs", `{"jobs":[{"title":"Draft"}],"wave":1,"wave_title":"New"}`, "wave_title"},
		{"propose_jobs", `{"jobs":[{"title":"Draft"}],"wave":1,"milestone_id":"m1"}`, "milestone_id"},
		{"write_agent_page", `{"page_id":"p1","title":"Rename","markdown":"Words","updated_from":"Discussion"}`, "title"},
		{"write_agent_page", `{"page_id":"p1","parent_id":"p2","markdown":"Words","updated_from":"Discussion"}`, "parent_id"},
		{"propose_goahead", `{"scope":"wave","wave":1,"job":0,"cap_usd":1,"ends_on":"2026-11-04","why":"Words"}`, "job"},
		{"workstream_read", `{"what":"pages","page_id":"p1"}`, "page_id"},
		{"workstream_read", `{"what":"plan","state":"kept"}`, "state"},
		{"workstream_read", `{"what":"plan","kind":"decision"}`, "kind"},
	}
	for _, tc := range cases {
		d := &workstreamFakeDoor{}
		got := workstreamCallForTest(t, d, tc.name, tc.args, true)
		if !strings.Contains(got, tc.key) || d.otherCalls+d.reads+len(d.saves) != 0 {
			t.Fatalf("%s: %q %+v", tc.name, got, d)
		}
	}
}

// Contracts (b) and (g) bound proposed permission by the UTC day and dollar cap.
func TestWorkstreamGoAheadDatesAndCapsAreOnlyProposals(t *testing.T) {
	now := time.Date(2026, 11, 3, 23, 0, 0, 0, time.FixedZone("west", -5*60*60))
	base := WorkstreamGoAheadAsk{Scope: "job", Factory: "f", Job: 7, CapUSD: 10000, EndsOn: "2026-11-05", Why: "Review"}
	if err := WorkstreamValidateGoAhead(base, now); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*WorkstreamGoAheadAsk){
		func(a *WorkstreamGoAheadAsk) { a.CapUSD = 0 },
		func(a *WorkstreamGoAheadAsk) { a.CapUSD = 10000.01 },
		func(a *WorkstreamGoAheadAsk) { a.EndsOn = "2026-11-04" },
		func(a *WorkstreamGoAheadAsk) { a.EndsOn = "2027-02-03" },
		func(a *WorkstreamGoAheadAsk) { a.EndsOn = "2026-11-05T00:00:00Z" },
		func(a *WorkstreamGoAheadAsk) { a.Why = "" },
	} {
		ask := base
		mutate(&ask)
		if err := WorkstreamValidateGoAhead(ask, now); err == nil {
			t.Fatalf("accepted %+v", ask)
		}
	}
}

// Contract (b) exposes no way to keep a record or grant a go-ahead.
func TestWorkstreamProposalsAreOnlyWords(t *testing.T) {
	d := &workstreamFakeDoor{plan: WorkstreamPlanState{Waves: []workstream.Wave{{N: 2}}}}
	for _, kind := range []string{"decision", "question"} {
		got := workstreamCallForTest(t, d, "propose_"+kind, `{"text":"Keep this?"}`, false)
		want := "Proposed " + kind + " record-1. Nothing is kept until the person keeps it."
		if got != want || d.proposals[len(d.proposals)-1].Kind != kind || d.proposals[len(d.proposals)-1].Source != nil {
			t.Fatalf("proposal: %q %+v", got, d.proposals)
		}
	}
	got := workstreamCallForTest(t, d, "propose_goahead", `{"scope":"wave","wave":2,"cap_usd":12.5,"ends_on":"2026-11-04","why":"Review the result"}`, false)
	p := d.proposals[len(d.proposals)-1]
	if got != "Proposed a go-ahead as decision record-1. It grants nothing; the person grants it if they agree." || p.Kind != "decision" || p.Text != "Proposed go-ahead (not granted): wave 2, up to $12.50, until 2026-11-04. Review the result" {
		t.Fatalf("go-ahead: %q %+v", got, p)
	}
}

// Contract (f) rereads and reapplies once, preserving every existing plan field.
func TestWorkstreamJobsRetryOnlyOneVersionConflict(t *testing.T) {
	for _, conflicts := range []int{0, 1, 2} {
		d := &workstreamFakeDoor{conflicts: conflicts, plan: WorkstreamPlanState{Version: 4, Milestones: []workstream.Milestone{{ID: "m1", Title: "Ship"}}, Waves: []workstream.Wave{{N: 3, Title: "Existing", MilestoneID: "m1", AutoStart: true, Items: []workstream.PlanItem{{Kind: "job", Factory: "f", Job: 7}}}}}}
		before := d.plan
		got := workstreamCallForTest(t, d, "propose_jobs", `{"jobs":[{"title":"Draft","repo":"api","text":"Read first"}]}`, conflicts == 2)
		wantCalls := min(conflicts+1, 2)
		if len(d.saves) != wantCalls || d.reads != wantCalls || d.otherCalls != 0 {
			t.Fatalf("conflicts=%d calls=%+v", conflicts, d)
		}
		if !reflect.DeepEqual(d.saves[0].Waves[0], before.Waves[0]) || !reflect.DeepEqual(d.saves[0].Milestones, before.Milestones) {
			t.Fatal("existing plan changed")
		}
		last := d.saves[len(d.saves)-1]
		wave := last.Waves[len(last.Waves)-1]
		if wave.AutoStart || len(wave.Items) != 1 || wave.Items[0].Kind != "draft" || !strings.HasPrefix(wave.Items[0].DraftID, "draft_") {
			t.Fatalf("not drafts: %+v", wave)
		}
		if conflicts == 2 && got != "The plan changed while you were proposing; nothing was added. Read the plan and propose again." {
			t.Fatalf("second conflict: %q", got)
		}
		if conflicts == 1 && (wave.N != 10 || d.bases[1] != 5 || last.Waves[1].Title != "Person's wave") {
			t.Fatalf("did not reapply to the new plan: %+v", last)
		}
	}
}

// Contract (d) surfaces an ownership refusal without pretending to write.
func TestWorkstreamPageOwnershipRefusal(t *testing.T) {
	d := &workstreamFakeDoor{pageError: ErrWorkstreamNotAgentPage}
	got := workstreamCallForTest(t, d, "write_agent_page", `{"page_id":"page-1","markdown":"Words","updated_from":"Discussion"}`, true)
	if got != "That page is the person's. You write only your own pages; suggesting edits on theirs is not available yet." {
		t.Fatalf("refusal: %q", got)
	}
}

// Contract (g) refuses unknown fields before touching any door.
func TestWorkstreamToolsRefuseInventedFacts(t *testing.T) {
	names := []string{"workstream_read", "propose_decision", "propose_question", "propose_jobs", "propose_goahead", "write_agent_page"}
	fields := []string{"kept", "state", "cost_usd", "spend_usd", "count", "merged", "budget_usd", "auto_start", "version"}
	for _, name := range names {
		for _, field := range fields {
			if name == "workstream_read" && field == "state" {
				continue
			}
			d := &workstreamFakeDoor{}
			got := workstreamCallForTest(t, d, name, `{"`+field+`":true}`, true)
			if !strings.Contains(got, field) || d.otherCalls+d.reads+len(d.saves) != 0 {
				t.Fatalf("%s accepted %s: %q", name, field, got)
			}
		}
		var schema map[string]any
		if err := json.Unmarshal(workstreamToolForTest(t, &workstreamFakeDoor{}, name).Schema, &schema); err != nil || schema["additionalProperties"] != false {
			t.Fatalf("%s schema: %+v %v", name, schema, err)
		}
		properties := schema["properties"].(map[string]any)
		for _, field := range fields {
			if name == "workstream_read" && field == "state" {
				continue
			}
			if _, exists := properties[field]; exists {
				t.Fatalf("%s schema accepts invented %s", name, field)
			}
		}
	}
}

// Contract (h) assigns an action family to every workstream tool.
func TestWorkstreamToolActionCategories(t *testing.T) {
	for _, tool := range (&Agent{config: Config{Workstream: &workstreamFakeDoor{}}}).workstreamTools() {
		want := ActionCreate
		if tool.Name == "workstream_read" {
			want = ActionRead
		}
		if got := ActionCategoryForTool(tool.Name); got != want {
			t.Errorf("%s: %s, want %s", tool.Name, got, want)
		}
	}
}
