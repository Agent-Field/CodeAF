package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── the run 24 replays ──────────────────────────────────────────────────────
//
// testdata/run24-replies holds five turns of the connected-canvas qloop's run
// 24 (deepseek-v4.1-flash, engine e85f90ba1), each from the person's message
// to the reply the turn first ended on, exactly as the session journal kept
// them. Two things are left out: the canvas skill's own pages that a step read
// (the check never offers them), and the instructions the harness appended to
// the person's message. Every one of the five replies told the person
// something the work did not show, and every one passed the reply check:
//
//   - spring-ad-t2: "both over the dark table clear of the cup and cake",
//     over a headline band that covers the cup. Nothing the turn saw placed
//     the cup, so nothing contradicted it.
//   - pipeline-t2: "set the three charts to a row of equal width so the row
//     stays full", after a look that said the right half of that row is empty
//     and before a patch to a different chart's callout, which retired it as
//     (earlier).
//   - cafe-site-t3: "no names or signage", after one look had read "Harbour
//     Café" off a shopfront and a later look at a crop of the picture said
//     there was no lettering.
//   - vector-brief-t1: "The brief renders clean", after three looks had
//     described the References lines printing raw citation markup, all of them
//     (earlier) by the time the reply was written.
//   - sales-dashboard-t1: "net revenue $439k in February" over a takeaway the
//     look read as "439 ($k)", on a stale turn whose outputs after the look
//     shared no word with the reply — so it went to look again with no reader,
//     and the model answered [no change].

func run24Turn(t *testing.T, name string) []ai.Message {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "run24-replies", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var messages []ai.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) < 2 || messages[0].Role != "user" || messages[len(messages)-1].Role != "assistant" {
		t.Fatalf("%s is not one turn from the person's message to a reply", name)
	}
	return messages
}

// run24Page is the reader's page for a replayed turn, built the way
// [Agent.replyLooksBack] builds it.
func run24Page(messages []ai.Message) (said, page string, stale bool) {
	said = strings.TrimSpace(partsText(messages[len(messages)-1]))
	looks, stale := lookedAtTheWork(messages)
	page = replyCheckPage(said, looks, relevantObservations(said, observedThisTurn(messages)), changedThisTurn(messages))
	return said, page, stale
}

// THE EVIDENCE THAT SHOWED EACH REPLY WRONG REACHES THE READER, AND SO DOES WHAT
// CAME BETWEEN. The pages these turns were read with in run 24 carried no
// CHANGE lines, so "(earlier)" could not be told apart from "superseded"; every
// step line opened on the same `cd` into the working folder, which ate the
// bound before the command was reached; and the pipeline turn's page carried a
// grep over the canvas skill's own page as evidence about the work.
func TestRun24PagesCarryWhatShowedTheReplyWrong(t *testing.T) {
	for name, row := range map[string]struct {
		claim string
		// shows is every line the page must carry, in this order.
		shows []string
		stale bool
	}{
		"spring-ad-t2": {
			claim: "clear of the cup and cake",
			shows: []string{"OUTPUT (earlier) of bash CID=cv_3huwggjzl3fl PIC=f_3y37hg3jxhj6 canvas patch", "LOOK:", "Spring menu"},
		},
		"pipeline-t2": {
			claim: "row of equal width so the row stays full",
			shows: []string{
				"left completely blank/empty",
				`CHANGE: bash CV=cv_5fe3dbs2j6sx canvas patch "$CV" f_4bx4z3ejuup3 --json '{"chart"`,
				"LOOK (asked: \"Focus on the third chart",
			},
		},
		"cafe-site-t3": {
			claim: "no names or signage",
			shows: []string{`labelled "Harbour Café"`, "CHANGE: bash python3", "There is no text or lettering visible"},
		},
		"vector-brief-t1": {
			claim: "The brief renders clean",
			shows: []string{
				"LOOK (earlier) (asked: \"(1) In the benchmark table, list the Store column",
				"(canvas-cite:f_7wtm4ou3zj5b#)",
				"OUTPUT (earlier) of bash CV=cv_leztya3v7wrx; DOC=f_q2jacrpf7bgh canvas patch",
				"LOOK (earlier) (asked: \"(1) In the benchmark table, is 'pgvector' now on one line",
			},
			stale: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			said, page, stale := run24Page(run24Turn(t, name))
			if !strings.Contains(said, row.claim) {
				t.Fatalf("the replayed reply does not say %q", row.claim)
			}
			if stale != row.stale {
				t.Errorf("stale = %v; want %v", stale, row.stale)
			}
			at := 0
			for _, line := range row.shows {
				next := strings.Index(page[at:], line)
				if next < 0 {
					t.Fatalf("the page does not carry %q after byte %d:\n%s", line, at, page)
				}
				at += next + len(line)
			}
			if strings.Contains(page, " of bash cd ") || strings.Contains(page, "CHANGE: bash cd ") {
				t.Errorf("a step line still opens on its cd into the working folder")
			}
			if !strings.Contains(page, "\nCHANGE: ") && !strings.HasPrefix(page, "CHANGE: ") {
				t.Errorf("the page names no step that may have changed the work")
			}
			if strings.Contains(strings.ToLower(page), "skill.md") {
				t.Errorf("the canvas skill's own page reached the reader as evidence")
			}
			if limit := replyCheckReplyBytes + replyCheckLookBytes + replyEvidenceBytes + replyCheckChangeBytes + 2000; len(page) > limit {
				t.Errorf("the page is %d bytes; want at most %d", len(page), limit)
			}
		})
	}
}

