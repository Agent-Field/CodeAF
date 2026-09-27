package tui3

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"strings"
	"testing"
	"time"
)

func TestCleanChatKeepsConfirmedUpdatesAcrossLaterWork(t *testing.T) {
	es := foldFixture()
	es[5].confirmed = &responseConfirmation{done: true}
	es = append(es, entry{kind: entryTool, tool: "read", status: toolOK, turn: 1}, entry{kind: entryAssistant, text: "Final answer", settled: true, turn: 1})
	for name, folds := range map[string]map[int]workfold{"chat": deriveWorkfolds(es, 0), "room": derivePhaseFolds(es)} {
		for _, f := range folds {
			if f.start <= 5 && f.answer > 5 {
				t.Fatalf("%s hid confirmed update: %+v", name, f)
			}
		}
		stampHierarchy(es, folds)
		if es[5].demoted {
			t.Fatalf("%s demoted confirmed update", name)
		}
	}
}

func TestCleanChatImportantToolBoundariesRemainVisible(t *testing.T) {
	for _, status := range []toolState{toolConsent} {
		es := foldFixture()
		es[4].status = status
		folds := deriveWorkfolds(es, 0)
		for _, f := range folds {
			if f.start <= 4 && f.answer > 4 {
				t.Fatalf("status %v hidden: %+v", status, f)
			}
		}
		if len(folds) == 0 {
			t.Fatalf("status %v prevented preceding successful work folding", status)
		}
	}
}

func TestCleanChatDisclosureIncludesNonCaptionHistory(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = foldFixture()
	a.entries = append(a.entries[:5], entry{kind: entryNote, text: "saved operational receipt", turn: 1, settled: true}, a.entries[5])
	a.workMode = config.WorkFold
	a.touch()
	if text := strings.Join(plainRows(a), "\n"); strings.Contains(text, "saved operational receipt") {
		t.Fatal("receipt leaked from closed work")
	}
	a.toggleLatestWorkfold()
	text := strings.Join(plainRows(a), "\n")
	if !strings.Contains(text, "saved operational receipt") || !strings.Contains(text, "thought") {
		t.Fatalf("opened work lost uncaptained detail:\n%s", text)
	}
}

func TestCleanChatManagerTrafficFoldsWithoutDemotingAnswer(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = []entry{
		{kind: entryUser, text: "check the team", turn: 1},
		{kind: entryTool, tool: "team_send", status: toolOK, turn: 1, settled: true},
		{kind: entryAssistant, text: "## Ready\n\n**Review is ready.**", turn: 1, settled: true, confirmed: &responseConfirmation{done: true}},
		{kind: entryTeam, text: "private delivery", turn: 1, settled: true},
		{kind: entryCompact, text: "internal compaction", turn: 1, ended: time.Unix(100, 0)},
	}
	a.workMode = config.WorkFold
	a.touch()
	text := strings.Join(plainRows(a), "\n")
	if a.entries[2].demoted || strings.Contains(text, "## Ready") || strings.Contains(text, "private delivery") || strings.Contains(text, "internal compaction") {
		t.Fatalf("manager leaked work or raw answer markup:\n%s", text)
	}
	if !strings.Contains(text, "Review is ready.") {
		t.Fatalf("missing answer:\n%s", text)
	}
}

func TestCleanChatDisclosedNarrationRendersMarkdown(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	text := strings.Join(a.workingProse("## Inspecting\n\n**Checking** the `config`.\n\n```go\nvalue := 1\n```", 100), "\n")
	for _, raw := range []string{"## ", "**", "```"} {
		if strings.Contains(text, raw) {
			t.Fatalf("raw markdown %q in %s", raw, text)
		}
	}
	if !strings.Contains(text, "value := 1") {
		t.Fatalf("lost code: %s", text)
	}
}

func TestCleanChatOpenedCaptionKeepsInterveningReasoning(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = hierarchyLab()
	a.entries[2].open = true
	a.workMode = config.WorkFold
	a.workOpen = map[int]bool{1: true}
	a.capOpen = map[int]bool{1: true}
	a.touch()
	text := strings.Join(plainRows(a), "\n")
	if !strings.Contains(text, "the prefix is read twice") {
		t.Fatalf("caption swallowed its intervening reasoning:\n%s", text)
	}
}

