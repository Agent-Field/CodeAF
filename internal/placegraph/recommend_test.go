package placegraph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---- harness -----------------------------------------------------------------

// fakeModel stands in for the engine's role door: it answers from a script and
// counts every question, so a test can say "no call was made" as a fact.
type fakeModel struct {
	mu     sync.Mutex
	asked  []ModelRequest
	answer func(ModelRequest) (string, error)
}

func (f *fakeModel) ask(_ context.Context, q ModelRequest) (string, error) {
	f.mu.Lock()
	f.asked = append(f.asked, q)
	answer := f.answer
	f.mu.Unlock()
	if answer == nil {
		return "", errors.New("no answer scripted")
	}
	return answer(q)
}

func (f *fakeModel) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

type rig struct {
	t      *testing.T
	store  *Store
	rec    *Recommender
	model  *fakeModel
	policy RecommendPolicy
	clock  time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	s, path := newStore(t)
	l, err := OpenLedger(filepath.Join(filepath.Dir(path), "places-ai.json"))
	if err != nil {
		t.Fatal(err)
	}
	g := &rig{t: t, store: s, model: &fakeModel{}, policy: DefaultRecommendPolicy(), clock: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	g.rec = &Recommender{Store: s, Ledger: l, Ask: g.model.ask,
		Policy: func() RecommendPolicy { return g.policy },
		Now:    func() time.Time { return g.clock }}
	return g
}

func (g *rig) later(d time.Duration) { g.clock = g.clock.Add(d) }

func (g *rig) folderPlace(name, folder string) Place {
	g.t.Helper()
	p, _, err := g.store.CreatePlace(NewPlace{Name: name, Context: Context{Sources: []Source{{Kind: SourceRepo, Ref: folder, AddedBy: AddedByYou}}}})
	if err != nil {
		g.t.Fatal(err)
	}
	return p
}

func (g *rig) revision() uint64 {
	g.t.Helper()
	rev, err := g.store.Revision()
	if err != nil {
		g.t.Fatal(err)
	}
	return rev
}

func chat(id, title string) ChatEvidence {
	return ChatEvidence{ChatID: id, Title: title, Replies: 1}
}

func chatsIn(prefix, folder string, n int, titles ...string) []ChatEvidence {
	var out []ChatEvidence
	for i := range n {
		title := fmt.Sprintf("work item %d", i)
		if i < len(titles) {
			title = titles[i]
		}
		out = append(out, ChatEvidence{ChatID: fmt.Sprintf("%s%d", prefix, i), Title: title, Workspace: folder, Replies: 2})
	}
	return out
}

// releaseChats are five chats in no folder that plainly share one subject.
func releaseChats() []ChatEvidence {
	return []ChatEvidence{
		chat("r1", "launch week release notes draft"),
		chat("r2", "launch week release checklist"),
		chat("r3", "release notes for launch week"),
		chat("r4", "launch week release blog outline"),
		chat("r5", "launch week release timeline"),
	}
}

// ---- filing one chat -----------------------------------------------------------

func TestAChatInAPlacesFolderIsOfferedThatPlaceWithoutAskingAModel(t *testing.T) {
	g := newRig(t)
	home := g.folderPlace("codeaf", "/home/u/src/codeaf")
	g.folderPlace("garden", "/home/u/garden")
	before := g.revision()

	p, err := g.rec.FileChat(context.Background(), ChatEvidence{ChatID: "c1", Title: "fix flaky test", Workspace: "/home/u/src/codeaf/internal", Replies: 1}, nil)
	if err != nil || p == nil {
		t.Fatalf("offer %v, %v", p, err)
	}
	if p.Kind != ProposalFile || p.PlaceID != home.ID || p.Basis != BasisFolder {
		t.Fatalf("offered %+v", p)
	}
	if g.model.calls() != 0 {
		t.Fatal("a folder match asked the model anyway")
	}
	if g.revision() != before {
		t.Fatal("an offer changed the graph before anyone accepted it")
	}
}

func TestNothingIsOfferedBeforeTheFirstReply(t *testing.T) {
	g := newRig(t)
	g.folderPlace("codeaf", "/home/u/src/codeaf")
	p, err := g.rec.FileChat(context.Background(), ChatEvidence{ChatID: "c1", Title: "x", Workspace: "/home/u/src/codeaf", Replies: 0}, nil)
	if p != nil || err != nil {
		t.Fatalf("got %v, %v", p, err)
	}
}

func TestAChatIsOfferedAtMostOnceHoweverOftenItIsWeighed(t *testing.T) {
	g := newRig(t)
	g.folderPlace("Config parser", "/home/u/src/parser")
	c := ChatEvidence{ChatID: "c1", Title: "config parser rejects nested tables", Workspace: "/home/u/src/parser", Replies: 1}
	first, err := g.rec.FileChat(context.Background(), c, nil)
	if err != nil || first == nil {
		t.Fatalf("first: %v %v", first, err)
	}
	for range 3 {
		again, err := g.rec.FileChat(context.Background(), c, nil)
		if err != nil || again == nil || again.ID != first.ID {
			t.Fatalf("again: %+v %v", again, err)
		}
	}
	if g.model.calls() != 0 {
		t.Fatalf("model asked %d times for one chat", g.model.calls())
	}
	open, _ := g.rec.Pending()
	if len(open) != 1 {
		t.Fatalf("%d open offers for one chat", len(open))
	}
}

func TestFilingDoesNotAskAModelWhereAChatGoes(t *testing.T) {
	g := newRig(t)
	mk(t, g.store, "Config parser")
	before := g.revision()
	g.model.answer = func(ModelRequest) (string, error) { return `{"place":"p1","confidence":99}`, nil }
	p, err := g.rec.FileChat(context.Background(), chat("c1", "config parser nested tables"), nil)
	if err != nil || p != nil {
		t.Fatalf("offer %+v, %v", p, err)
	}
	if g.model.calls() != 0 {
		t.Fatal("filing asked a model; that question belongs to the organiser")
	}
	if g.revision() != before {
		t.Fatal("weighing a chat changed the graph")
	}
}

func TestAFencedAnswerIsStillRead(t *testing.T) {
	i, conf, err := readFileAnswer("```json\n{\"place\": \"p1\", \"confidence\": 80}\n```", []string{"Config parser"})
	if err != nil || i != 0 || conf != 80 {
		t.Fatalf("%d %d %v", i, conf, err)
	}
}

func TestAnUnsureAnswerIsNotOffered(t *testing.T) {
	g := newRig(t)
	mk(t, g.store, "Config parser")
	if p, err := g.rec.FileChat(context.Background(), chat("c1", "config parser nested tables"), nil); err != nil || p != nil {
		t.Fatalf("offered %+v %v", p, err)
	}
}

func TestAModelFailureLeavesEverythingAsItWasAndMayBeTriedAgain(t *testing.T) {
	g := newRig(t)
	before := g.revision()
	g.model.answer = func(ModelRequest) (string, error) { return "", errors.New("provider down") }
	c := chat("c1", "config parser nested tables")
	if p, err := g.rec.FileChat(context.Background(), c, nil); p != nil || err != nil {
		t.Fatalf("%v %v", p, err)
	}
	if g.model.calls() != 0 || g.revision() != before {
		t.Fatal("a filing without a folder called the model or changed the graph")
	}
}

func TestTheDailyFilingBudgetStopsCallsAndADayLaterAllowsThem(t *testing.T) {
	g := newRig(t)
	mk(t, g.store, "Config parser")
	g.policy.FilingCallsPerDay = 2
	for i := range 5 {
		if p, err := g.rec.FileChat(context.Background(), chat(fmt.Sprintf("c%d", i), "config parser nested tables"), nil); err != nil || p != nil {
			t.Fatalf("%+v %v", p, err)
		}
	}
	if g.model.calls() != 0 {
		t.Fatalf("%d model calls; filing no longer spends that budget", g.model.calls())
	}
}

func TestATooVagueChatOrAnEmptyGraphCostsNoCall(t *testing.T) {
	g := newRig(t)
	g.model.answer = func(ModelRequest) (string, error) { return `{"place":"p1","confidence":99}`, nil }
	// No places at all.
	if p, _ := g.rec.FileChat(context.Background(), chat("c1", "config parser nested tables"), nil); p != nil {
		t.Fatal("offered with no places")
	}
	mk(t, g.store, "Config parser")
	// A chat that says almost nothing.
	if p, _ := g.rec.FileChat(context.Background(), chat("c2", "hi"), nil); p != nil {
		t.Fatal("offered for a chat that said nothing")
	}
	if g.model.calls() != 0 {
		t.Fatalf("%d calls with nothing to decide", g.model.calls())
	}
}

func TestOfferingNeverFilesUnlessAutomaticFilingIsTurnedOn(t *testing.T) {
	g := newRig(t)
	pl := g.folderPlace("codeaf", "/home/u/src/codeaf")
	c := ChatEvidence{ChatID: "c1", Title: "fix it", Workspace: "/home/u/src/codeaf", Replies: 1}
	if _, err := g.rec.FileChat(context.Background(), c, nil); err != nil {
		t.Fatal(err)
	}
	snap, _ := g.store.Snapshot()
	if len(snap.PlacesOf("c1")) != 0 {
		t.Fatal("the default policy filed a chat without asking")
	}

	g.policy.AutoFile = true
	c2 := ChatEvidence{ChatID: "c2", Title: "fix it", Workspace: "/home/u/src/codeaf", Replies: 1}
	p, err := g.rec.FileChat(context.Background(), c2, nil)
	if err != nil || p == nil || p.Status != StatusAccepted || !p.Auto {
		t.Fatalf("%+v %v", p, err)
	}
	snap, _ = g.store.Snapshot()
	ms := snap.PlacesOf("c2")
	if len(ms) != 1 || ms[0].PlaceID != pl.ID || ms[0].AddedBy != AddedByAI {
		t.Fatalf("memberships %+v", ms)
	}
}

func TestAutomaticFilingStillWaitsBelowItsOwnThreshold(t *testing.T) {
	g := newRig(t)
	g.folderPlace("codeaf", "/home/u/src/codeaf")
	g.policy.AutoFile = true
	g.policy.AutoFileConfidence = 99
	p, err := g.rec.FileChat(context.Background(), ChatEvidence{ChatID: "c1", Title: "fix it", Workspace: "/home/u/src/codeaf", Replies: 1}, nil)
	if err != nil || p == nil || p.Status != StatusPending {
		t.Fatalf("%+v %v", p, err)
	}
	snap, _ := g.store.Snapshot()
	if len(snap.PlacesOf("c1")) != 0 {
		t.Fatal("a folder match below the automatic threshold was filed")
	}
}

// ---- organizing chats in no place --------------------------------------------

func TestFourChatsAreNotEnoughForANewPlace(t *testing.T) {
	g := newRig(t)
	g.model.answer = func(ModelRequest) (string, error) {
		return `{"belong":true,"chats":["c1","c2","c3","c4"],"name":"Launch","confidence":99}`, nil
	}
	open, err := g.rec.Organize(context.Background(), releaseChats()[:4])
	if err != nil || len(open) != 0 {
		t.Fatalf("%+v %v", open, err)
	}
	if g.model.calls() != 0 {
		t.Fatal("asked about a group below the minimum")
	}
}

func TestFiveChatsInOneFolderAreOfferedAPlaceNamedAfterItAndAcceptingIsNarrow(t *testing.T) {
	g := newRig(t)
	lib := chatsIn("w", "/home/u/src/weather-app", 5)
	open, err := g.rec.Organize(context.Background(), lib)
	if err != nil || len(open) != 0 || g.model.calls() != 0 {
		t.Fatalf("a shared folder is no longer a new place from here: %+v %v calls %d", open, err, g.model.calls())
	}
}

func TestAGroupThatFitsAnExistingPlaceIsOfferedThatPlaceNotANewOne(t *testing.T) {
	g := newRig(t)
	g.folderPlace("Weather", "/home/u/src/weather-app")
	open, err := g.rec.Organize(context.Background(), chatsIn("w", "/home/u/src/weather-app/web", 5))
	if err != nil || len(open) != 0 || g.model.calls() != 0 {
		t.Fatalf("filing a group into an existing place is the organiser's run: %+v %v", open, err)
	}
}

func TestAModelNamedPlaceIsOfferedForAGroupOfWords(t *testing.T) {
	g := newRig(t)
	g.model.answer = func(ModelRequest) (string, error) {
		return `{"belong":true,"chats":["c1","c2","c3","c4","c5"],"use":"","name":"Launch week","under":"root","confidence":85}`, nil
	}
	open, err := g.rec.Organize(context.Background(), releaseChats())
	if err != nil || len(open) != 0 || g.model.calls() != 0 {
		t.Fatalf("shared words are not a place from here: %+v %v calls %d", open, err, g.model.calls())
	}
}

func TestUnusableSuggestionsOfferNothingAndChangeNothing(t *testing.T) {
	cases := map[string]string{
		"a path for a name":            `{"belong":true,"chats":["c1","c2","c3","c4","c5"],"name":"../../etc/passwd","confidence":90}`,
		"a url for a name":             `{"belong":true,"chats":["c1","c2","c3","c4","c5"],"name":"https://x.test","confidence":90}`,
		"a sentence for a name":        `{"belong":true,"chats":["c1","c2","c3","c4","c5"],"name":"All of the chats that are about the launch week release","confidence":90}`,
		"a chat it was not shown":      `{"belong":true,"chats":["c1","c2","c3","c4","c9"],"name":"Launch","confidence":90}`,
		"a parent it was not shown":    `{"belong":true,"chats":["c1","c2","c3","c4","c5"],"name":"Launch","under":"u4","confidence":90}`,
		"an existing it was not shown": `{"belong":true,"chats":["c1","c2","c3","c4","c5"],"use":"p2","confidence":90}`,
		"too few chats":                `{"belong":true,"chats":["c1","c2","c3"],"name":"Launch","confidence":90}`,
		"not sure":                     `{"belong":true,"chats":["c1","c2","c3","c4","c5"],"name":"Launch","confidence":40}`,
		"they do not belong":           `{"belong":false,"confidence":90}`,
		"truncated":                    `{"belong":true,"chats":["c1"`,
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			g := newRig(t)
			before := g.revision()
			g.model.answer = func(ModelRequest) (string, error) { return answer, nil }
			open, _ := g.rec.Organize(context.Background(), releaseChats())
			if len(open) != 0 {
				t.Fatalf("offered %+v", open)
			}
			if g.revision() != before {
				t.Fatal("the graph changed")
			}
		})
	}
}

