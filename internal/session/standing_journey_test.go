package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ── THE CHAT-DRIVEN LOCAL-FILE JOURNEY, ONE CHANGE AT A TIME (W5-B) ─────────
//
// The chat's edit (standing_edit.go) was pinned for its instructions, a declined
// watch and a move. The two changes a person makes to a digest most after its
// instructions — where its report is kept and what it watches — had no test that
// answered the card yes and then ran the work. These do, through the runner the
// pass uses.

// editAnswered sends one edit of item through the conversation, answers its
// card yes, and hands back the card and the stand tool's answer.
func (d *chatDoor) editAnswered(t *testing.T, said string, fields map[string]any) (*StandingNotice, string) {
	t.Helper()
	body := map[string]any{"op": "edit", "id": d.only(t).ID}
	for key, value := range fields {
		body[key] = value
	}
	call, _ := json.Marshal(body)
	d.agent.client = &scriptedCompleter{steps: []step{standCall("e1", string(call)), finalText("changed")}}
	var card *StandingNotice
	events := d.submitAnswering(t, said, func(event Event) {
		card = event.Standing
		d.yes(event)
	})
	return card, toolOutput(t, events, "stand")
}

// A REPORT PATH CHANGED IN THE CHAT IS THE SAME WORK KEEPING ANOTHER FILE. The
// card draws the path old → new, the yes revises the item in place, the next
// run publishes at the new path, the file it used to keep is left exactly as it
// was, and the old path has no owner any more while the new one does.
//
// Journey coverage, not a regression: it passes on the head before W5-B too.
func TestAReportPathChangedInTheChatPublishesThereAndFreesTheOldPath(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	d.proposeInbox(t, d.yes)
	first := d.only(t)
	if outcome := fireReport(t, d.store, first, 1, "# Inbox\n- the first report"); outcome.Published == nil {
		t.Fatalf("the first report was not published: %+v", outcome)
	}
	oldFile := filepath.Join(d.project, "reports", "inbox-report.md")
	kept, _ := os.ReadFile(oldFile)

	card, out := d.editAnswered(t, "keep it at reports/weekly.md from now on", map[string]any{"does": map[string]any{"report": "reports/weekly.md"}})
	if card == nil {
		t.Fatalf("no card was drawn for the report change; the tool said %q", out)
	}
	var line string
	for _, term := range card.Terms {
		if strings.HasPrefix(term, "report · ") {
			line = term
		}
	}
	if !strings.HasPrefix(line, "report · reports/inbox-report.md") || !strings.Contains(line, " → reports/weekly.md") {
		t.Fatalf("the card does not draw the report path old → new: %q", card.Terms)
	}
	after := d.only(t)
	if after.ID != first.ID || after.SpecRevision != 2 || after.Does.Report != "reports/weekly.md" {
		t.Fatalf("the change made %s version %d keeping %q, want %s version 2 keeping reports/weekly.md", after.ID, after.SpecRevision, after.Does.Report, first.ID)
	}
	if outcome := fireReport(t, d.store, after, 2, "# Inbox\n- kept at its new path"); outcome.Published == nil || outcome.Withheld != "" {
		t.Fatalf("the run after the change did not publish at the new path: %+v", outcome)
	}
	if raw, _ := os.ReadFile(filepath.Join(d.project, "reports", "weekly.md")); string(raw) != "# Inbox\n- kept at its new path\n" {
		t.Fatalf("the new path holds %q", raw)
	}
	if raw, _ := os.ReadFile(oldFile); string(raw) != string(kept) {
		t.Fatalf("the file the work used to keep was changed: %q", raw)
	}
	if owner, live := d.store.LiveOwner(oldFile); live {
		t.Fatalf("the path the report left is still owned by %s", owner.ID)
	}
	if owner, live := d.store.LiveOwner(filepath.Join(d.project, "reports", "weekly.md")); !live || owner.ID != first.ID {
		t.Fatalf("the new path is not owned by the work that moved there (%s, %v)", owner.ID, live)
	}
}

// A WATCH CHANGED IN THE CHAT REACHES WHAT ITS CARD SAYS. The pattern that
// stands after the yes reaches the subfolders it was widened to and still
// reaches what it reached before, and the model is told the card's line. It is
// journey coverage, not a regression: it passes on the head before W5-B too.
func TestAWatchChangedInTheChatReachesItsSubfolders(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	d.proposeInbox(t, d.yes)
	first := d.only(t)
	card, out := d.editAnswered(t, "pick up the client subfolders too", map[string]any{"when": map[string]any{"kind": "file", "glob": "inbox/**"}})
	if card == nil {
		t.Fatalf("no card was drawn for the watch change; the tool said %q", out)
	}
	if card.WhenWords != "when inbox/* changes → when inbox/** changes" {
		t.Fatalf("the when band reads %q", card.WhenWords)
	}
	after := d.only(t)
	if after.ID != first.ID || after.SpecRevision != 2 || !after.Watches("inbox/acme/new.md") || !after.Watches("inbox/top.md") {
		t.Fatalf("the watch that stands is %q at version %d on %s", after.When.Glob, after.SpecRevision, after.ID)
	}
	if !strings.Contains(out, "\nwhen · when inbox/* changes → when inbox/** changes\n") {
		t.Fatalf("the model was not told the when line the card drew: %q", out)
	}
}

