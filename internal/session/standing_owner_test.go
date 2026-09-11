package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── ONE LIVE OWNER PER REPORT PATH, AND WHAT THE REVIEW OF 417fa43a3 FOUND ──
//
// R2 moved a report's receipt from the item to the path so a successor could
// publish where its stopped predecessor had. It also let two LIVE items at one
// path take turns: each found the file matching the path's receipt — the
// other's — and replaced it, every run, for ever (the review's B1). Every test
// below failed on 417fa43a3.

// writeItemDocument writes item's document as it stands, past every door — a
// pair made before one owner per path was kept is exactly that.
func writeItemDocument(t *testing.T, store *standing.Store, item standing.Item) {
	t.Helper()
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ItemPath(item.ID), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// B1. TWO LIVE ITEMS AT ONE REPORT PATH DO NOT TAKE TURNS. The first to publish
// keeps it; the other is held, with its own code, until one is stopped.
func TestTwoLiveItemsAtOneReportDoNotTakeTurns(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, first := storedReporting(t, root, workspace)
	second := first
	second.ID, second.Words = "0123456789abcdef", "a second order on the same report"
	writeItemDocument(t, store, second)
	if outcome := fireReport(t, store, first, 1, "# Report\n- the first's"); outcome.Published == nil {
		t.Fatalf("the first item did not publish: %+v", outcome)
	}
	outcome := fireReport(t, store, second, 1, "# Report\n- the second's")
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if outcome.Published != nil || string(raw) != "# Report\n- the first's\n" {
		t.Fatalf("a second live item replaced the first's report: %q (outcome %+v)", raw, outcome)
	}
	if outcome.Withheld != "report-owned" || !strings.Contains(outcome.Text, first.ID) {
		t.Fatalf("the second item's run = %+v, want report-owned naming %s", outcome, first.ID)
	}
	if again := fireReport(t, store, first, 2, "# Report\n- the first's, again"); again.Published == nil {
		t.Fatalf("the owner was held at its own report: %+v", again)
	}
	if _, err := store.SetStatus(first.ID, standing.StatusRetired, standing.StoppedWhy); err != nil {
		t.Fatal(err)
	}
	if after := fireReport(t, store, second, 2, "# Report\n- the second's, alone"); after.Published == nil {
		t.Fatalf("with the first stopped the second was still held: %+v", after)
	}
}

// B1 AT THE STORE. A report path another live item owns is refused by the
// write itself, not only by the card: an edit that moves a report onto it too.
func TestTheStoreRefusesASecondLiveOwnerOfAReport(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, first := storedReporting(t, root, workspace)
	again := reporting(workspace)
	again.ID = ""
	if made, err := store.Create(again); err == nil || !strings.Contains(err.Error(), "is already the report of") {
		t.Fatalf("a second live owner was created: %+v (%v)", made, err)
	}
	elsewhere := reporting(workspace)
	elsewhere.ID, elsewhere.Does.Report = "", "reports/other.md"
	other, err := store.Create(elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Revise(other.ID, other.SpecRevision, func(item *standing.Item) error {
		item.Does.Report = first.Does.Report
		return nil
	}); err == nil || !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("an edit moved a report onto a path another live item owns (%v)", err)
	}
}

// listCountingStore counts the reads of every item a door can make.
type listCountingStore struct {
	*standing.Store
	lists atomic.Int32
}

func (c *listCountingStore) ForWorkspace(workspace string) ([]standing.Item, error) {
	c.lists.Add(1)
	return c.Store.ForWorkspace(workspace)
}

// B2 (L6). THE WRITE GUARD READS ONE RECORD, NOT THE STORE. It read every item
// on the machine on every write; now it reads the path's owner and that one
// item. And a path spelled the ways bare's write accepts it — `@reports/…`,
// `file://…` — is the same file to the guard (the review's non-blocking R11).
func TestTheWriteGuardReadsThePathsOwnerNotEveryItem(t *testing.T) {
	var counting *listCountingStore
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, func(config *Config) {
		counting = &listCountingStore{Store: config.Standing.Store}
		config.standingItems = counting
	})
	d.proposeInbox(t, d.yes)
	item := d.only(t)
	report := filepath.Join(d.project, "reports", "inbox-report.md")
	for _, path := range []string{"reports/inbox-report.md", "@reports/inbox-report.md", "file://" + report, "notes/free.md"} {
		counting.lists.Store(0)
		args, _ := json.Marshal(map[string]string{"path": path, "content": "# by the chat\n"})
		d.agent.client = &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("w1", "write", string(args)), nil
			},
			finalText("done"),
		}}
		out := toolOutput(t, d.submitAnswering(t, "write it", nil), "write")
		if n := counting.lists.Load(); n != 0 {
			t.Errorf("writing %s read every item %d time(s)", path, n)
		}
		if path == "notes/free.md" {
			if strings.Contains(out, "is the report of") {
				t.Errorf("a free path was refused: %q", out)
			}
			continue
		}
		if !strings.Contains(out, item.ID) {
			t.Errorf("%s slipped past the guard: %q", path, out)
		}
		if _, err := os.Stat(report); !os.IsNotExist(err) {
			t.Fatalf("%s wrote the report aforge publishes", path)
		}
	}
}

