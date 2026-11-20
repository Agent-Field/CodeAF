package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// ── a turn that checked what it changed has finished ────────────────────────

// The finished replies the request for a note still sent back after #2834, as
// deepseek-v4.1-flash wrote them on the connected-canvas qloop (open-day-flyer
// t2, "date moved to sat 14 march, fix the flyer"). Each is two plain
// paragraphs, the shape [readsAsAShortNote] reads as a note, and each was
// answered [NoChangeReply] when it was sent back.
const (
	flyerFixedRun15 = "Fixed — the flyer now reads **Saturday 14 March · 10am–1pm**. Same doc, edited in place: " +
		"[workaf://canvas/cv_dqcxhylk4i32#f_hipxaavby3n3](workaf://canvas/cv_dqcxhylk4i32#f_hipxaavby3n3)\n\n" +
		"Verified on the current look: the date line shows \"Saturday 14 March · 10am–1pm\", everything else unchanged, " +
		"no cut-off or overlap. The A4 PDF for the print run is re-exported from the canvas."
	flyerFixedRun14 = "Done — the flyer now reads Saturday 14 March, 10am–1pm. Re-exported the PDF (print from the flyer's own menu → Export).\n\n" +
		"One thing worth a look: your notes doc still says \"Saturday 7 March\" — say the word and I'll update that too."
	// flyerSeen is what the second model said about the run 15 look.
	flyerSeen = "* **Date and Time:** Saturday, 14 March, from 10:00 am to 1:00 pm.\n" +
		"* **Exact Date Line:** `Saturday 14 March · 10am–1pm`\n" +
		"* **Cut-off or Overlapping Text:** None. All text is fully legible, well-spaced, and fits neatly within the inner frame."
	// flyerQuestion is the question run 15 put to the look.
	flyerQuestion = "What date and time does this flyer show for the open day? Read the date line verbatim. Any cut-off or overlapping text?"
)

// shellTool is a step the approval floor cannot call read-only, as every
// canvas write and read is: a shell line. It answers with a fresh line each
// time, so the ledger rule beside the silent ladder is not exercised.
func shellTool(name string, fails bool) bare.Tool {
	var ran atomic.Int64
	return bare.Tool{
		Name: name, Description: "runs a canvas command",
		Schema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			n := ran.Add(1)
			if fails {
				return fmt.Sprintf("usage: unknown flag --id for `canvas put` (%d)", n), true, nil
			}
			return fmt.Sprintf(`{"id":"n_czk6rlc2xoel","seq":%d}`, 20+n), false, nil
		},
	}
}

// seenTool answers the way `view_image` does: "seen by <model>: <words>".
func seenTool(name, seen string) bare.Tool {
	return bare.Tool{
		Name: name, Description: "asks a question of a picture",
		Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"question":{"type":"string"}}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return "seen by stub/vision: " + seen, false, nil
		},
	}
}

// glanceTool answers the way a `read` of an image does: a description, with
// nothing asked of it.
func glanceTool(name string) bare.Tool {
	return bare.Tool{
		Name: name, Description: "describes a picture",
		Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return "[vision: stub/vision]\nA 1280×800 photo of a latte and lemon cake on a dark counter; no text on it.", false, nil
		},
	}
}

// The calls a scripted turn makes, each a quiet tool-using reply.
func shellStep(index int) step {
	return callStep(fmt.Sprintf("sh-%d", index), "shell", fmt.Sprintf(`{"command":"canvas show cv_dqcxhylk4i32 # %d"}`, index))
}

func failedStep(index int) step {
	return callStep(fmt.Sprintf("put-%d", index), "put", fmt.Sprintf(`{"command":"canvas put cv_dqcxhylk4i32 --id n_czk6rlc2xoel # %d"}`, index))
}

func checkStep(index int) step {
	arguments, _ := json.Marshal(map[string]string{"path": fmt.Sprintf("look-%d.png", index), "question": flyerQuestion})
	return callStep(fmt.Sprintf("see-%d", index), "see", string(arguments))
}

func glanceStep(index int) step {
	return callStep(fmt.Sprintf("glance-%d", index), "glance", fmt.Sprintf(`{"path":"ad-look-%d.png"}`, index))
}

