package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

func landGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// landBackend is an agent whose workspace is a temp repository with one
// recorded task branch, and whose own folder records it.
func landBackend(t *testing.T) (*appBackend, appwire.Agent, string) {
	t.Helper()
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	repo := agent.Workspace
	landGit(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	landGit(t, repo, "add", ".")
	landGit(t, repo, "commit", "-q", "-m", "base")
	landGit(t, repo, "checkout", "-q", "-b", "task/paint-abc123")
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("task\n"), 0o644)
	landGit(t, repo, "add", ".")
	landGit(t, repo, "commit", "-q", "-m", "work")
	landGit(t, repo, "checkout", "-q", "main")
	held, err := backend.find(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	backend.locked(func() { held.transcript = filepath.Join(t.TempDir(), "0123456789abcdef", "transcript.jsonl") })
	dir := filepath.Dir(held.transcript)
	os.MkdirAll(dir, 0o700)
	doc := `{"type":"tasks","version":1,"seq":1,"nodes":[],"runs":[{"id":1,"title":"Paint","state":"done","branch":"task/paint-abc123"}]}`
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return backend, agent, repo
}

func TestABranchLandsOnceOnTheConfirmedPlan(t *testing.T) {
	backend, agent, repo := landBackend(t)
	head := landGit(t, repo, "rev-parse", "HEAD")
	plan, err := backend.PlanBranchLand(appwire.BranchLandPlanQuery{Agent: agent.ID, Branch: "task/paint-abc123"})
	if err != nil || plan.Plan == "" || plan.Revision == "" || plan.Target != "main" || plan.Head != head || !plan.FastForward || plan.Title != "Paint" || plan.Files == nil || *plan.Files != 1 {
		t.Fatalf("plan %+v %v", plan, err)
	}
	// A wrong revision spends the plan and lands nothing.
	if _, err := backend.LandBranch(appwire.BranchLandQuery{Agent: agent.ID, Plan: plan.Plan, Revision: strings.Repeat("0", 64)}); !hasCode(err, appwire.CodeConflict) {
		t.Fatalf("wrong revision: %v", err)
	}
	if _, err := backend.LandBranch(appwire.BranchLandQuery{Agent: agent.ID, Plan: plan.Plan, Revision: plan.Revision}); !hasCode(err, appwire.CodeConflict) {
		t.Fatalf("spent plan: %v", err)
	}
	if got := landGit(t, repo, "rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD moved to %s", got)
	}
	plan, err = backend.PlanBranchLand(appwire.BranchLandPlanQuery{Agent: agent.ID, Branch: "task/paint-abc123"})
	if err != nil {
		t.Fatal(err)
	}
	landed, err := backend.LandBranch(appwire.BranchLandQuery{Agent: agent.ID, Plan: plan.Plan, Revision: plan.Revision})
	if err != nil || landed.HeadAfter != plan.BranchHead || landed.HeadBefore != head {
		t.Fatalf("landed %+v %v", landed, err)
	}
	if _, err := backend.PlanBranchLand(appwire.BranchLandPlanQuery{Agent: agent.ID, Branch: "task/paint-abc123"}); !hasCode(err, appwire.CodeConflict) {
		t.Fatalf("landed branch planned again: %v", err)
	}
	if _, err := backend.PlanBranchLand(appwire.BranchLandPlanQuery{Agent: agent.ID, Branch: "main"}); !hasCode(err, appwire.CodeNotFound) {
		t.Fatalf("unrecorded branch: %v", err)
	}
}

func TestADirtyCheckoutIsARefusalWithItsPaths(t *testing.T) {
	backend, agent, repo := landBackend(t)
	os.WriteFile(filepath.Join(repo, "mine.txt"), []byte("x"), 0o644)
	_, err := backend.PlanBranchLand(appwire.BranchLandPlanQuery{Agent: agent.ID, Branch: "task/paint-abc123"})
	e, ok := err.(*appwire.Error)
	if !ok || e.Code != appwire.CodeConflict {
		t.Fatalf("dirty: %v", err)
	}
	if detail, ok := e.Detail.(appwire.BranchLandRefusal); !ok || detail.Reason != session.LandDirty || len(detail.Paths) != 1 || detail.Paths[0] != "mine.txt" {
		t.Fatalf("detail %+v", e.Detail)
	}
}

func TestLandPlansExpireAndAreBounded(t *testing.T) {
	now := time.Unix(1000, 0)
	store := newAppLandPlanStore(func() time.Time { return now })
	id, _, err := store.put(appLandPlan{agent: "a"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(appLandPlanTTL)
	if _, ok := store.take("a", id); ok {
		t.Fatal("an expired plan was taken")
	}
	now = now.Add(time.Second)
	id, _, _ = store.put(appLandPlan{agent: "a"})
	if _, ok := store.take("b", id); ok {
		t.Fatal("another agent took the plan")
	}
	if _, ok := store.take("a", id); ok {
		t.Fatal("a plan offered to the wrong agent survived")
	}
	// An agent past its share loses its oldest.
	var ids []string
	for i := 0; i <= appwire.BranchLandPlansPerAgent; i++ {
		now = now.Add(time.Millisecond)
		id, _, err := store.put(appLandPlan{agent: "a"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, ok := store.take("a", ids[0]); ok {
		t.Fatal("the oldest plan survived its agent's share")
	}
	if _, ok := store.take("a", ids[len(ids)-1]); !ok {
		t.Fatal("the newest plan was lost")
	}
	// The whole store is bounded across agents.
	full := newAppLandPlanStore(func() time.Time { return now })
	for i := 0; i < appwire.BranchLandPlansMax; i++ {
		if _, _, err := full.put(appLandPlan{agent: fmt.Sprint("agent", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := full.put(appLandPlan{agent: "late"}); !hasCode(err, appwire.CodeBusy) {
		t.Fatalf("a full store: %v", err)
	}
}

func TestTwoConfirmationsOfOnePlanTakeItOnce(t *testing.T) {
	store := newAppLandPlanStore(time.Now)
	id, _, _ := store.put(appLandPlan{agent: "a"})
	var wg sync.WaitGroup
	var mu sync.Mutex
	taken := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := store.take("a", id); ok {
				mu.Lock()
				taken++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if taken != 1 {
		t.Fatalf("taken %d times", taken)
	}
}