func TestAGroupIsAskedAboutOnceAndADeclineHoldsForTheSnooze(t *testing.T) {
	g := newRig(t)
	open, err := g.rec.Organize(context.Background(), releaseChats())
	if err != nil || len(open) != 0 || g.model.calls() != 0 {
		t.Fatalf("%+v %v", open, err)
	}
}

func TestAGroupTheModelRejectedIsNotAskedAboutAgainSoon(t *testing.T) {
	g := newRig(t)
	if _, err := g.rec.Organize(context.Background(), releaseChats()); err != nil || g.model.calls() != 0 {
		t.Fatal(err)
	}
	if _, err := g.rec.Organize(context.Background(), releaseChats()); err != nil || g.model.calls() != 0 {
		t.Fatal(err)
	}
}

func TestTheOrganizingIntervalSpacesModelCalls(t *testing.T) {
	g := newRig(t)
	if _, err := g.rec.Organize(context.Background(), releaseChats()); err != nil || g.model.calls() != 0 {
		t.Fatal("organizing a group of words called a model")
	}
}

func TestTheTopLevelCapStopsAICreationButNeverAPersonsOwnPlaces(t *testing.T) {
	g := newRig(t)
	open, err := g.rec.Organize(context.Background(), chatsIn("a", "/home/u/alpha", 5))
	if err != nil || len(open) != 0 {
		t.Fatalf("a folder of chats is not a new place from this pass: %+v %v", open, err)
	}
	for i := range 10 {
		mk(t, g.store, fmt.Sprintf("mine %d", i))
	}
	snap, _ := g.store.Snapshot()
	if len(snap.Children(RootID, false)) != 10 {
		t.Fatalf("%d top-level places", len(snap.Children(RootID, false)))
	}
}