// replayAgent is an agent whose transcript is a replayed turn, and whose reader
// answers from the page it is shown.
func replayAgent(t *testing.T, messages []ai.Message, read func(page string) string) (*Agent, *atomic.Int64, *[]Event) {
	t.Helper()
	var asked atomic.Int64
	completer := &scriptedCompleter{aside: func(sent []ai.Message) (*ai.Response, bool) {
		if !isReplyCheckCall(sent) {
			return nil, false
		}
		asked.Add(1)
		return textResponse(read(messageText(sent[0]))), true
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.RolesSource = tierSettings(map[string]string{roles.PinKey(roles.RoleReplyCheck): "check/model"})
	})
	for _, message := range messages {
		agent.record(message)
	}
	return agent, &asked, &[]Event{}
}

// lookBack runs the guard on the replayed turn's reply and returns what it
// wrote: whether it sent the turn back, the continuation, and the notices.
func lookBack(t *testing.T, agent *Agent, messages []ai.Message) (bool, string, []string) {
	t.Helper()
	var events []Event
	hub := &eventHub{observe: func(event Event) { events = append(events, event) }}
	said := strings.TrimSpace(partsText(messages[len(messages)-1]))
	sent := agent.replyLooksBack(context.Background(), hub, &checkpointMeter{}, said)
	snapshot := agent.snapshot()
	lead := ""
	if last := snapshot[len(snapshot)-1]; last.Role == "user" {
		lead = messageText(last)
	}
	return sent, lead, noticeTexts(events)
}

// THE STALE TURN IS READ BEFORE IT IS SENT TO LOOK AGAIN. In run 24 the bare
// stale arm sent sales-dashboard-t1 back with no reader, because no output
// after its look shared a word with the reply; the model answered [no change]
// and "$439k" went to the person over a takeaway the look read as "439 ($k)".
// Now the reader is asked first, and what it raises is the send-back.
func TestRun24StaleReplyIsReadBeforeItIsSentToLookAgain(t *testing.T) {
	messages := run24Turn(t, "sales-dashboard-t1")
	finding := `- WRONG: "net revenue $439k in February" — the look reads the takeaway as "Net revenue was 439 ($k)".`
	agent, asked, _ := replayAgent(t, messages, func(page string) string {
		if strings.Contains(page, "439 ($k)") && strings.Contains(page, "$439k") {
			return finding
		}
		return "NONE"
	})
	sent, lead, notices := lookBack(t, agent, messages)
	if !sent {
		t.Fatal("the turn was not sent back")
	}
	if asked.Load() != 1 {
		t.Fatalf("the reader was asked %d times; want 1 — a stale turn is read first", asked.Load())
	}
	if lead == replyStaleLookLead || !isReplyCheckLead(lead) || !strings.Contains(lead, strings.TrimPrefix(finding, "- WRONG: ")) {
		t.Fatalf("the send-back was %q; want the reader's line, not the bare look-again", lead)
	}
	if !saidSomething(notices, replyCheckNote) {
		t.Errorf("notices were %q", notices)
	}
}

