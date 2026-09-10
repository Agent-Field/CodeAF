package session

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The fallback contract references the person's brief instead of spelling out
// an acceptance. A checker needs that request without inventing it from claims.
func TestCheckerReceivesBriefOnlyWhenFallbackAcceptanceReferencesIt(t *testing.T) {
	for _, acceptance := range []string{taskPersonAcceptance, "The approval phrase and its source are present."} {
		node := loneTestNode(t, "look up an earlier decision")
		node.spec.brief = "Save decision.json with the approval phrase and the source's stored metadata."
		node.spec.acceptance = acceptance
		question := auditQuestion(node, taskTree{}, auditGround{}, auditDoor{}, checkGround{}, landingFiles{}, "I saved the result", nil)
		if strings.Contains(question, node.spec.brief) != (acceptance == taskPersonAcceptance) {
			t.Fatalf("checker received the wrong contract for %q: %s", acceptance, question)
		}
		if !strings.Contains(question, "that retained text is the deliverable") || !strings.Contains(question, "factual claims still require independent evidence") {
			t.Fatal("checker was not told to evaluate a retained answer without inventing a file requirement")
		}
	}
}

// The real check boundary must see the conclusion after the display summary,
// including on a repaired result and a later check with no supplied claim.
func TestCheckerReadsKeptConclusionAtEveryCheck(t *testing.T) {
	for _, mode := range []string{"initial", "repair", "recheck", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			repo := newTestRepo(t)
			tree, err := prepareTaskTree(Place{}, repo, "c0ffee1122334455", 1, "explain the result")
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(tree.dir, "result.txt"), "the current result\n")
			node := loneTestNode(t, "explain the result")
			node.spec.acceptance = "The result and its context are explained."
			conclusion := "First line\nSecond line\nThird line\nBaseline context: the dependency failure predates this work."
			if mode != "legacy" {
				node.keepResult(conclusion)
			}
			if mode == "repair" {
				conclusion = "Repair line one\nRepair line two\nRepair line three\nCurrent context: the changed requirement is now addressed."
				node.keepResult(conclusion)
			}
			fallback := composeTaskReport(conclusion)
			if mode == "legacy" {
				fallback = conclusion
			}
			if mode == "recheck" {
				fallback = ""
			}
			seen := false
			completer := &routedCompleter{audit: []step{
				func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
					page := messageContentText(messages[len(messages)-1])
					seen = true
					if !strings.Contains(page, conclusion) {
						t.Errorf("checker lost the complete current conclusion: %s", page)
					}
					if mode == "repair" && strings.Contains(page, "dependency failure predates") {
						t.Error("checker received the superseded result")
					}
					if !strings.Contains(page, "CHECK THESE FILES HERE:") || !strings.Contains(page, "CLEAN RESTORE") {
						t.Error("checker was not oriented to its actual restore")
					}
					return textResponse("VERIFIED — the result and context are present"), nil
				},
			}}
			a, _ := newTestAgent(t, completer, func(c *Config) { c.Workspace = repo })
			verdict := a.auditNode(context.Background(), node, tree, []string{"result.txt"}, fallback, io.Discard)
			if !seen || !verdict.verified {
				t.Fatalf("check did not reach its reader: seen=%v, verdict=%s", seen, verdict.report())
			}
		})
	}
}

// A committed deliverable has no staged delta, so its checker must not be told
// that the empty index is the whole change or to install tools it cannot run.
func TestCheckerGroundExplainsCommittedWorkAndReadingOnly(t *testing.T) {
	repo := newTestRepo(t)
	before, _ := git(repo, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(repo, "result.txt"), "already committed\n")
	mustGit(t, repo, "add", "result.txt")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "deliver result")
	node := loneTestNode(t, "read the result")
	page := auditQuestion(node, taskTree{root: repo, checkBase: strings.TrimSpace(before)}, auditGround{dir: repo, restored: true}, plainDoor(auditReadCommands), checkGround{}, landingFiles{own: []string{"result.txt"}}, "the result is committed", nil)
	for _, want := range []string{repo, strings.TrimSpace(before), "may be empty when the work is already committed", "nothing staged", "no repeatable check"} {
		if !strings.Contains(strings.ToLower(page), strings.ToLower(want)) {
			t.Errorf("checker ground omitted %q", want)
		}
	}
	for _, wrong := range []string{"Its changes are staged", "Install and build whatever"} {
		if strings.Contains(page, wrong) {
			t.Errorf("checker received contradictory instruction %q", wrong)
		}
	}
}

// The existing result bound and overflow address also govern the checker; a
// complete conclusion must not become an unbounded transcript in its prompt.
func TestCheckerConclusionKeepsExistingBoundAndOverflow(t *testing.T) {
	node := loneTestNode(t, "read long result")
	node.produced = taskResult{text: strings.Repeat("x", taskResultLimit), bytes: taskResultLimit + 100, overflow: "/tmp/whole-result.txt"}
	claim := checkerConclusion(node, "old display report")
	if !strings.Contains(claim, "whole-result.txt") || !strings.Contains(claim, resultPartLead) || strings.Contains(claim, "old display report") {
		t.Fatal("bounded result lost its existing overflow pointer or reused its display report")
	}
	if !strings.Contains(claim, strings.Repeat("x", taskResultLimit)) {
		t.Fatal("checker changed the retained result bound")
	}
}

