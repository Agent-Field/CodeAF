package session

// THE ATTRIBUTION LAW HAS TWO READERS AND THIS FILE HOLDS BOTH TO THE SAME BYTES.
//
// One reader is the model, which is told the law in words on the belt and then
// types the trailer itself (beltfacts.go). The other is the harness, which
// commits a node's work without asking anybody and appends the trailer with no
// model in the loop (task_run.go's [signed]). A build where the two write
// different lines is a build whose git history cannot be counted.
//
// The bytes are pinned in internal/exec (attribution_test.go there), so what is
// asked here is that the one spelling there is what reaches each reader — and,
// for the commit the harness writes itself, the exact bytes, because nobody
// reads that message before it is history.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/exec"
)

// TestTheAttributionLawIsOnEveryBelt is the belt half. Signing has no off, so
// every page spells the law, and the `Assisted-by` line names the model this
// session runs — filled by the render, never left for the model to guess.
func TestTheAttributionLawIsOnEveryBelt(t *testing.T) {
	page := promptWithBeltFacts(Config{Workspace: t.TempDir(), Model: "deepseek/deepseek-v4-flash"})
	want := "`Assisted-by: CodeAF (deepseek-v4-flash)` and `" + exec.AttributionTrailer + "` as its last two lines"
	if !strings.Contains(page, want) {
		t.Fatalf("the page does not spell both trailer lines, model named and in order: want %q", want)
	}
	if !strings.Contains(page, exec.AttributionPullFooter) {
		t.Fatalf("the page never spells the pull-request footer")
	}
	// AND THE COMMENT LINE, WHICH IS THE ONE WITH A BOUND ON IT. The chat is
	// where comments get written, so a page that spelled the line without saying
	// once-per-thread would be the page that turns provenance into a signature on
	// every reply in somebody's thread.
	if !strings.Contains(page, exec.AttributionCommentFooter) {
		t.Fatalf("the page never spells the comment line")
	}
	for _, want := range []string{"ONCE per thread", "one-liner", "suggestion block", "dictated"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page spells the comment line without its bound: %q", want)
		}
	}
	// AND THE PLACES IT MUST NOT GO ARE ON THE PAGE, because that half is the
	// half a model gets wrong: a footer in the reply, a trailer in a README. The
	// CONTRIBUTING sentence is the repository's rule, and it still wins.
	for _, want := range []string{"commit subject", "README", "CONTRIBUTING"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page does not say attribution stays out of %q", want)
		}
	}
	// AND THE LINES ARE SAID ONCE. The page used to carry the law and then a
	// second sentence spelling the trailer block again; one spelling is the one
	// that cannot drift.
	if n := strings.Count(page, exec.AttributionTrailer); n != 1 {
		t.Fatalf("the page spells the co-author %d times, want once", n)
	}
	for _, unwanted := range []string{exec.AttributionAssistedBySlot, "CodeAF ()", "deepseek/deepseek-v4-flash)"} {
		if strings.Contains(page, unwanted) {
			t.Fatalf("the page carries %q", unwanted)
		}
	}
}

// THE MODEL-OFF LINE. A person who turned the `attribution.model` row off still
// signs; the `Assisted-by` line is bare, and never an empty `()`.
func TestTheModelOffLineIsBareAndStillSigns(t *testing.T) {
	page := promptWithBeltFacts(Config{Workspace: t.TempDir(), Model: "deepseek/deepseek-v4-flash", AttributionModelOff: true})
	want := "`Assisted-by: CodeAF` and `" + exec.AttributionTrailer + "` as its last two lines"
	if !strings.Contains(page, want) {
		t.Fatalf("the model-off page does not carry the bare line and the co-author: want %q", want)
	}
	for _, unwanted := range []string{"deepseek", "CodeAF (", "CodeAF ()"} {
		if strings.Contains(page, unwanted) {
			t.Fatalf("the model-off page still says %q", unwanted)
		}
	}
}