// quietTurn pads a script's calls with shell reads AT THE FRONT to exactly the
// silent ladder's first rung, so the `[silent]` note lands right after the
// script's own last call, as it did in the measured turns.
func quietTurn(t *testing.T, tail ...step) []step {
	t.Helper()
	pad := silentThreshold(0) - len(tail)
	if pad < 0 {
		t.Fatalf("the script has %d calls; the first rung is at %d", len(tail), silentThreshold(0))
	}
	steps := make([]step, 0, silentThreshold(0))
	for index := 0; index < pad; index++ {
		steps = append(steps, shellStep(100+index))
	}
	return append(steps, tail...)
}

func noteCheckAgent(t *testing.T, steps []step) (*Agent, *scriptedCompleter) {
	t.Helper()
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer,
		shellTool("shell", false), shellTool("put", true), seenTool("see", flyerSeen), glanceTool("glance"))
	return agent, completer
}

// A FINISHED EDIT THAT THE TURN CHECKED IS NOT SENT BACK, HOWEVER SHORT. Each
// replay is the measured false positive after #2834: the flyer patched, a look
// of it asked whether the date reads right and said it does, the `[silent]`
// note, and the finished two-paragraph reply — which is where the turn now
// ends, with no dim line saying it stopped early.
func TestAFinishedEditTheTurnCheckedIsNotSentBack(t *testing.T) {
	t.Run("run 15: the check is the last call", func(t *testing.T) {
		steps := quietTurn(t, failedStep(0), shellStep(1), shellStep(2), checkStep(3))
		quiet := len(steps)
		steps = append(steps, replyStep(flyerFixedRun15), unreachable)
		agent, completer := noteCheckAgent(t, steps)
		collected := runTurnOn(t, agent, "date moved to sat 14 march, fix the flyer")

		if !readsAsAShortNote(flyerFixedRun15) {
			t.Fatal("the fixture no longer has the shape of a note; it would not exercise the check")
		}
		asked := completer.request(quiet)
		if last := asked[len(asked)-1]; last.Role != "user" || !strings.HasPrefix(messageText(last), silentNoteOpening) {
			t.Fatalf("the reply answered %s %q; want the silent ladder's note", last.Role, messageText(last))
		}
		if got, want := completer.requests(), quiet+1; got != want {
			t.Fatalf("the loop made %d requests; want %d — the checked reply ends the turn", got, want)
		}
		if saidSomething(noticeTexts(collected), checkpointNotesNudgeNote) {
			t.Errorf("a checked, finished reply was sent back; notices were %q", noticeTexts(collected))
		}
		if got := lastAssistantText(agent); got != flyerFixedRun15 {
			t.Errorf("the turn ended on %q; want the finished reply", got)
		}
	})
	// RUN 14 READ THE FLYER'S TEXT TWICE AFTER ITS LOOK. Reading after a check
	// does not undo it. Those reads are shell lines, so the reply check's
	// stale-look arm (replycheck.go) may still ask for one more look; that is
	// its own guard and is answered here with the token. What this pins is that
	// the request for a note no longer sends the reply back.
	t.Run("run 14: reads after the check", func(t *testing.T) {
		steps := quietTurn(t, shellStep(0), checkStep(1), shellStep(2), shellStep(3))
		quiet := len(steps)
		steps = append(steps, replyStep(flyerFixedRun14), replyStep(NoChangeReply), unreachable)
		agent, completer := noteCheckAgent(t, steps)
		collected := runTurnOn(t, agent, "date moved to sat 14 march, fix the flyer")

		if !readsAsAShortNote(flyerFixedRun14) {
			t.Fatal("the fixture no longer has the shape of a note; it would not exercise the check")
		}
		asked := completer.request(quiet)
		if last := asked[len(asked)-1]; last.Role != "user" || !strings.HasPrefix(messageText(last), silentNoteOpening) {
			t.Fatalf("the reply answered %s %q; want the silent ladder's note", last.Role, messageText(last))
		}
		if saidSomething(noticeTexts(collected), checkpointNotesNudgeNote) {
			t.Errorf("a checked, finished reply was sent back; notices were %q", noticeTexts(collected))
		}
		if strings.Contains(transcriptText(agent), checkpointNotesNudgeLead) {
			t.Errorf("the notes continuation reached the record:\n%s", transcriptText(agent))
		}
		if !strings.Contains(transcriptText(agent), "assistant: "+flyerFixedRun14) {
			t.Errorf("the finished reply is gone from the record:\n%s", transcriptText(agent))
		}
	})
}

