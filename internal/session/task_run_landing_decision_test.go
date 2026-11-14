package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The provider is scripted, but admission, Git landing and saved conversation
// recovery use the same doors as an ordinary local task.
func localLandingFixture(t *testing.T, collision bool) (*Agent, string, uint64, func() *Agent) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo, place := beltRunCommittedRepo(t), t.TempDir()
	mustGit(t, repo, "checkout", "-b", "feature/landing")
	engine := newBeltRunDouble("COLLISION.txt is ready")
	engine.real = true
	engine.work = func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "COLLISION.txt"), []byte("task copy\n"), 0600); err != nil {
			t.Errorf("task write: %v", err)
		}
	}
	registerBeltRunEngine(t, engine)
	open := func() *Agent {
		a, _ := newTestAgent(t, beltRunCompleter{text: "COLLISION.txt is ready"}, func(c *Config) {
			c.Workspace, c.Place, c.SessionFile = repo, Place{Dir: place}, filepath.Join(place, placeTranscript)
			c.AskConsent = false
		})
		return a
	}
	a := open()
	id, _, _, err := a.StartTask(context.Background(), "Create COLLISION.txt containing exactly task copy followed by newline. Do not edit any other file.", false)
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	if collision {
		writeFile(t, filepath.Join(repo, "COLLISION.txt"), "my newer untracked copy\n\x00")
	}
	before := beltRunRepoState(t, repo)
	close(engine.release)
	beltRunWaitFor(t, "local landing to settle", func() bool {
		a.beltMu.Lock()
		defer a.beltMu.Unlock()
		return a.beltRun == nil
	})
	if collision && beltRunRepoState(t, repo) != before {
		t.Fatal("refused landing changed the person's checkout")
	}
	return a, repo, id, open
}

func TestLocalRunUntrackedCollisionNeedsYourCall(t *testing.T) {
	a, repo, id, _ := localLandingFixture(t, true)
	page, ok := a.PlanTaskPage(planStoreID(strconv.FormatUint(id, 10)))
	if !ok || page.Row.StateWord() != "your call" {
		t.Fatalf("conflicted task says %q, want your call", page.Row.StateWord())
	}
	if got := readFile(t, filepath.Join(repo, "COLLISION.txt")); got != "my newer untracked copy\n\x00" {
		t.Fatalf("person's bytes changed: %q", got)
	}
	questions := a.OpenQuestions()
	if len(questions) != 1 || questions[0].Kind != QuestionConflict || questions[0].Reason != "your folder already has files the task wrote: COLLISION.txt" {
		t.Fatalf("landing question = %+v", questions)
	}
	q := questions[0]
	if len(q.Options) < 2 || q.Options[0].Key != LandingYesKey || q.Options[0].Label != "resolve it" || q.Options[1].Label != "drop it" || !strings.Contains(q.Options[0].Consequence, ".yours") {
		t.Fatalf("landing controls = %+v", q.Options)
	}
	if branches := gitOut(t, repo, "branch", "--list", "task/*"); strings.TrimSpace(branches) == "" {
		t.Fatal("conflicted task lost its branch")
	}
}

func TestLocalRunUntrackedCollisionResolvesAndKeepsYours(t *testing.T) {
	for _, restart := range []bool{false, true} {
		for _, tool := range []bool{false, true} {
			name := "question"
			if tool {
				name = "tasks-tool"
			}
			if restart {
				name += "-after-restart"
			}
			t.Run(name, func(t *testing.T) {
				a, repo, id, open := localLandingFixture(t, true)
				if restart {
					if err := a.Close(); err != nil {
						t.Fatal(err)
					}
					a = open()
				}
				questions := a.OpenQuestions()
				if len(questions) != 1 || questions[0].ID != id || questions[0].Kind != QuestionConflict || questions[0].Reason != "your folder already has files the task wrote: COLLISION.txt" {
					t.Fatalf("saved collision has no answerable question: %+v", questions)
				}
				if tool {
					args, _ := json.Marshal(map[string]string{"id": strconv.FormatUint(id, 10), "resolve": "accept"})
					reply, bad, err := a.tasksTool().Execute(context.Background(), args)
					if err != nil || bad {
						t.Fatalf("resolve: %s, bad=%v err=%v", reply, bad, err)
					}
				} else if err := a.ResolveQuestion(Answer{Kind: QuestionConflict, ID: id, Key: LandingYesKey}); err != nil {
					t.Fatalf("a resolve it: %v", err)
				}
				if got := readFile(t, filepath.Join(repo, "COLLISION.txt")); got != "task copy\n" {
					t.Fatalf("task's file did not land: %q", got)
				}
				if got := readFile(t, filepath.Join(repo, "COLLISION.txt.yours")); got != "my newer untracked copy\n\x00" {
					t.Fatalf("person's bytes were not kept beside task's: %q", got)
				}
				if questions := a.OpenQuestions(); len(questions) != 0 {
					t.Fatalf("answered question still open: %+v", questions)
				}
				if err := a.Close(); err != nil {
					t.Fatal(err)
				}
				a = open()
				page, ok := a.PlanTaskPage(planStoreID(strconv.FormatUint(id, 10)))
				if !ok || page.Row.StateWord() != "done" || len(a.OpenQuestions()) != 0 {
					t.Fatalf("settled landing reopened incorrectly: %+v", page.Row)
				}
			})
		}
	}
}

func TestLocalRunCleanLandingStillEndsDone(t *testing.T) {
	a, repo, id, _ := localLandingFixture(t, false)
	page, ok := a.PlanTaskPage(planStoreID(strconv.FormatUint(id, 10)))
	if !ok || page.Row.StateWord() != "done" || len(a.OpenQuestions()) != 0 {
		t.Fatalf("clean landing did not finish: %+v", page.Row)
	}
	if got := readFile(t, filepath.Join(repo, "COLLISION.txt")); got != "task copy\n" {
		t.Fatalf("clean landing lost task's file: %q", got)
	}
}
