package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// L-LOOP2 C1, C6: a real proposal join establishes its own assignment for
// workers and checks below it, under either kind of run root.
func TestJoinedProposalWorkersReadTheirOwnAssignmentsAsk(t *testing.T) {
	for _, proposedRoot := range []bool{false, true} {
		t.Run(map[bool]string{false: "typed root", true: "proposal root"}[proposedRoot], func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", "bash")
			double := newBeltRunDouble("finished")
			registerBeltRunEngine(t, double)
			completer := &routedCompleter{parent: []step{
				proposeCall("Create red.txt", "ROOT_ONLY_ASSIGNMENT: write red.txt"),
				finalText("handed off"),
			}}
			agent, _ := newTestAgent(t, completer, func(c *Config) {
				c.Workspace = newTestRepo(t)
				c.Place = Place{Dir: t.TempDir()}
				c.ProfileDir = t.TempDir()
				c.AskConsent = false
				c.TaskAutoApproveSeconds = 0
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if proposedRoot {
				events, err := agent.Submit(ctx, "propose_task: Create red.txt")
				if err != nil {
					t.Fatal(err)
				}
				collect(t, events)
			} else if _, _, _, err := agent.StartTask(ctx, "ROOT_ONLY_ASSIGNMENT: Create red.txt; PERSON_ONLY_FORBIDDEN", false); err != nil {
				t.Fatal(err)
			}
			select {
			case <-double.entered:
			case <-ctx.Done():
				t.Fatal("root never reached the run engine")
			}
			defer endBeltRun(t, agent, double)
			double.mu.Lock()
			store := double.spec.Store
			double.mu.Unlock()
			agent.taskTimer = func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} }
			args, err := json.Marshal(taskArguments{Title: "Create green.txt", Summary: "Write green into green.txt", Brief: "Write green into green.txt", Deliverable: "green.txt", Acceptance: "green.txt contains green"})
			if err != nil {
				t.Fatal(err)
			}
			proposal, ok := agent.stageTask(ctx, args).(*stagedProposal)
			if !ok {
				t.Fatal("joined proposal was not staged")
			}
			agent.ResolveTask(proposal.id, TaskAnswer{Approved: true})
			if answer, refused, err := proposal.Commit(ctx); err != nil || refused {
				t.Fatalf("join refused: %s, %v", answer, err)
			}
			var joined *plandb.Task
			for _, task := range store.Tasks() {
				if task.Title == "Create green.txt" {
					joined = task
				}
			}
			if joined == nil || joined.ParentID != store.RootID() {
				t.Fatalf("proposal did not join the live run: %+v", joined)
			}
			if _, err := store.AddManyForWorker(joined.ID, []plandb.TaskSpec{{ID: "part", ParentID: joined.ID, Title: "Write green bytes", Description: "Do this part"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.AddReviewCheck(plandb.TaskSpec{ID: "check", ParentID: "part", Role: plandb.RoleCheck, Title: "Check green bytes", Description: "Check this part"}); err != nil {
				t.Fatal(err)
			}
			// Read through a second handle so the assignment fact must be stored.
			read, err := plandb.Open(store.Path(), "", "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			for _, id := range []string{joined.ID, "part", "check"} {
				doc := BeltWorkerBrief(read, read.Task(id), false, false, "", "")
				for _, forbidden := range []string{"ROOT_ONLY_ASSIGNMENT", "PERSON_ONLY_FORBIDDEN", "Create red.txt", "propose_task"} {
					if strings.Contains(doc, forbidden) {
						t.Errorf("%s reads another assignment %q:\n%s", id, forbidden, doc)
					}
				}
				for _, want := range []string{"Create green.txt", "Write green into green.txt", "green.txt contains green"} {
					if !strings.Contains(doc, want) {
						t.Errorf("%s lost its joined assignment %q:\n%s", id, want, doc)
					}
				}
			}
			if _, err := store.AddManyForWorker(store.RootID(), []plandb.TaskSpec{{ID: "root-part", ParentID: store.RootID(), Title: "Root part"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.AddReviewCheck(plandb.TaskSpec{ID: "root-check", ParentID: store.RootID(), Role: plandb.RoleCheck, Title: "Check root work"}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.AddMany([]plandb.TaskSpec{{ID: "root-fix", ParentID: store.RootID(), Title: "Fix root work"}}); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"root-part", "root-check", "root-fix"} {
				if doc := BeltWorkerBrief(read, read.Task(id), false, false, "", ""); !strings.Contains(doc, "ROOT_ONLY_ASSIGNMENT") {
					t.Fatalf("%s lost the root ask:\n%s", id, doc)
				}
			}
		})
	}
}