// THE PATH OF A STOPPED ITEM IS NOT GUARDED: its report is nobody's any more.
func TestAStoppedItemsReportIsNotGuarded(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	d.proposeInbox(t, d.yes)
	if _, err := d.store.SetStatus(d.only(t).ID, standing.StatusRetired, standing.StoppedWhy); err != nil {
		t.Fatal(err)
	}
	if owner, owned := d.agent.standingReportAt(filepath.Join(d.project, "reports", "inbox-report.md")); owned {
		t.Fatalf("a stopped item's report is still guarded as %s's", owner.ID)
	}
}

// (g) A SUCCESSOR TO AN ITEM STOPPED BEFORE PATHS KEPT RECEIPTS publishes where
// it did: the predecessor's own receipt is moved to the path once.
func TestASuccessorToAnItemStoppedBeforeTheUpgradePublishes(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, err := standing.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	old := reporting(workspace)
	old.ID, old.Status = "fedcba9876543210", standing.StatusRetired
	writeItemDocument(t, store, old)
	body := "# Report\n- from before the upgrade\n"
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "reports", "r.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.ItemDir(old.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	kept, _ := json.Marshal(map[string]standing.Publication{"reports/r.md": {Path: "reports/r.md", SHA256: sha256Hex(body), Bytes: len(body)}})
	if err := os.WriteFile(filepath.Join(store.ItemDir(old.ID), "published.json"), kept, 0o600); err != nil {
		t.Fatal(err)
	}
	successor := reporting(workspace)
	successor.ID = ""
	made, err := store.Create(successor)
	if err != nil {
		t.Fatal(err)
	}
	if outcome := fireReport(t, store, made, 1, "# Report\n- from the successor"); outcome.Published == nil {
		t.Fatalf("the successor was held at its predecessor's report: %+v", outcome)
	}
}

// (b) AN EDIT OF A STOPPED ITEM IS REFUSED BEFORE ANY CARD, and so is a move.
func TestAnEditOfAStoppedItemIsRefusedBeforeTheCard(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	d.proposeInbox(t, d.yes)
	item := d.only(t)
	if _, err := d.store.SetStatus(item.ID, standing.StatusRetired, standing.StoppedWhy); err != nil {
		t.Fatal(err)
	}
	work := d.folder(t, "Work")
	for _, call := range []map[string]any{
		{"op": "edit", "id": item.ID, "does": map[string]any{"instructions": "Read inbox/ and list the requests."}},
		{"op": "edit", "id": item.ID, "placement": work.ID},
	} {
		raw, _ := json.Marshal(call)
		d.agent.client = &scriptedCompleter{steps: []step{standCall("s2", string(raw)), finalText("no")}}
		events := d.submitAnswering(t, "change it", func(Event) { t.Fatal("a card was drawn for a stopped item") })
		if out := toolOutput(t, events, "stand"); !strings.Contains(out, "stopped") {
			t.Fatalf("an edit of a stopped item answered %q", out)
		}
	}
}

// (a) A MOVE IS FENCED AT THE YES AND READS WHERE THE WORK IS THEN (L2). A
// move with nothing else in it skipped the revision, so a stop while its card
// was open still placed the work; and it unplaced the folders it read when
// the card was drawn, not the ones the work was in at the yes.
func TestAMoveIsFencedAtTheYesAndReadsItsFoldersThen(t *testing.T) {
	for _, stop := range []bool{true, false} {
		d := newChatDoor(t, nil, nil)
		personal, work, launch := d.folder(t, "Personal"), d.folder(t, "Work"), d.folder(t, "Launch")
		d.agent.client = &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"placement": personal.ID})), finalText("set up")}}
		d.proposeInbox(t, d.yes)
		item := d.only(t)
		move, _ := json.Marshal(map[string]any{"op": "edit", "id": item.ID, "placement": work.ID})
		d.agent.client = &scriptedCompleter{steps: []step{standCall("s2", string(move)), finalText("moved")}}
		ref := workspace.Ref{Kind: workspace.StandingKind, ID: item.ID}
		events := d.submitAnswering(t, "move it to Work", func(event Event) {
			if stop {
				if _, err := d.store.SetStatus(item.ID, standing.StatusRetired, standing.StoppedWhy); err != nil {
					t.Fatal(err)
				}
			} else if err := d.org.AddPlacement(context.Background(), launch.ID, ref); err != nil {
				t.Fatal(err)
			}
			d.yes(event)
		})
		governing, err := d.org.GoverningCollections(context.Background(), ref)
		if err != nil {
			t.Fatal(err)
		}
		var direct []string
		for _, folder := range governing {
			if folder.Depth == 0 {
				direct = append(direct, folder.Name)
			}
		}
		slices.Sort(direct)
		switch {
		case stop && !slices.Equal(direct, []string{"Personal"}):
			t.Errorf("a move answered after a stop placed the stopped work: %q (the tool said %q)", direct, toolOutput(t, events, "stand"))
		case !stop && !slices.Equal(direct, []string{"Work"}):
			t.Errorf("after the move the work is in %q, want only Work", direct)
		}
	}
}

