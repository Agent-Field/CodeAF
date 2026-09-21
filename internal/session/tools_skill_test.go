package session

// The skill hand, driven the way the wire drives it: what a worker is handed,
// what one call answers with, and who does not get the verb at all.
//
// A skill is a store.Fact of kind "skill" that has been activated, whose
// Artifact is the directory on the shelf. These tests build the shelf the way
// the store builds it — a candidate recorded, then activated — so the reading
// path under test is the one a real shelf produces.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// shelfSkill records one skill candidate and activates it, which is the only
// transition that makes it retrievable. It answers the artifact path it was put
// on, for the assertions that must NOT see it in a listing.
func shelfSkill(t *testing.T, brain *store.Store, name, doc string) string {
	t.Helper()
	artifact := filepath.Join(t.TempDir(), "shelf", name)
	// An empty node id is the root's own channel, which is what the store's
	// own skill tests record on (exec_test.go, notebook_test.go).
	candidate, err := brain.RecordSkillCandidate("", "repo:audit", doc, artifact)
	if err != nil {
		t.Fatalf("record skill %s: %v", name, err)
	}
	if err := brain.ActivateSkill(candidate.Seq, artifact, ""); err != nil {
		t.Fatalf("activate skill %s: %v", name, err)
	}
	return artifact
}

// useSkill calls the tool the way the wire does.
func useSkill(t *testing.T, agent *Agent, args string) string {
	t.Helper()
	out, failed, err := agent.runUseSkill(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("use_skill: %v", err)
	}
	if failed {
		t.Fatalf("use_skill refused %s: %s", args, out)
	}
	return out
}

// LIST SHOWS DOC LINES AND NOTHING ELSE: one skill per line, the name and its
// one-line doc, and never the shelf path or any internal field. A listing that
// leaked a path would spend the model's attention on a directory it has not
// asked to open.
func TestUseSkillList(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	auditPath := shelfSkill(t, brain, "repo-audit", "Walk a repo for dead code and unused exports.")
	testPath := shelfSkill(t, brain, "flaky-test", "Re-run a failing test in isolation to separate flake from breakage.")

	out := useSkill(t, agent, `{"mode":"list"}`)
	for _, want := range []string{
		"- repo-audit: Walk a repo for dead code and unused exports.",
		"- flaky-test: Re-run a failing test in isolation to separate flake from breakage.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing does not carry %q:\n%s", want, out)
		}
	}
	for _, leak := range []string{auditPath, testPath, "Path:"} {
		if strings.Contains(out, leak) {
			t.Errorf("the listing leaks %q, which a discovery row must not carry:\n%s", leak, out)
		}
	}
}

// GET RESOLVES ONE NAME to the shelf path a worker will open and the doc that
// says what the skill is for.
func TestUseSkillGet(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	auditPath := shelfSkill(t, brain, "repo-audit", "Walk a repo for dead code and unused exports.")

	out := useSkill(t, agent, `{"mode":"get","name":"repo-audit"}`)
	if !strings.Contains(out, "Walk a repo for dead code and unused exports.") {
		t.Errorf("the answer does not carry the skill's doc:\n%s", out)
	}
	if !strings.Contains(out, "Path: "+auditPath) {
		t.Errorf("the answer does not point at the shelf path %q:\n%s", auditPath, out)
	}
}

// THE VERB IS ABSENT, NOT REFUSING, ON A NODE STANDING ON THE FLOOR. It is the
// same gate propose_task reads (mayProposeTask), and a floor node is handed the
// store here specifically to prove the DEPTH is what keeps the verb off: memory
// alone does not put it there.
func TestUseSkillAbsentAtDepthFloor(t *testing.T) {
	agent, _ := brainAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.tasker = graphForShape(t)
		config.taskID = 2
		config.taskDepth = taskDepthLimit
	})
	if beltHas(agent, useSkillToolName) {
		t.Fatal("a node on the floor of its tree was handed a verb over a shelf it should not reach")
	}
}

// AN UNKNOWN NAME IS A NOT-FOUND ANSWER rather than a failure: the shelf simply
// does not have it, and the model can list what is there.
func TestUseSkillNotFound(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	shelfSkill(t, brain, "repo-audit", "Walk a repo for dead code and unused exports.")

	out := useSkill(t, agent, `{"mode":"get","name":"no-such-skill"}`)
	if !strings.Contains(out, "Skill 'no-such-skill' not found.") {
		t.Fatalf("an unknown name did not answer with the not-found sentence:\n%s", out)
	}
}

// AN EMPTY SHELF SAYS SO, and it is not the same sentence as a populated one:
// "no active skills" names the state rather than printing an empty list.
func TestUseSkillEmptyShelf(t *testing.T) {
	agent, _ := brainAgent(t, &scriptedCompleter{}, nil)
	out := useSkill(t, agent, `{"mode":"list"}`)
	if out != "No active skills on the shelf." {
		t.Fatalf("an empty shelf answered %q, want the empty-shelf sentence", out)
	}
}