// A STALL IS STILL SENT BACK WHEN NOTHING IT CHANGED WAS CHECKED. Each row is a
// measured or adjacent shape of the stop #2831 was written for, and in each the
// short note is sent back once and the turn goes on to finish.
func TestANoteThatDidNotCheckWhatItChangedIsStillSentBack(t *testing.T) {
	const stalledSpringAd = "Learned: the Spring ad is a 1280×800 photo — latte and lemon cake on a dark counter, rainy harbour window left, " +
		"teal wall and plants right. No text on it yet. Next I'm checking whether the logo PNG has a transparent background."
	const stalledCafe = "Where things stand: the Home and Visit pages exist. Next I'm stripping the duplicate sections."
	for name, row := range map[string]struct {
		calls func() []step
		note  string
	}{
		// Baseline, pipeline-dashboard t1: built a Summary tab, never looked.
		"changed and never looked": {func() []step { return quietTurn(t, shellStep(0)) }, stalledNote},
		// Run 5, spring-ad t1: looked at the picture with `read`, which asks nothing.
		"looked without asking anything": {func() []step { return quietTurn(t, glanceStep(1), glanceStep(2)) }, stalledSpringAd},
		// Asked a question of the work before changing any of it: the check
		// comes first of all, and the shell lines after it are unchecked.
		"checked before it changed anything": {func() []step {
			return append([]step{checkStep(0)}, quietTurn(t, shellStep(1))[1:]...)
		}, stalledCafe},
		// Run 2, cafe-site t1: checked, then its next steps failed.
		"checked, then a step failed": {func() []step { return quietTurn(t, checkStep(1), failedStep(2)) }, stalledCafe},
	} {
		t.Run(name, func(t *testing.T) {
			steps := row.calls()
			if len(steps) != silentThreshold(0) {
				t.Fatalf("the script has %d calls; want the first rung, %d", len(steps), silentThreshold(0))
			}
			quiet := len(steps)
			final := "Done: the work the note named is built."
			steps = append(steps,
				replyStep(row.note),
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponseWithText("sh-next", "shell", `{"command":"canvas patch next"}`, "[update] Doing it now."), nil
				},
				replyStep(final),
				replyStep(NoChangeReply),
				unreachable,
			)
			agent, completer := noteCheckAgent(t, steps)
			collected := runTurnOn(t, agent, "make it")

			asked := completer.request(quiet)
			if last := asked[len(asked)-1]; last.Role != "user" || !strings.HasPrefix(messageText(last), silentNoteOpening) {
				t.Fatalf("the note answered %s %q; want the silent ladder's note", last.Role, messageText(last))
			}
			next := completer.request(quiet + 1)
			if last := next[len(next)-1]; last.Role != "user" || messageText(last) != checkpointNotesNudgeLead {
				t.Fatalf("the request after the note ended on %s %q; want the notes continuation", last.Role, messageText(last))
			}
			if !saidSomething(noticeTexts(collected), checkpointNotesNudgeNote) {
				t.Errorf("the stall was not sent back; notices were %q", noticeTexts(collected))
			}
			if !strings.Contains(transcriptText(agent), "assistant: "+final) {
				t.Errorf("the turn never reached its final answer:\n%s", transcriptText(agent))
			}
		})
	}
}

// A HELD BATCH IS WORK NOT DONE. The write-your-notes rule's `[held]` demand
// stands in for calls that never ran, so a check before them does not finish
// the turn, and the note written in answer is sent back as #2831 sends it.
func TestACheckBeforeAHeldBatchDoesNotFinishTheTurn(t *testing.T) {
	rung := enforcedRungAt()
	steps := []step{shellStep(0), checkStep(1)}
	for index := 2; index <= rung; index++ {
		steps = append(steps, shellStep(index))
	}
	steps = append(steps,
		replyStep("Where things stand: the Home and Visit pages exist. Next I'm stripping the duplicate sections."),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponseWithText("sh-next", "shell", `{"command":"canvas patch next"}`, "[update] Stripping the duplicates."), nil
		},
		replyStep("The cafe site is done: Home and Visit, no duplicates."),
		replyStep(NoChangeReply),
		unreachable,
	)
	agent, _ := noteCheckAgent(t, steps)
	collected := runTurnOn(t, agent, "can we get a simple website for the cafe")
	if len(heldToolResults(agent)) != 1 {
		t.Fatalf("the fixture never reached the held rung: %v", heldToolResults(agent))
	}
	if !saidSomething(noticeTexts(collected), checkpointNotesNudgeNote) {
		t.Errorf("the note answering the held batch was not sent back; notices were %q", noticeTexts(collected))
	}
}