// (c) AN EDIT CARD SHOWS THE VALUES IT CHANGES, NOT ONLY THEIR NAMES. The grant
// is the permission record: a yes to `what it may do` without its words is a
// yes to something nobody read.
func TestAnEditCardShowsTheValuesItChanges(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	d.proposeInbox(t, d.yes)
	item := d.only(t)
	edit, _ := json.Marshal(map[string]any{"op": "edit", "id": item.ID, "grant": "open a pull request but never merge it",
		"title": "inbox digest", "does": map[string]any{"model": "deepseek/deepseek-v4-flash", "acceptance": "every request is listed", "max_steps": 30}})
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s2", string(edit)), finalText("changed")}}
	var card *StandingNotice
	d.submitAnswering(t, "let it open pull requests", func(event Event) {
		card = event.Standing
		d.agent.ResolveStanding(event.Standing.ID, StandingAnswer{})
	})
	if card == nil {
		t.Fatal("no card was drawn")
	}
	for _, want := range []string{
		"grant · none → open a pull request but never merge it",
		"title · none → inbox digest",
		"model · the configured one → deepseek/deepseek-v4-flash",
		"acceptance · none → every request is listed",
		"steps · " + strconv.Itoa(standingRunSteps) + " → 30",
	} {
		if !slices.Contains(card.Terms, want) {
			t.Errorf("the card does not say %q: %q", want, card.Terms)
		}
	}
}

// (d) WORDS ON AN EDIT NAME THE ITEM. A call with no id whose words are the
// change, not the item, is told so and told how to name it.
func TestAnEditWhoseWordsNameNothingIsToldWhatWordsAreFor(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	d.proposeInbox(t, d.yes)
	edit, _ := json.Marshal(map[string]any{"op": "edit", "words": "also list who owns each request", "does": map[string]any{"instructions": "List each request and its owner."}})
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s2", string(edit)), finalText("no")}}
	out := toolOutput(t, d.submitAnswering(t, "also list who owns each request", nil), "stand")
	if !strings.Contains(out, "send its id") || !strings.Contains(out, "words") {
		t.Fatalf("an edit naming nothing answered %q", out)
	}
}

// (f) D7: A JSON OBJECT SENT AS A STRING IS THE OBJECT. The live rails run sent
// `"does":"{\"kind\":\"task\",…}"` six times and was told six times that does
// takes an object. The string is unwrapped for every tool; free text is still
// refused, since reading prose as `does` would guess between a say and a task.
func TestAnObjectSentAsAJSONStringIsTheObject(t *testing.T) {
	var parsed standArguments
	if err := decodeToolArguments(json.RawMessage(`{"op":"propose","does":"{\"kind\":\"task\",\"instructions\":\"Summarise inbox/.\",\"max_steps\":12}"}`), &parsed); err != nil {
		t.Fatalf("a JSON-encoded does was refused: %v", err)
	}
	if parsed.Does.Kind != "task" || parsed.Does.Instructions != "Summarise inbox/." || parsed.Does.MaxSteps != 12 {
		t.Fatalf("the unwrapped does = %+v", parsed.Does)
	}
	if err := decodeToolArguments(json.RawMessage(`{"op":"propose","does":"summarise the inbox"}`), &parsed); err == nil || !strings.Contains(err.Error(), "does takes an object") {
		t.Fatalf("free text was taken as does (%v)", err)
	}
}

// (h) THE RAILS SENTENCE WITH NO OTHER COPY IS BACK: a model that names no
// money is told what the card quotes instead.
func TestTheRailsSayWhatTheCardQuotesWhenNoMoneyIsNamed(t *testing.T) {
	if !strings.Contains(standSchemaJSON, "otherwise the card quotes the machine-wide daily allowance") {
		t.Fatal("the rails description lost what the card quotes when nobody named money")
	}
}