func TestCleanChatLiveCaptionNeverExceedsThreeRows(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	w := liveWork{key: 1, turn: 1, steps: []caption{{text: strings.Repeat("long caption ", 20), source: captionSaid}}, last: true}
	got := a.liveStepBlock(w, 20, deck{})
	if len(got) > liveStepRows {
		t.Fatalf("compact activity has %d rows", len(got))
	}
}

func TestCleanChatFailureIsCompactStatusUntilOpened(t *testing.T) {
	a := liveStepsApp(t)
	a.entries = append(a.entries, entry{kind: entryTool, tool: "bash", status: toolFailed, turn: 1, text: "failed command", detail: toolDetail{Output: "PRIVATE TRACEBACK"}})
	a.touch()
	text := strings.Join(plainRows(a), "\n")
	if strings.Contains(text, "PRIVATE TRACEBACK") || !strings.Contains(text, a.icon(tokens.GFailed)) {
		t.Fatalf("failure not compact status:\n%s", text)
	}
}

func TestCleanChatLiveReceiptRemainsClickableAfterDisclosure(t *testing.T) {
	a := liveStepsApp(t)
	at := len(a.entries)
	a.entries = append(a.entries, entry{kind: entryDone, turn: 1, done: &taskDone{id: 7, title: "background receipt", ident: identFor(7)}})
	a.touch()
	if got := strings.Join(plainRows(a), "\n"); strings.Contains(got, "background receipt") {
		t.Fatalf("receipt escaped compact work: %s", got)
	}
	a.toggleLatestWorkfold()
	for _, r := range rows(a) {
		if r.entry == at && r.hit == hitDone && strings.Contains(plain(r.text), "background receipt") {
			return
		}
	}
	t.Fatal("opened work lost the clickable task receipt")
}

func TestCleanChatHousekeepingSharesTurnDisclosureWithoutExtraChips(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = append([]entry{{kind: entryNote, text: "startup housekeeping", turn: 0}}, foldFixture()...)
	a.entries = append(a.entries, entry{kind: entryNote, text: "cache housekeeping", turn: 1})
	a.workMode = config.WorkFold
	a.touch()
	got := strings.Join(plainRows(a), "\n")
	if strings.Count(got, "worked") != 1 || strings.Contains(got, "housekeeping") {
		t.Fatalf("housekeeping created extra visible work:\n%s", got)
	}
	a.toggleLatestWorkfold()
	got = strings.Join(plainRows(a), "\n")
	for _, text := range []string{"startup housekeeping", "cache housekeeping"} {
		if !strings.Contains(got, text) {
			t.Fatalf("turn disclosure lost %q:\n%s", text, got)
		}
	}
}

func TestCleanChatSteeringAndConfirmedUpdateKeepOneActivityWindow(t *testing.T) {
	a := liveStepsApp(t)
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "A useful interim reply.", turn: 1, settled: true, confirmed: &responseConfirmation{done: true}}, entry{kind: entrySteer, text: "also check formatting", turn: 1, steer: &steerElbow{words: "also check formatting", at: liveStepsBase}}, entry{kind: entryNote, text: "internal chatter", turn: 1}, entry{kind: entryTool, tool: "bash", text: "second command", status: toolRunning, turn: 1})
	a.touch()
	got := strings.Join(plainRows(a), "\n")
	if !strings.Contains(got, "A useful interim reply") || !strings.Contains(got, "also check formatting") || strings.Contains(got, "internal chatter") {
		t.Fatalf("interim reply or steering boundary broken:\n%s", got)
	}
	n := 0
	for _, r := range rows(a) {
		if r.hit == hitWorkFold {
			n++
		}
	}
	if n > liveStepRows {
		t.Fatalf("steering produced %d activity rows:\n%s", n, got)
	}
}

