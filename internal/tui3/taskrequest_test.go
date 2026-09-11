package tui3

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// This is the journal shape of one piece of a developer task, including the
// inherited request and evidence that must remain available behind the preview.
func requestFixture() string {
	return strings.Join([]string{
		taskRequestAsk + "\nThis is the message the whole job came out of, and this task is ONE PIECE of it: preserve the scope.\n\nFix issue #812: cancelled background searches leave stale results after switching projects. Reproduce the race, repair ownership, and verify the real UI.",
		"THE FOLDER THIS WORK IS ABOUT, AND YOUR OWN COPY OF IT\nRead anywhere on the machine; write only inside your copy.\n\nOriginal: /src/search\nYour copy: /worktrees/search-worker",
		"THE WORK\n\nRepair worker cancellation ownership.\nKeep completed results attached to their originating project.\nAdd a regression that switches projects while two searches overlap.",
		"WHAT TO PRODUCE\n\nA scoped worker patch and deterministic race regression.",
		"DONE WHEN\n\nThe older result cannot overwrite the newer project.\nThe race detector passes and existing cancellation tests still pass.",
		"SOME OF WHAT WAS SAID AROUND THIS WORK\n\nThe user asked to retain successful cached results.",
		"CALLS THAT HAVE ALREADY RUN\n\nread worker.go: ownership currently follows the active view.",
		"WHAT THIS BRIEF ASSUMES, AND WAS CHECKED BEFORE YOU STARTED\n\nThe worker API accepts a project ID and a cancellation token.",
		"THE PERSON'S ORIGINAL MESSAGE\n\n/session/main.jsonl:42",
	}, "\n\n")
}

func TestTaskRequestCompletedNestedJournalKeepsRequestAndAnswerReadable(t *testing.T) {
	raw := requestFixture()
	a, fake, _ := roomApp(t)
	a.workMode = config.WorkFold
	a.taskUpdate(update(1, "Fix cross-project search race", session.TaskRunning, session.TaskNotice{}))
	railKinship(a, 1, 7)
	fake.journal = roomJournal(t,
		`{"type":"message","role":"user","note":true,"content":`+strconv.Quote(raw)+`}`,
		`{"type":"message","role":"assistant","content":"Tracing the cancellation boundary.","toolCalls":[{"id":"read-worker","function":{"name":"read","arguments":"{\"path\":\"worker.go\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"read-worker","content":"ownership currently follows the active view"}`,
		`{"type":"message","role":"assistant","content":"Testing overlapping searches.","toolCalls":[{"id":"test-worker","function":{"name":"bash","arguments":"{\"command\":\"go test -race ./worker\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"test-worker","content":"PASS: overlapping cancellation preserves project ownership"}`,
		`{"type":"message","role":"assistant","content":"## Search ownership repaired\n\nResults now retain their originating project. The overlap regression and race detector pass."}`)
	a.openRoom(7, "Repair worker cancellation ownership")
	landRoom(t, a)
	a.touch()
	if !strings.Contains(a.roomTrail(), "Fix cross-project search race") {
		t.Fatalf("fixture lost the parent: %s", a.roomTrail())
	}
	if e := a.room.entries[0]; e.kind != entryUser || !e.brief || e.text != raw {
		t.Fatalf("canonical task request was not retained intact: %#v", e)
	}
	preview := strings.Join(briefBodyRows(a), "\n")
	if len(briefBodyRows(a)) != 3 || !strings.Contains(preview, "Repair worker cancellation ownership") || strings.Contains(preview, "issue #812") {
		t.Fatalf("preview should lead with this task's three lines:\n%s", preview)
	}
	page := roomText(a)
	if !strings.Contains(page, "Search ownership repaired") || strings.Contains(page, "Tracing the cancellation boundary") {
		t.Fatalf("completed task should expose its answer and fold intermediate narration:\n%s", page)
	}
	if _, ok := briefDoor(a); !ok {
		t.Fatal("canonical request has no disclosure")
	}
	a.roomScroll(-10000)
	pressBriefDoor(t, a)
	page = roomText(a)
	for _, want := range []string{"Task request", "Original request", "Workspace", "Deliverable", "Completion criteria", "Conversation context", "Prior evidence", "Verified assumptions", "Original message reference", "ONE PIECE", "issue #812", "successful cached results", "cancellation token", "/session/main.jsonl:42"} {
		if !strings.Contains(page, want) {
			t.Errorf("expanded request omitted %q", want)
		}
	}
	if a.room.entries[0].text != raw {
		t.Fatal("display expansion mutated the stored request")
	}
	display := requestDisplayText(&a.room.entries[0])
	for _, line := range strings.Split(raw, "\n") {
		// Only the canonical all-capital labels are replaced. Every original
		// body line, including the model's context rules, remains available.
		if line != "" && strings.ToUpper(line) != line && !strings.Contains(display, line) {
			t.Errorf("display omitted original request content: %q", line)
		}
	}
	drive(t, a, key(briefFoldKey))
	if len(briefBodyRows(a)) != 3 {
		t.Fatal("keyboard did not collapse the same request opened by the pointer")
	}
}

