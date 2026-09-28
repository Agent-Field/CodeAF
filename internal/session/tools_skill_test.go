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
	"os"
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

// agentskillsShelfSkill puts one imported skill on the shelf: a real directory
// holding a top-level SKILL.md (the shape a skill written for Claude Code,
// Codex or any agentskills.io harness arrives in), recorded and activated the
// way the store builds the shelf, with the ORIGINAL directory as the artifact.
func agentskillsShelfSkill(t *testing.T, brain *store.Store, name, doc string) string {
	t.Helper()
	folder := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("make skill folder %s: %v", folder, err)
	}
	body := "---\nname: " + name + "\ndescription: " + doc + "\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(folder, "references"), 0o755); err != nil {
		t.Fatalf("make references: %v", err)
	}
	candidate, err := brain.RecordSkillCandidate("", "repo:audit", doc, folder)
	if err != nil {
		t.Fatalf("record skill %s: %v", name, err)
	}
	if err := brain.ActivateSkill(candidate.Seq, folder, ""); err != nil {
		t.Fatalf("activate skill %s: %v", name, err)
	}
	return folder
}

// executableShelfSkill puts one forged skill on disk — run.sh and check.sh,
// executable, no SKILL.md — and on the shelf, which is the shape the shelf has
// always held.
func executableShelfSkill(t *testing.T, brain *store.Store, name, doc string) string {
	t.Helper()
	folder := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("make skill folder %s: %v", folder, err)
	}
	for _, script := range []string{"run.sh", "check.sh"} {
		if err := os.WriteFile(filepath.Join(folder, script), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("write %s: %v", script, err)
		}
	}
	candidate, err := brain.RecordSkillCandidate("", "repo:audit", doc, folder)
	if err != nil {
		t.Fatalf("record skill %s: %v", name, err)
	}
	if err := brain.ActivateSkill(candidate.Seq, folder, ""); err != nil {
		t.Fatalf("activate skill %s: %v", name, err)
	}
	return folder
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

// GET ON AN AGENTSKILLS FOLDER points at the SKILL.md, not the directory: the
// directory is what `read` refuses, and the tool's own description promises a
// path the worker then opens with `read`. The answer's shape does not change —
// name, doc, path — only which path.
func TestUseSkillGetPointsAnAgentskillsFolderAtItsBodyFile(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	folder := agentskillsShelfSkill(t, brain, "pdf-extract", "Extract pages from PDFs.")

	out := useSkill(t, agent, `{"mode":"get","name":"pdf-extract"}`)
	want := "pdf-extract: Extract pages from PDFs.\nPath: " + filepath.Join(folder, "SKILL.md")
	if out != want {
		t.Fatalf("get on an agentskills folder:\ngot:  %q\nwant: %q", out, want)
	}
}

// GET ON A FORGED SKILL keeps its directory, byte for byte — the compatibility
// law: a skill whose artifact holds no top-level SKILL.md is answered exactly
// as it always was, because the directory is the thing the worker runs.
func TestUseSkillGetKeepsExecutableSkillsOnTheirDirectory(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	folder := executableShelfSkill(t, brain, "imgshrink", "Optimize images without losing quality.")

	out := useSkill(t, agent, `{"mode":"get","name":"imgshrink"}`)
	want := "imgshrink: Optimize images without losing quality.\nPath: " + folder
	if out != want {
		t.Fatalf("get on an executable skill:\ngot:  %q\nwant: %q", out, want)
	}
}

// A PATH THAT DOES NOT RESOLVE answers with the artifact as it stands — no
// error, no refusal — because the shelf has always held facts whose
// directories come and go.
func TestUseSkillGetToleratesAMissingArtifact(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	// shelfSkill's artifact is a directory nothing ever created.
	missing := shelfSkill(t, brain, "gone", "A skill whose directory left.")

	out := useSkill(t, agent, `{"mode":"get","name":"gone"}`)
	want := "gone: A skill whose directory left.\nPath: " + missing
	if out != want {
		t.Fatalf("get on a missing artifact:\ngot:  %q\nwant: %q", out, want)
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
	if !strings.Contains(out, "Skill \"no-such-skill\" not found.") {
		t.Fatalf("an unknown name did not answer with the not-found sentence:\n%s", out)
	}
	if !strings.Contains(out, "1 active skills on the shelf") {
		t.Fatalf("a miss on a populated shelf did not say how many skills are active:\n%s", out)
	}
}

// A MISS IS NOT A DEAD END: the answer names the nearest skills, scored against
// the name and the doc line, so a model that guessed a name learns what the
// shelf actually holds and how close it got.
func TestUseSkillNotFoundNamesTheNearestSkills(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	shelfSkill(t, brain, "repo-audit", "Walk a repository for dead code and unused exports.")
	shelfSkill(t, brain, "flaky-test", "Re-run a failing test in isolation to separate flake from breakage.")

	out := useSkill(t, agent, `{"mode":"get","name":"repo-audit-report"}`)
	if !strings.Contains(out, "repo-audit") {
		t.Fatalf("a miss did not name the nearest skill on the shelf:\n%s", out)
	}
	if strings.Contains(out, "flaky-test") {
		t.Fatalf("the miss named a skill that scored nowhere near the asked name:\n%s", out)
	}
}

// A GET THAT DIFFERS ONLY IN CASE resolves: a folder called Release-Notes is
// not a different skill from release-notes, and the hit is answered with the
// shelf's own spelling so the name a worker reads back is the one that works
// next time.
func TestUseSkillGetResolvesCaseInsensitively(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	shelfSkill(t, brain, "repo-audit", "Walk a repo for dead code and unused exports.")

	out := useSkill(t, agent, `{"mode":"get","name":"Repo-Audit"}`)
	if !strings.HasPrefix(out, "repo-audit: Walk a repo for dead code and unused exports.\nPath: ") {
		t.Fatalf("a case-folded name did not resolve to the shelf's own spelling:\n%s", out)
	}
}

// THE LISTING IS SORTED BY NAME, whatever order the shelf returns: two calls
// minutes apart must read as the same shelf unless a skill actually moved.
func TestUseSkillListIsSortedByName(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	shelfSkill(t, brain, "zeta", "Later.")
	shelfSkill(t, brain, "alpha", "Earlier.")

	out := useSkill(t, agent, `{"mode":"list"}`)
	if strings.Index(out, "alpha") > strings.Index(out, "zeta") {
		t.Fatalf("the listing is not sorted by name:\n%s", out)
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