// A later worker that says nothing must not lend the checker an older success.
func TestCheckerDoesNotReuseAnEarlierConclusionOrReceiptsAfterEmptyWork(t *testing.T) {
	node := loneTestNode(t, "finish the result")
	node.keepWorkerConclusion("Earlier work succeeded.", io.Discard)
	node.keepReceipts([]toolReceipt{{tool: "bash", result: "earlier success"}})
	node.keepWorkerConclusion("", io.Discard)
	node.keepReceipts(nil)
	if got := checkerConclusion(node, ""); got != "" {
		t.Fatalf("empty latest attempt reused an old conclusion: %q", got)
	}
	if len(node.lastReceipts()) != 0 {
		t.Fatal("empty latest attempt reused old receipts")
	}
}

// A resolver changes the files after the first check. The actual second check
// must receive its current explanation and receipts instead of the old report.
func TestCheckerAfterMergeReceivesResolversConclusion(t *testing.T) {
	repo, tree := conflictingRepo(t, "fedcba1122334455", 3, "edit the shared file")
	const conclusion = "Resolved the shared file.\nBoth sides are retained.\nThe result is ready.\nResolution context: the two lines express independent requirements."
	seen := false
	completer := &routedCompleter{
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("current-write", "write", `{"path":"shared.txt","content":"the other side's line\nthe task's line\n"}`), nil
			},
			finalText(conclusion),
		},
		audit: []step{
			func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
				page := messageContentText(messages[len(messages)-1])
				seen = true
				if !strings.Contains(page, conclusion) || !strings.Contains(page, "shared.txt") || strings.Contains(page, "OLD-CONCLUSION") {
					t.Errorf("recheck did not receive the resolver's current context: %s", page)
				}
				return textResponse("VERIFIED — both lines are in shared.txt"), nil
			},
		},
	}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.Workspace = repo })
	node := resolvableTestNode(t, "edit the shared file")
	node.keepResult("OLD-CONCLUSION")
	detail := conflictSentence(tree.branch, []string{"shared.txt"}, "")
	state := a.landConflicted(context.Background(), node, tree, []string{"shared.txt"}, "OLD-CONCLUSION", mergeConflicted, detail, io.Discard)
	if !seen || state != TaskDone {
		t.Fatalf("resolved work did not reach and pass its check: seen=%v, state=%s", seen, state)
	}
	if len(node.lastReceipts()) != 1 || node.lastReceipts()[0].tool != "write" {
		t.Fatal("resolver's actual receipt was not retained")
	}
}

// A check after undoing a rejected resolution must distinguish the attempted
// merge from the branch that was actually restored.
func TestCheckerAfterRollbackSeesTheRestoredState(t *testing.T) {
	repo, tree := conflictingRepo(t, "abcdef2233445566", 3, "edit the shared file")
	const conclusion = "The resolver retained both lines in shared.txt."
	checks := 0
	completer := &routedCompleter{
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("resolve-write", "write", `{"path":"shared.txt","content":"the other side's line\nthe task's line\n"}`), nil
			},
			finalText(conclusion),
		},
		audit: []step{
			finalText("REFUTED — this resolution does not meet the requested behavior"),
			func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
				checks++
				page := messageContentText(messages[len(messages)-1])
				for _, want := range []string{"Rollback completed:", "abandoned attempt, not changes retained", conclusion} {
					if !strings.Contains(page, want) {
						t.Errorf("later check lost rollback context %q: %s", want, page)
					}
				}
				return textResponse("REFUTED — the restored branch still needs integration"), nil
			},
		},
	}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.Workspace = repo })
	node := resolvableTestNode(t, "edit the shared file")
	_, _, _ = a.mergeRoundAtLanding(context.Background(), node, tree, []string{"shared.txt"}, "previous report", io.Discard)
	if got := readFile(t, filepath.Join(tree.dir, "shared.txt")); strings.Contains(got, "the other side's line") {
		t.Fatalf("test did not restore the pre-resolution file: %q", got)
	}
	verdict := a.auditNode(context.Background(), node, tree, []string{"shared.txt"}, "", io.Discard)
	if checks != 1 || !verdict.answered || verdict.verified {
		t.Fatalf("later check did not examine the restored state: checks=%d verdict=%s", checks, verdict.report())
	}
}

// A failed reset must never be described as successful restoration, and its
// actual error stays available to the next reader with the attempted result.
func TestCheckerRollbackFailureKeepsTheActualError(t *testing.T) {
	repo := newTestRepo(t)
	node := loneTestNode(t, "preserve the result")
	node.keepResult("The resolver's attempted conclusion.")
	(&Agent{}).undoMergeRound(node, taskTree{dir: repo, branch: "task/missing-before-ref"}, io.Discard)
	claim := checkerConclusion(node, "")
	for _, want := range []string{"Rollback failed:", "restoration was not confirmed", "unknown revision", "The resolver's attempted conclusion."} {
		if !strings.Contains(claim, want) {
			t.Errorf("failed rollback lost %q: %s", want, claim)
		}
	}
	if strings.Contains(claim, "Rollback completed:") {
		t.Fatal("failed reset was described as a completed rollback")
	}
}