func TestCleanChatInterruptPreservesConfirmedInterimReply(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = foldFixture()
	a.entries[5].confirmed = &responseConfirmation{done: true}
	a.entries = append(a.entries, entry{kind: entryThinking, turn: 1, text: "unfinished work", settled: false}, entry{kind: entryAssistant, turn: 1, text: "unfinished sentence"})
	a.cutTurn(1)
	if a.entries[5].cut || !a.entries[7].cut {
		t.Fatal("interrupt crossed the confirmed response boundary")
	}
	a.workMode = config.WorkFold
	a.touch()
	got := strings.Join(plainRows(a), "\n")
	if !strings.Contains(got, "Done.") || strings.Contains(got, "unfinished sentence") {
		t.Fatalf("interrupt lost reply or exposed partial work:\n%s", got)
	}
}

func TestCleanChatPhaseKeepsUserDirectedNotesAndDecisions(t *testing.T) {
	for _, kept := range []entry{{kind: entryNote, text: "your requested help", told: true, turn: 1}, {kind: entryTool, tool: "bash", status: toolOK, decision: "allowed", turn: 1}} {
		es := []entry{{kind: entryTool, tool: "read", status: toolOK, turn: 1}, kept, {kind: entryTool, tool: "read", status: toolOK, turn: 1}, {kind: entryAssistant, text: "reply", settled: true, turn: 1}}
		for _, f := range derivePhaseFolds(es) {
			if f.start <= 1 && f.answer > 1 {
				t.Fatalf("phase hid user decision/notice: %+v", f)
			}
		}
	}
}

func TestCleanChatSettledTailAfterUserNoticeStillFolds(t *testing.T) {
	es := []entry{{kind: entryUser, text: "check", turn: 1}, {kind: entryNote, text: "requested help", told: true, turn: 1}, {kind: entryTool, tool: "bash", status: toolFailed, turn: 1}}
	folds := deriveWorkfolds(es, 0)
	f, ok := folds[2]
	if !ok || f.answer != 3 || f.failures != 1 {
		t.Fatalf("operational tail after notice escaped disclosure: %+v", folds)
	}
}

func TestCleanChatInterruptWithUserNoticeStillCollapsesWork(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = []entry{{kind: entryUser, text: "check", turn: 1}, {kind: entryTool, tool: "bash", status: toolOK, turn: 1}, {kind: entryNote, text: "requested help", told: true, turn: 1}, {kind: entryTool, tool: "bash", status: toolRunning, text: "unfinished command", turn: 1}}
	a.cutTurn(1)
	folds := deriveWorkfolds(a.entries, 0)
	for _, at := range []int{1, 3} {
		if _, ok := folds[at]; !ok {
			t.Fatalf("interrupted work exposed around user notice: %+v", folds)
		}
	}
	for _, f := range folds {
		if f.start <= 2 && f.answer > 2 {
			t.Fatal("user notice folded on interrupt")
		}
	}
}

func TestCleanChatSettledToolOnlyTailAcrossAllSurfaces(t *testing.T) {
	for _, l := range []lens{participantLens, overseerLens, transcriptLens} {
		for _, status := range []toolState{toolOK, toolFailed} {
			a := newTestApp(&fakeAgent{model: "m"})
			es := []entry{{kind: entryUser, text: "Run a check", turn: 1}, {kind: entryAssistant, text: "Checking the repository.", turn: 1, settled: true}, {kind: entryTool, tool: "bash", text: "PRIVATE COMMAND", detail: toolDetail{Output: "PRIVATE OUTPUT"}, turn: 1, status: status}}
			d := deck{entries: es, lens: l, workOpen: map[int]bool{}, capOpen: map[int]bool{1: true}}
			rendered, _ := a.deckRows(d, 100)
			var lines []string
			for _, r := range rendered {
				lines = append(lines, plain(r.text))
			}
			got := strings.Join(lines, "\n")
			if !strings.Contains(got, "worked") || strings.Contains(got, "PRIVATE") {
				t.Fatalf("settled tool-only tail expanded: %s", got)
			}
			for _, f := range a.deckFolds(d) {
				d.workOpen[f.key] = true
			}
			rendered, _ = a.deckRows(d, 100)
			lines = nil
			for _, r := range rendered {
				lines = append(lines, plain(r.text))
			}
			if !strings.Contains(strings.Join(lines, "\n"), "PRIVATE COMMAND") {
				t.Fatal("settled tail cannot be disclosed")
			}
		}
	}
}