// TestALandedCommitCarriesTheExactTrailerBytes is the harness half: the commit
// nobody was asked about. It is the one attribution nothing else can catch — no
// model saw this message, so a wrong trailer here is wrong forever. So it is
// pinned on the exact bytes git holds, with the model named and without.
func TestALandedCommitCarriesTheExactTrailerBytes(t *testing.T) {
	const coAuthor = "Co-Authored-By: CodeAF <267109073+agentfield-bot@users.noreply.github.com>"
	for _, row := range []struct {
		name string
		sign gitSignature
		want string
	}{
		{
			name: "model on",
			sign: gitSignature{named: true, model: "deepseek/deepseek-v4-flash"},
			want: "task: write the report\n\nAssisted-by: CodeAF (deepseek-v4-flash)\n" + coAuthor + "\n",
		},
		{
			name: "model off",
			sign: gitSignature{named: false, model: "deepseek/deepseek-v4-flash"},
			want: "task: write the report\n\nAssisted-by: CodeAF\n" + coAuthor + "\n",
		},
		{
			name: "no model known",
			sign: gitSignature{},
			want: "task: write the report\n\nAssisted-by: CodeAF\n" + coAuthor + "\n",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			repo := newTestRepo(t)
			tree, err := prepareTaskTree(Place{}, repo, "d1d1d1d1d1d1d1d1", 1, "write the report")
			if err != nil {
				t.Fatalf("prepareTaskTree: %v", err)
			}
			writeFile(t, filepath.Join(tree.dir, "report.md"), "# what happened\n")
			if _, problem, _ := commitTaskWork(tree.dir, "write the report", []string{"report.md"}, row.sign, false); problem != "" {
				t.Fatalf("the landing could not commit: %s", problem)
			}
			// The commit object itself: headers, one blank line, and the message
			// exactly as git stored it.
			object := gitOut(t, tree.dir, "cat-file", "commit", "HEAD")
			_, message, found := strings.Cut(object, "\n\n")
			if !found || message != row.want {
				t.Fatalf("the landed commit's message is\n%q\nwant\n%q", message, row.want)
			}
			// AND GIT READS BOTH AS TRAILERS, in order.
			trailers := gitOut(t, tree.dir, "log", "-1", "--format=%(trailers:only)")
			if lines := strings.Split(strings.TrimSpace(trailers), "\n"); len(lines) != 2 ||
				!strings.HasPrefix(lines[0], "Assisted-by: CodeAF") || lines[1] != coAuthor {
				t.Fatalf("git does not read the two lines as the trailer block: %q", trailers)
			}
			// AND THE AUTHOR IS THE BOT ACCOUNT. The trailer is provenance on top;
			// the author line is the identity sibling landings read
			// (task_branch_protection.go).
			if got := strings.TrimSpace(gitOut(t, tree.dir, "log", "-1", "--format=%an|%ae")); got != codeafGitName+"|"+codeafGitEmail {
				t.Fatalf("the landed commit is authored %q, want %q|%q", got, codeafGitName, codeafGitEmail)
			}
		})
	}
}

// A NODE'S LANDING NAMES THE MODEL THE NODE RAN ON, and falls back to the
// conversation's only when the node names none.
func TestASignatureNamesTheModelTheWorkRanOn(t *testing.T) {
	conversation := gitSignature{named: true, model: "deepseek/deepseek-v4-flash"}
	if got := conversation.ranOn("qwen/qwen3-coder").sign("task: x"); !strings.HasSuffix(got, "Assisted-by: CodeAF (qwen3-coder)\n"+exec.AttributionTrailer) {
		t.Fatalf("a node on its own model signed as %q", got)
	}
	if got := conversation.ranOn("").sign("task: x"); !strings.HasSuffix(got, "Assisted-by: CodeAF (deepseek-v4-flash)\n"+exec.AttributionTrailer) {
		t.Fatalf("a node naming no model signed as %q", got)
	}
}

// THE SETTING TRAVELS WITH THE WORK. A node is handed no ProfileDir and could
// not re-read the row if it wanted to, so a child that did not inherit it would
// name the model for somebody who turned the name off — in a worktree, with
// nobody watching.
func TestATaskNodeInheritsTheModelNameRow(t *testing.T) {
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.AttributionModelOff = true
	})
	// THROUGH THE PRODUCTION CONSTRUCTOR AND NEVER AROUND IT (task_divide_test.go's
	// [workerFor] says why): the defect this guards against is a field the real
	// constructor forgot to copy.
	worker, _ := workerFor(t, session, taskSpec{
		title: "land the change", request: "land the change", brief: "land the change",
		acceptance: "it lands", depth: 1,
	})
	if !worker.config.AttributionModelOff {
		t.Fatal("the person's answer did not travel from the conversation to the worker it built")
	}
	if worker.signsGitWork().named {
		t.Fatal("a worker that inherited the row still names the model in what it lands")
	}
}
