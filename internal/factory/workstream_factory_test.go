package factory

import (
	"testing"
	"time"
)

// WHICH WORKSTREAM'S FACTORY SWITCH TAKES A NEW ISSUE: one naming the
// issue's repository before one naming none, never one naming only other
// repositories, the oldest among equals, and never before its switch went on
// or while archived.
func TestTheFactorySwitchTakesAnIssueIntoTheNearestWorkstream(t *testing.T) {
	on := time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)
	rules := func(factoryOn bool) *WorkstreamRules {
		r := DefaultRules("priya")
		r.KeepsRunning.Factory = factoryOn
		if factoryOn {
			r.KeepsRunning.FactorySince = &on
		}
		return &r
	}
	repo := func(ref string) []WorkstreamSource { return []WorkstreamSource{{ID: "s", Kind: "repo", Ref: ref}} }
	archived := on
	all := OrgWorkstreams{
		"ws_any_new": {ID: "ws_any_new", Rules: rules(true), CreatedAt: on.Add(time.Hour)},
		"ws_any_old": {ID: "ws_any_old", Rules: rules(true), CreatedAt: on},
		"ws_other":   {ID: "ws_other", Rules: rules(true), Sources: repo("acme/web"), CreatedAt: on.Add(-time.Hour)},
		"ws_off":     {ID: "ws_off", Rules: rules(false), Sources: repo("acme/api")},
		"ws_gone":    {ID: "ws_gone", Rules: rules(true), Sources: repo("acme/api"), ArchivedAt: &archived},
	}
	issue := Item{Origin: OriginForge, Repo: "acme/api", Num: 3, Kind: KindIssue, Created: on.Add(time.Minute)}
	if id, ok := FactoryWorkstreamFor(all, issue); !ok || id != "ws_any_old" {
		t.Fatalf("with no workstream naming acme/api, the oldest naming none takes it: %q %v", id, ok)
	}
	all["ws_named"] = OrgWorkstream{ID: "ws_named", Rules: rules(true), Sources: repo("Acme/API"), CreatedAt: on.Add(2 * time.Hour)}
	if id, _ := FactoryWorkstreamFor(all, issue); id != "ws_named" {
		t.Fatalf("the workstream naming the repository takes it first: %q", id)
	}
	early := issue
	early.Created = on.Add(-time.Minute)
	if id, ok := FactoryWorkstreamFor(all, early); ok {
		t.Fatalf("an issue opened before the switch went on was taken by %q", id)
	}
	web := issue
	web.Repo = "acme/web"
	if id, _ := FactoryWorkstreamFor(all, web); id != "ws_other" {
		t.Fatalf("acme/web goes to the workstream naming it: %q", id)
	}
}

// The server owns the switch's time: kept while on, set when turned on,
// dropped when off, whatever a writer sends.
func TestTheFactorySwitchTimeIsTheServers(t *testing.T) {
	now := time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)
	forged := now.Add(-time.Hour)
	next := WorkstreamRules{KeepsRunning: KeepsRunning{Factory: true, FactorySince: &forged}}
	StampFactorySince(WorkstreamRules{}, &next, now)
	if next.KeepsRunning.FactorySince == nil || !next.KeepsRunning.FactorySince.Equal(now) {
		t.Fatalf("turned on: %v", next.KeepsRunning.FactorySince)
	}
	later := WorkstreamRules{KeepsRunning: KeepsRunning{Factory: true}}
	StampFactorySince(next, &later, now.Add(time.Hour))
	if !later.KeepsRunning.FactorySince.Equal(now) {
		t.Fatalf("kept on: %v", later.KeepsRunning.FactorySince)
	}
	off := WorkstreamRules{KeepsRunning: KeepsRunning{FactorySince: &now}}
	StampFactorySince(later, &off, now)
	if off.KeepsRunning.FactorySince != nil {
		t.Fatal("a switch that is off keeps a time")
	}
}

// A FACTORY TASK WHOSE PUBLISH FAILED SAYS WHY IN ITS ASK: the job's own
// sentence (connect GitHub, and where), and Retry publishing as its yes. It
// was a bare "Go on with #333?" that hid the reason.
func TestAFailedPublishTellsTheTaskWhatIsNeeded(t *testing.T) {
	said := "Connect GitHub to publish: the pull request is opened from @priya's own GitHub account, and the computer that ran this job is connected to GitHub as @santosh. Connect GitHub as @priya in Settings › Connectors on that computer, or have a teammate who signs in to the organization with GitHub take the job, then Retry publishing."
	it := Item{ID: 79, Repo: "acme/api", Num: 333, Kind: KindIssue, State: StateLanded, QKind: QKindApprove, Question: "Approve the proof?",
		PublishError: said, PublishStatus: &Publishing{State: "failed", Code: "github_other_account", Attempt: 1, At: time.Unix(100, 0)}}
	if NeedsKind(it) != NeedsPublishFailed {
		t.Fatalf("fixture is not a failed publish: %q", NeedsKind(it))
	}
	ask := JobTaskAsk(it)
	if ask == nil || ask.Title != "Connect GitHub to publish "+it.Ref() || ask.Detail != said {
		t.Fatalf("ask = %+v", ask)
	}
	if len(ask.Options) != 2 || ask.Options[0].ID != "yes" || ask.Options[0].Label != "Retry publishing" {
		t.Fatalf("options = %+v", ask.Options)
	}
}