// A FILE WATCH IS SAID FROM ITS PATTERN, NEVER FROM THE MODEL'S WORDS. One order
// watches one pattern, and "keep an eye on inbox/ and notes/" sent as `inbox/*`
// with when_words "whenever something lands in inbox/ or notes/" drew exactly
// those words on the card and in the stand result: a yes to a watch on notes/
// that never wakes for it. The card, the result and the record now say the
// pattern, as the terminal writes it.
func TestAFileWatchIsSaidFromItsPatternNotTheModelsWords(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"when_words": "whenever something lands in inbox/ or notes/"})), finalText("set up")}}, nil)
	card, events := d.proposeInbox(t, d.yes)
	item := d.only(t)
	if item.Watches("notes/n.md") {
		t.Fatal("the fixture's watch reaches notes/")
	}
	if card.WhenWords != "when inbox/* changes" {
		t.Fatalf("the card says %q over a watch that reaches only inbox/", card.WhenWords)
	}
	if out := toolOutput(t, events, "stand"); strings.Contains(out, "notes/") || !strings.Contains(out, "\nwhen · when inbox/* changes\n") {
		t.Fatalf("the model was told %q", out)
	}
	if item.When.Words != standing.WatchWords("inbox/*") {
		t.Fatalf("the record keeps %q", item.When.Words)
	}
	// An edit that sends only other words for the same watch changes nothing.
	edit, _ := json.Marshal(map[string]any{"op": "edit", "id": item.ID, "when_words": "whenever inbox/ or notes/ changes"})
	if text, failed, err := d.agent.standTool(context.Background(), edit); err != nil || !failed || !strings.Contains(text, "a file watch is said by its pattern, so when_words change nothing on it") {
		t.Fatalf("an edit of a watch's words alone answered %q (failed %v, %v)", text, failed, err)
	}
	// A watch made before this, with the model's words on its record, is said
	// from its pattern too.
	old := standing.When{Kind: standing.WhenFile, Glob: "inbox/*", Words: "whenever something lands in inbox/ or notes/"}
	if got := old.CardWords(); got != "when inbox/* changes" {
		t.Fatalf("an older watch reads %q", got)
	}
}

// AN EDIT CAN MOVE THE REPORT, AND THE MODEL IS TOLD SO WHERE IT READS THE
// FIELD. "rename the digest file: keep it at reports/weekly-digest.md" was
// answered in the live journey (W5-B live 1) with a stop and a new proposal —
// "that means a new standing item since the path is part of the key" — and the
// stop is permanent. placement's description said what it means on an edit;
// report's said nothing.
func TestTheReportFieldSaysAnEditMovesIt(t *testing.T) {
	var schema struct {
		Properties struct {
			Does struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"does"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(standSchemaJSON), &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Properties.Does.Properties["report"].Description; !strings.Contains(got, "On an edit, the path it moves to") {
		t.Fatalf("does.report never says an edit moves it: %q", got)
	}
}

// AN OPENING TAG THAT STARTS A LINE OPENS THE REPORT, ITS FIRST LINE GLUED ON.
// The W5-B acceptance run (live 3, run 000003) answered "…Let me track that I've
// processed it.\n\n<report># Digest\n…\nLAUNCH-CHECKED\n</report>", and a whole
// report was withheld `unopened-report`. A tag later in a line is still a
// mention, and a closing tag glued to words still closes nothing.
func TestAnOpeningTagGluedToTheFirstLineOpensTheReport(t *testing.T) {
	live := "New files since the previous occurrence: `inbox/clients/acme.md`. Let me track that I've processed it.\n\n<report># Digest\n\n- Clients — Acme: waiting on revised quote; Omar owns it.\n\nLAUNCH-CHECKED\n</report>"
	body, lines := delimitedReport(live)
	if lines != closedReport || body != "# Digest\n\n- Clients — Acme: waiting on revised quote; Omar owns it.\n\nLAUNCH-CHECKED" {
		t.Fatalf("the live answer reads %v with body %q", lines, body)
	}
	if _, lines := delimitedReport("I will put the digest in <report># Digest tags\n</report>"); lines != unopenedReport {
		t.Fatalf("a tag inside a sentence opened a report: %v", lines)
	}
	if _, lines := delimitedReport("<report># Digest\n- one\nLAUNCH-CHECKED</report>"); lines != unclosedReport {
		t.Fatalf("a closing tag glued to words closed a report: %v", lines)
	}
	if body, lines := delimitedReport("<report>\n# Digest\n</report>\n<report> above is this week's digest"); lines != closedReport || body != "# Digest" {
		t.Fatalf("a glued mention after a closed report reopened it: %v %q", lines, body)
	}
	if body, lines := delimitedReport("<report>\n# Digest\n</report>"); lines != closedReport || body != "# Digest" {
		t.Fatalf("a whole opening line no longer opens: %v %q", lines, body)
	}
}