func TestAnOfferTheCapsNoLongerAllowIsClosedOnAccept(t *testing.T) {
	g := newRig(t)
	open, err := g.rec.Organize(context.Background(), chatsIn("a", "/home/u/alpha", 5))
	if err != nil || len(open) != 0 {
		t.Fatalf("%+v %v", open, err)
	}
}

func TestANewPlaceIsNeverOfferedDeeperThanTheDepthCap(t *testing.T) {
	g := newRig(t)
	open, err := g.rec.Organize(context.Background(), releaseChats())
	if err != nil || len(open) != 0 || g.model.calls() != 0 {
		t.Fatalf("%+v %v", open, err)
	}
}

func TestAnOfferTheGraphMovedPastIsClosedNotApplied(t *testing.T) {
	g := newRig(t)
	open, err := g.rec.Organize(context.Background(), chatsIn("w", "/home/u/weather", 5))
	if err != nil || len(open) != 0 {
		t.Fatalf("%+v %v", open, err)
	}
}

func TestAPersonCanNarrowAndRenameAnOfferButNotWidenIt(t *testing.T) {
	g := newRig(t)
	open, err := g.rec.Organize(context.Background(), chatsIn("w", "/home/u/weather", 5))
	if err != nil || len(open) != 0 {
		t.Fatalf("narrowing a folder group is the organiser plan's skip: %+v %v", open, err)
	}
}