// AND A STALE TURN THE READER CLEARS IS STILL SENT TO LOOK AGAIN: reading it
// first adds a check, it removes none.
func TestRun24StaleReplyTheReaderClearsStillLooksAgain(t *testing.T) {
	messages := run24Turn(t, "sales-dashboard-t1")
	agent, asked, _ := replayAgent(t, messages, func(string) string { return "NONE" })
	sent, lead, notices := lookBack(t, agent, messages)
	if !sent || lead != replyStaleLookLead {
		t.Fatalf("sent %v with %q; want the stale arm", sent, lead)
	}
	if asked.Load() != 1 || !saidSomething(notices, replyStaleLookNote) {
		t.Errorf("asked %d, notices %q", asked.Load(), notices)
	}
}

// WHAT NOTHING SHOWED IS SENT BACK TO BE CHECKED OR SAID PLAINLY. spring-ad-t2
// said the headline was "clear of the cup and cake" on a turn whose look never
// placed the cup. A contradiction was the only thing the reader could raise,
// and there was none. Now the reader's UNSHOWN line sends the turn back under
// its own heading and its own note: the reply is not known to be wrong, it said
// more than the turn checked.
func TestRun24UnshownPromiseIsSentBackToBeCheckedOrSaidPlainly(t *testing.T) {
	messages := run24Turn(t, "spring-ad-t2")
	finding := `- UNSHOWN: "clear of the cup and cake" — nothing looked at whether the bands cover the cup or the cake.`
	agent, asked, _ := replayAgent(t, messages, func(page string) string {
		if strings.Contains(page, "clear of the cup and cake") {
			return finding
		}
		return "NONE"
	})
	sent, lead, notices := lookBack(t, agent, messages)
	if !sent || asked.Load() != 1 {
		t.Fatalf("sent %v, reader asked %d times; want sent, once", sent, asked.Load())
	}
	want := replyCheckUnshownHeading + strings.Replace(finding, "UNSHOWN: ", "", 1)
	if !isReplyCheckLead(lead) || !strings.Contains(lead, want) || strings.Contains(lead, replyCheckWrongHeading) {
		t.Fatalf("the send-back was %q; want the line under %q only", lead, replyCheckUnshownHeading)
	}
	if !strings.Contains(lead, "tell the person plainly that you could not check it") {
		t.Errorf("the send-back does not offer saying it plainly: %q", lead)
	}
	if !saidSomething(notices, replyUnshownNote) || saidSomething(notices, replyCheckNote) {
		t.Errorf("notices were %q; want the unshown note alone", notices)
	}
}

// THE ASK NAMES WHAT LET EACH OF THE FOUR THROUGH. These are the reader's
// instructions, and a respelling that drops one reopens the hole it closed;
// the live measurement (replyrun24_live_test.go) is what shows they work.
func TestRun24AskClosesEachHole(t *testing.T) {
	for _, clause := range []string{
		"still holds for anything no later entry covers, unless a CHANGE after it plainly altered that very thing",
		"neither settles it",
		"a LOOK outranks an OUTPUT that says how something was set up",
		"UNSHOWN — it tells the person the work is free of a problem",
		"one entry that reports the problem is enough to raise it",
		"never raise a statement only because the evidence does not mention it",
	} {
		if !strings.Contains(replyCheckAsk, clause) {
			t.Errorf("the reader's ask no longer says %q", clause)
		}
	}
}
