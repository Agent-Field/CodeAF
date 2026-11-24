package serve

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A HOLD SAYS ITS REASON: an issue imported when the factory was set up and a
// new issue Pick up issues is off for both waited in Backlog reading the same
// bare "Backlog" (blackmac team run, #337). Each now names its hold and what
// releases it, on the queue a row is drawn from and on the job's own read; and
// turning Pick up issues on takes the pick-up words away, never the import's.
func TestABacklogHoldSaysItsReason(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	imported, err := f.st.Create(factory.Item{Title: "already open", Repo: "acme/api", Num: 7, URL: "https://github.com/acme/api/issues/7", Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "owner", SetupHeld: true})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := f.st.Create(factory.Item{Title: "agent-ready", Repo: "acme/api", Num: 8, URL: "https://github.com/acme/api/issues/8", Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "owner", Labels: []string{"agent-ready"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.st.Create(factory.Item{Title: "my own idea", Repo: "acme/api", Kind: factory.KindIssue, Origin: factory.OriginChat, State: factory.StateNew, Assignee: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	read := func() map[int]api.Job {
		var q api.Queue
		f.request(t, "GET", "/v1/queue", "operator-secret", nil, 200, &q)
		out := map[int]api.Job{}
		for _, j := range q.Jobs {
			out[j.Num] = j
			if j.Title == "my own idea" {
				out[-1] = j
			}
		}
		return out
	}
	jobs := read()
	if j := jobs[imported.Num]; j.Hold != api.HoldImport || j.StatePresentation.Title != "Held at import" || j.StatePresentation.Detail != api.HeldAtImportDetail {
		t.Fatalf("an issue held at import reads %q %q (hold %q)", j.StatePresentation.Title, j.StatePresentation.Detail, j.Hold)
	}
	if j := jobs[fresh.Num]; j.Hold != api.HoldPickUpOff || j.StatePresentation.Title != "Held: pick-up is off" || j.StatePresentation.Detail != api.PickUpOffDetail {
		t.Fatalf("a new issue with Pick up issues off reads %q %q (hold %q)", j.StatePresentation.Title, j.StatePresentation.Detail, j.Hold)
	}
	if j := jobs[-1]; j.Hold != "" || j.StatePresentation.Title != "Backlog" {
		t.Fatalf("a job a person made on the desk is not held by pick-up: %q (hold %q)", j.StatePresentation.Title, j.Hold)
	}
	var one api.Job
	f.request(t, "GET", "/v1/jobs/"+itoa(fresh.ID), "operator-secret", nil, 200, &one)
	if one.Hold != api.HoldPickUpOff || one.StatePresentation.Title != api.PickUpOffTitle {
		t.Fatalf("the job's own read lost its hold reason: %q (hold %q)", one.StatePresentation.Title, one.Hold)
	}

	if _, err := f.st.ChangeHouseRules(factory.RulesChange{ChangedBy: "owner", ChangedAt: ptrTime(time.Now())}, func(h *factory.HouseRules) error {
		h.StartOnItsOwn = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	jobs = read()
	if j := jobs[fresh.Num]; j.Hold != "" || j.StatePresentation.Title != "Backlog" {
		t.Fatalf("with Pick up issues on, the issue still reads %q (hold %q)", j.StatePresentation.Title, j.Hold)
	}
	if j := jobs[imported.Num]; j.Hold != api.HoldImport {
		t.Fatalf("Pick up issues released the import hold: %q", j.Hold)
	}
}

// A TEAM SERVER RUNS ITS REPOSITORIES' OWN START LABELS EITHER WAY, so its
// switch being off holds nothing and says nothing.
func TestATeamServerNamesNoPickUpHold(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	if _, err := f.st.Create(factory.Item{Title: "agent-ready", Repo: "acme/api", Num: 8, URL: "https://github.com/acme/api/issues/8", Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "owner"}); err != nil {
		t.Fatal(err)
	}
	var q api.Queue
	f.request(t, "GET", "/v1/queue", "operator-secret", nil, 200, &q)
	for _, j := range q.Jobs {
		if j.Hold != "" {
			t.Fatalf("a team server held %q for pick-up: %q", j.Title, j.StatePresentation.Title)
		}
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// THE RUNNER RUNS WITHOUT GITHUB (#2746), BUT THE POLL READS NOTHING: a
// Personal factory's queue says GitHub is not connected, so the Computers page
// and a job's row can say what waits on it instead of sitting idle in silence.
// Connecting it takes the word away; a team server, and a host that cannot
// tell, never say it.
func TestTheQueueSaysGitHubIsNotConnected(t *testing.T) {
	token := ""
	f := peopleV2Setup(t, api.FactorySolo)
	f.cfg.GitHubToken = func(context.Context) string { return token }
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	var q api.Queue
	f.request(t, "GET", "/v1/queue", "operator-secret", nil, 200, &q)
	if q.GitHub != api.GitHubNotConnected {
		t.Fatalf("a Personal factory with no GitHub reads github=%q", q.GitHub)
	}
	token = "gho_connected"
	q = api.Queue{}
	f.request(t, "GET", "/v1/queue", "operator-secret", nil, 200, &q)
	if q.GitHub != "" {
		t.Fatalf("with GitHub connected the queue still reads github=%q", q.GitHub)
	}

	team := peopleV2Setup(t, api.FactoryTeam)
	team.cfg.GitHubToken = func(context.Context) string { return "" }
	team.h = New(team.seam, team.cfg)
	t.Cleanup(team.h.Close)
	q = api.Queue{}
	team.request(t, "GET", "/v1/queue", "operator-secret", nil, 200, &q)
	if q.GitHub != "" {
		t.Fatalf("a team server reads github=%q", q.GitHub)
	}
	unknown := peopleV2Setup(t, api.FactorySolo)
	q = api.Queue{}
	unknown.request(t, "GET", "/v1/queue", "operator-secret", nil, 200, &q)
	if q.GitHub != "" {
		t.Fatalf("a host that cannot tell reads github=%q", q.GitHub)
	}
}