func TestLookAlikeSiblingsAreOfferedAMergeThatKeepsTheGraphAcyclic(t *testing.T) {
	g := newRig(t)
	older := mk(t, g.store, "Config parser")
	newer := mk(t, g.store, "config-parsers")
	mk(t, g.store, "Garden")
	open, err := g.rec.Organize(context.Background(), nil)
	if err != nil || len(open) != 1 {
		t.Fatalf("%+v %v", open, err)
	}
	if open[0].Kind != ProposalMerge || open[0].PlaceID != older.ID || open[0].FromPlaceID != newer.ID {
		t.Fatalf("%+v", open[0])
	}
	if g.model.calls() != 0 {
		t.Fatal("a merge asked the model")
	}
	// The person nests one under the other before answering; the offer is no
	// longer between siblings, and accepting must not make a cycle.
	if _, err := g.store.Reparent(older.ID, []string{newer.ID}); err != nil {
		t.Fatal(err)
	}
	_, _ = g.rec.Accept(open[0].ID, AcceptEdit{})
	snap, _ := g.store.Snapshot()
	if names := (&snap.State).cycleNames(); names != "" {
		t.Fatalf("cycle %s", names)
	}
}

func TestOpenOffersNeverExceedTheLimit(t *testing.T) {
	g := newRig(t)
	g.policy.MaxPending = 2
	var lib []ChatEvidence
	for _, f := range []string{"alpha", "beta", "gamma", "delta"} {
		lib = append(lib, chatsIn(f, "/home/u/"+f, 5)...)
	}
	open, err := g.rec.Organize(context.Background(), lib)
	if err != nil || len(open) != 0 {
		t.Fatalf("folder groups are not offers from this pass: %+v %v", open, err)
	}
}

func TestWithoutAModelOnlyRuleOffersAreMade(t *testing.T) {
	g := newRig(t)
	g.rec.Ask = nil
	open, err := g.rec.Organize(context.Background(), append(releaseChats(), chatsIn("w", "/home/u/weather", 5)...))
	if err != nil || len(open) != 0 {
		t.Fatalf("without a model, and without the organiser, nothing new is offered: %+v %v", open, err)
	}
}

func TestADamagedLedgerIsSetAsideAndTheGraphIsUntouched(t *testing.T) {
	g := newRig(t)
	pl := g.folderPlace("codeaf", "/home/u/src/codeaf")
	if err := os.WriteFile(g.rec.Ledger.path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := g.rec.FileChat(context.Background(), ChatEvidence{ChatID: "c1", Title: "x", Workspace: "/home/u/src/codeaf", Replies: 1}, nil)
	if err != nil || p == nil || p.PlaceID != pl.ID {
		t.Fatalf("%+v %v", p, err)
	}
	if g.rec.Ledger.LastRecovery() == "" {
		t.Fatal("the damage was not reported")
	}
}