func TestTaskRequestReplayLeavesLaterNotesCorrectionsAndMainAlone(t *testing.T) {
	a, _, _ := roomApp(t)
	raw := requestFixture()
	entries := []session.DisplayEntry{
		{Role: "aside", Text: raw},
		{Role: "assistant", Text: "Investigating ownership."},
		{Role: "aside", Text: "Task 9 finished.\nInternal checkpoint detail."},
		{Role: "user", Text: "Please retain completed cached results.\nDo not reset the whole view."},
		{Role: "user", Text: "Only cancel requests from the outgoing project.", Steer: &session.SteerMark{Consumed: true}},
		{Role: "aside", Text: "THE WORK\n\nA later internal note is not a new request."},
	}
	blocks, turns := a.replayBlocks(entries, roomReplay(0))
	if len(blocks) != len(entries) || turns != 2 {
		t.Fatalf("replay changed message/turn count: %d/%d", len(blocks), turns)
	}
	if blocks[2].kind != entryNote || blocks[2].text != "Task 9 finished." || blocks[2].brief || blocks[5].kind != entryNote || blocks[5].brief {
		t.Fatalf("later engine notes acquired request semantics: %#v / %#v", blocks[2], blocks[5])
	}
	if blocks[3].brief || requestDisplayText(&blocks[3]) != entries[3].Text || blocks[4].kind != entrySteer || blocks[4].steer.words != entries[4].Text {
		t.Fatal("the person's later message or correction changed")
	}
	main, _ := a.replayBlocks(entries, chatReplay(0))
	if main[0].kind != entryNote || main[0].brief || main[0].text != taskRequestAsk {
		t.Fatalf("main replay adopted the room-only request treatment: %#v", main[0])
	}
	for _, text := range []string{"THE WORK is still unfinished", "A normal task update", "WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS\nUnrelated prose"} {
		if canonicalTaskRequest(text) {
			t.Errorf("ordinary prose matched canonical request: %q", text)
		}
	}
}

func TestTaskRequestUnicodeDisclosureCountsDisplayWrapAndPreservesContent(t *testing.T) {
	raw := "THE WORK\n\n" + strings.Repeat("修復 検索 👩🏽‍💻 cafe\u0301 project-identity ", 50) + "TAILEND\n\nDONE WHEN\n\n検証が完了する。"
	for _, width := range []int{28, 60, 120} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			a := briefRoom(t, raw)
			a.width = width
			a.touch()
			display := requestDisplayText(&a.room.entries[0])
			all := briefWrapped(a, display)
			door, ok := briefDoor(a)
			if !ok || !strings.Contains(plain(door.text), strconv.Itoa(len(all)-briefFoldLines)+" more lines") {
				t.Fatalf("disclosure doesn't count displayed Unicode wraps: %q", plain(door.text))
			}
			drive(t, a, key(briefFoldKey))
			var rendered strings.Builder
			for _, r := range briefRows(a) {
				if r.entry != 0 || r.hit == hitBrief {
					continue
				}
				if cells := ansi.StringWidth(plain(r.text)); cells > a.bodyWidth() {
					t.Errorf("request row overflows: %d cells in %d", cells, a.bodyWidth())
				}
				rendered.WriteString(plain(r.text))
			}
			if !strings.Contains(rendered.String(), "TAILEND") || a.room.entries[0].text != raw || !strings.Contains(display, "検証が完了する。") {
				t.Fatal("expanded Unicode request lost its tail or mutated the source")
			}
		})
	}
}

func TestTaskRequestLongRecordRetainsBriefSeamAndLatestAnswerWithinBudget(t *testing.T) {
	a, _, _ := roomApp(t)
	raw := requestFixture()
	entries := []session.DisplayEntry{{Role: "aside", Text: raw}}
	for i := 0; i < 180; i++ {
		entries = append(entries, session.DisplayEntry{Role: "assistant", Text: "Investigation step " + strconv.Itoa(i)})
	}
	entries = append(entries, session.DisplayEntry{Role: "assistant", Text: "FINAL: overlap regression and race detector pass."})
	for _, compacted := range []bool{false, true} {
		t.Run(strconv.FormatBool(compacted), func(t *testing.T) {
			record := session.Record{Entries: entries}
			if compacted {
				record.Earlier, record.Entries = entries[:60], entries[60:]
			}
			blocks, _ := a.roomRecord(record, 120)
			if len(blocks) != 120 || !blocks[0].brief || blocks[0].text != raw || blocks[1].kind != entrySeam || blocks[1].text != "Earlier work is outside this saved view." {
				t.Fatalf("bounded room lost its retained request or omission seam: length=%d first=%#v second=%#v", len(blocks), blocks[0], blocks[1])
			}
			if blocks[len(blocks)-1].text != entries[len(entries)-1].Text || blocks[2].text != "Investigation step 63" {
				t.Fatalf("retained tail is not the most recent 118 entries: %q ... %q", blocks[2].text, blocks[len(blocks)-1].text)
			}
		})
	}
	plainBlocks := []entry{{kind: entryAssistant, text: "one"}, {kind: entryAssistant, text: "two"}, {kind: entryAssistant, text: "three"}, {kind: entryAssistant, text: "four"}}
	if got := keepRoomTail(plainBlocks, 3); len(got) != 3 || got[0].text != "two" {
		t.Fatalf("ordinary tail gained synthetic request furniture: %#v", got)
	}
}

func TestTaskRequestWithoutSeparateWorkStartsWithTheActualRequest(t *testing.T) {
	raw := taskRequestAsk + "\nThis is the message this work came out of. Their words govern.\n\nFix cancellation without discarding completed results.\n\nWHAT TO PRODUCE\n\nA patch and regression test.\n\nDONE WHEN\n\nAll overlapping searches settle correctly."
	e := entry{brief: true, text: raw}
	shown := requestDisplayText(&e)
	if !strings.HasPrefix(shown, "Original request\nFix cancellation") {
		t.Fatalf("a deduplicated assignment opened on metadata: %s", shown)
	}
	if e.text != raw || !strings.Contains(shown, "Their words govern.") {
		t.Fatal("presentation changed or lost original context")
	}
}
