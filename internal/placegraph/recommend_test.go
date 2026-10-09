package placegraph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/roles"
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
	mk(t, g.store, "Config parser")
	g.model.answer = func(ModelRequest) (string, error) { return `{"place":"p1","confidence":88}`, nil }
	c := chat("c1", "config parser rejects nested tables")
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
	if g.model.calls() != 1 {
		t.Fatalf("model asked %d times for one chat", g.model.calls())
	}
	open, _ := g.rec.Pending()
	if len(open) != 1 {
		t.Fatalf("%d open offers for one chat", len(open))
	}
}

func TestTheModelIsAskedWithTheFilingRoleAndOnlyRealPlacesComeBack(t *testing.T) {
	cases := map[string]string{
		"a label it was not shown": `{"place":"p9","confidence":99}`,
		"a real place id":          `{"place":"pl_1","confidence":99}`,
		"prose instead of JSON":    `I think Config parser fits best.`,
		"two objects":              `{"place":"p1","confidence":90}{"place":"p1","confidence":90}`,
		"no confidence":            `{"place":"p1"}`,
		"a path":                   `{"place":"../../etc","confidence":99}`,
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			g := newRig(t)
			mk(t, g.store, "Config parser")
			before := g.revision()
			g.model.answer = func(ModelRequest) (string, error) { return answer, nil }
			p, _ := g.rec.FileChat(context.Background(), chat("c1", "config parser nested tables"), nil)
			if p != nil {
				t.Fatalf("offered %+v from %q", p, answer)
			}
			if g.model.asked[0].Role != roles.RolePlaceFile {
				t.Fatalf("asked as %q", g.model.asked[0].Role)
			}
			if g.revision() != before {
				t.Fatal("a bad answer changed the graph")
			}
		})
	}
}

func TestAFencedAnswerIsStillRead(t *testing.T) {
	g := newRig(t)
	pl := mk(t, g.store, "Config parser")
	g.model.answer = func(ModelRequest) (string, error) {
		return "```json\n{\"place\": \"p1\", \"confidence\": 80}\n```", nil
	}
	p, err := g.rec.FileChat(context.Background(), chat("c1", "config parser nested tables"), nil)
	if err != nil || p == nil || p.PlaceID != pl.ID {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestAnUnsureAnswerIsNotOffered(t *testing.T) {
	g := newRig(t)
	mk(t, g.store, "Config parser")
	g.model.answer = func(ModelRequest) (string, error) { return `{"place":"p1","confidence":60}`, nil }
	if p, _ := g.rec.FileChat(context.Background(), chat("c1", "config parser nested tables"), nil); p != nil {
		t.Fatalf("offered at 60%%: %+v", p)
	}
}

func TestAModelFailureLeavesEverythingAsItWasAndMayBeTriedAgain(t *testing.T) {
	g := newRig(t)
	pl := mk(t, g.store, "Config parser")
	before := g.revision()
	g.model.answer = func(ModelRequest) (string, error) { return "", errors.New("provider down") }
	c := chat("c1", "config parser nested tables")
	if p, err := g.rec.FileChat(context.Background(), c, nil); p != nil || err == nil {
		t.Fatalf("%v %v", p, err)
	}
	if g.revision() != before {
		t.Fatal("a failure changed the graph")
	}
	g.model.answer = func(ModelRequest) (string, error) { return `{"place":"p1","confidence":90}`, nil }
	p, err := g.rec.FileChat(context.Background(), c, nil)
	if err != nil || p == nil || p.PlaceID != pl.ID {
		t.Fatalf("retry: %+v %v", p, err)
	}
}

func TestTheDailyFilingBudgetStopsCallsAndADayLaterAllowsThem(t *testing.T) {
	g := newRig(t)
	mk(t, g.store, "Config parser")
	g.policy.FilingCallsPerDay = 2
	g.model.answer = func(ModelRequest) (string, error) { return `{"place":"none","confidence":0}`, nil }
	for i := range 5 {
		_, _ = g.rec.FileChat(context.Background(), chat(fmt.Sprintf("c%d", i), "config parser nested tables"), nil)
	}
	if g.model.calls() != 2 {
		t.Fatalf("%d calls on a budget of 2", g.model.calls())
	}
	g.later(25 * time.Hour)
	_, _ = g.rec.FileChat(context.Background(), chat("c9", "config parser nested tables"), nil)
	if g.model.calls() != 3 {
		t.Fatal("the budget did not come back after a day")
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
	mk(t, g.store, "Config parser")
	g.policy.AutoFile = true
	g.model.answer = func(ModelRequest) (string, error) { return `{"place":"p1","confidence":80}`, nil }
	p, _ := g.rec.FileChat(context.Background(), chat("c1", "config parser nested tables"), nil)
	if p == nil || p.Status != StatusPending {
		t.Fatalf("%+v", p)
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
	if err != nil || len(open) != 1 {
		t.Fatalf("%+v %v", open, err)
	}
	p := open[0]
	if p.Kind != ProposalCreate || p.Name != "weather-app" || p.ParentID != "" || len(p.ChatIDs) != 5 {
		t.Fatalf("%+v", p)
	}
	if g.model.calls() != 0 {
		t.Fatal("a folder group asked the model for its name")
	}
	res, err := g.rec.Accept(p.ID, AcceptEdit{})
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := g.store.Snapshot()
	pl, _ := snap.Place(res.PlaceID)
	if pl.Name != "weather-app" || len(pl.Context.Sources) != 0 || pl.Policy != (Policy{}) {
		t.Fatalf("accepting widened the place: %+v", pl)
	}
	for _, c := range lib {
		ms := snap.PlacesOf(c.ChatID)
		if len(ms) != 1 || ms[0].PlaceID != pl.ID || ms[0].AddedBy != AddedByAI {
			t.Fatalf("%s: %+v", c.ChatID, ms)
		}
	}
	// Taking the receipts back in reverse takes the whole accept back.
	for i := len(res.Receipts) - 1; i >= 0; i-- {
		if _, err := g.store.Undo(res.Receipts[i].ID); err != nil {
			t.Fatal(err)
		}
	}
	snap, _ = g.store.Snapshot()
	if len(snap.Places) != 0 || len(snap.Memberships) != 0 {
		t.Fatal("undo left part of the accept behind")
	}
}

func TestAGroupThatFitsAnExistingPlaceIsOfferedThatPlaceNotANewOne(t *testing.T) {
	g := newRig(t)
	home := g.folderPlace("Weather", "/home/u/src/weather-app")
	open, err := g.rec.Organize(context.Background(), chatsIn("w", "/home/u/src/weather-app/web", 5))
	if err != nil || len(open) != 1 {
		t.Fatalf("%+v %v", open, err)
	}
	if open[0].Kind != ProposalMove || open[0].PlaceID != home.ID {
		t.Fatalf("%+v", open[0])
	}
}

func TestAModelNamedPlaceIsOfferedForAGroupOfWords(t *testing.T) {
	g := newRig(t)
	g.model.answer = func(ModelRequest) (string, error) {
		return `{"belong":true,"chats":["c1","c2","c3","c4","c5"],"use":"","name":"Launch week","under":"root","confidence":85}`, nil
	}
	open, err := g.rec.Organize(context.Background(), releaseChats())
	if err != nil || len(open) != 1 {
		t.Fatalf("%+v %v", open, err)
	}
	if open[0].Kind != ProposalCreate || open[0].Name != "Launch week" || open[0].Basis != BasisModel {
		t.Fatalf("%+v", open[0])
	}
	if g.model.asked[0].Role != roles.RolePlaceSuggest {
		t.Fatalf("asked as %q", g.model.asked[0].Role)
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
	g.policy.OrganizeEveryMinutes = 5
	g.model.answer = func(ModelRequest) (string, error) {
		return `{"belong":true,"chats":["c1","c2","c3","c4","c5"],"name":"Launch week","confidence":85}`, nil
	}
	lib := releaseChats()
	open, _ := g.rec.Organize(context.Background(), lib)
	if len(open) != 1 {
		t.Fatalf("%+v", open)
	}
	g.later(time.Hour)
	again, _ := g.rec.Organize(context.Background(), lib)
	if len(again) != 1 || again[0].ID != open[0].ID || g.model.calls() != 1 {
		t.Fatalf("repeat: %d offers, %d calls", len(again), g.model.calls())
	}
	if err := g.rec.Decline(open[0].ID); err != nil {
		t.Fatal(err)
	}
	// A sixth chat joins: still mostly the declined group.
	lib = append(lib, chat("r6", "launch week release press list"))
	g.later(10 * 24 * time.Hour)
	if after, _ := g.rec.Organize(context.Background(), lib); len(after) != 0 || g.model.calls() != 1 {
		t.Fatalf("re-offered inside the snooze: %+v, %d calls", after, g.model.calls())
	}
	g.later(21 * 24 * time.Hour)
	if after, _ := g.rec.Organize(context.Background(), lib); len(after) != 1 || g.model.calls() != 2 {
		t.Fatalf("not offered after the snooze: %+v", after)
	}
}

func TestAGroupTheModelRejectedIsNotAskedAboutAgainSoon(t *testing.T) {
	g := newRig(t)
	g.policy.OrganizeEveryMinutes = 5
	g.model.answer = func(ModelRequest) (string, error) { return `{"belong":false,"confidence":90}`, nil }
	_, _ = g.rec.Organize(context.Background(), releaseChats())
	g.later(time.Hour)
	_, _ = g.rec.Organize(context.Background(), releaseChats())
	if g.model.calls() != 1 {
		t.Fatalf("asked %d times about one rejected group", g.model.calls())
	}
}

func TestTheOrganizingIntervalSpacesModelCalls(t *testing.T) {
	g := newRig(t)
	g.model.answer = func(ModelRequest) (string, error) { return `{"belong":false,"confidence":90}`, nil }
	_, _ = g.rec.Organize(context.Background(), releaseChats())
	other := []ChatEvidence{
		chat("g1", "garden irrigation plan spring"), chat("g2", "garden irrigation drip spring"),
		chat("g3", "spring garden irrigation timer"), chat("g4", "garden irrigation spring layout"),
		chat("g5", "garden irrigation spring budget"),
	}
	g.later(10 * time.Minute)
	_, _ = g.rec.Organize(context.Background(), other)
	if g.model.calls() != 1 {
		t.Fatal("a second pass inside the interval called the model")
	}
	g.later(time.Hour)
	_, _ = g.rec.Organize(context.Background(), other)
	if g.model.calls() != 2 {
		t.Fatal("the interval never reopened")
	}
}

func TestTheTopLevelCapStopsAICreationButNeverAPersonsOwnPlaces(t *testing.T) {
	g := newRig(t)
	g.policy.MaxAITopLevel = 1
	accept := func(prefix, folder string) error {
		open, err := g.rec.Organize(context.Background(), chatsIn(prefix, folder, 5))
		if err != nil {
			return err
		}
		for _, p := range open {
			if p.Kind == ProposalCreate {
				_, err := g.rec.Accept(p.ID, AcceptEdit{})
				return err
			}
		}
		return errors.New("no create offer")
	}
	if err := accept("a", "/home/u/alpha"); err != nil {
		t.Fatal(err)
	}
	if err := accept("b", "/home/u/beta"); err == nil {
		t.Fatal("a second AI top-level place was offered past the cap")
	}
	// A person may make as many as they like, and those do not use the cap up.
	for i := range 10 {
		mk(t, g.store, fmt.Sprintf("mine %d", i))
	}
	snap, _ := g.store.Snapshot()
	if len(snap.Children(RootID, false)) != 11 {
		t.Fatalf("%d top-level places", len(snap.Children(RootID, false)))
	}
}

func TestAnOfferTheCapsNoLongerAllowIsClosedOnAccept(t *testing.T) {
	g := newRig(t)
	open, _ := g.rec.Organize(context.Background(), chatsIn("a", "/home/u/alpha", 5))
	g.policy.MaxAIPlaces = 0
	if _, err := g.rec.Accept(open[0].ID, AcceptEdit{}); !errors.Is(err, ErrPolicyLimit) {
		t.Fatalf("accept past the cap: %v", err)
	}
	snap, _ := g.store.Snapshot()
	if len(snap.Places) != 0 {
		t.Fatal("a place was created past the cap")
	}
	if still, _ := g.rec.Pending(); len(still) != 0 {
		t.Fatal("the refused offer is still shown")
	}
}

func TestANewPlaceIsNeverOfferedDeeperThanTheDepthCap(t *testing.T) {
	g := newRig(t)
	g.policy.MaxAIDepth = 2
	a := mk(t, g.store, "launch")
	mk(t, g.store, "launch week release", a.ID) // depth 2: no room below it
	// The model always picks the LAST parent it was offered.
	g.model.answer = func(q ModelRequest) (string, error) {
		last := "root"
		for i := 1; strings.Contains(q.User, fmt.Sprintf("u%d:", i)); i++ {
			last = fmt.Sprintf("u%d", i)
		}
		return fmt.Sprintf(`{"belong":true,"chats":["c1","c2","c3","c4","c5"],"name":"Press","under":%q,"confidence":95}`, last), nil
	}
	g.policy.MinConfidence = 90 // keep the lexical match from reusing a place outright
	open, _ := g.rec.Organize(context.Background(), releaseChats())
	snap, _ := g.store.Snapshot()
	if len(open) != 1 || open[0].Kind != ProposalCreate || open[0].ParentID != a.ID {
		t.Fatalf("expected one offer under %s: %+v", a.ID, open)
	}
	if d := depthOf(snap, open[0].ParentID) + 1; d > 2 {
		t.Fatalf("offered a place at depth %d", d)
	}
}

func TestAnOfferTheGraphMovedPastIsClosedNotApplied(t *testing.T) {
	g := newRig(t)
	open, _ := g.rec.Organize(context.Background(), chatsIn("w", "/home/u/weather", 5))
	mine := mk(t, g.store, "Mine")
	for i := range 5 {
		if _, _, err := g.store.AddChat(fmt.Sprintf("w%d", i), mine.ID, AddedByYou); err != nil {
			t.Fatal(err)
		}
	}
	before := g.revision()
	if _, err := g.rec.Accept(open[0].ID, AcceptEdit{}); !errors.Is(err, ErrProposalGone) {
		t.Fatalf("accept: %v", err)
	}
	if g.revision() != before {
		t.Fatal("a stale offer changed the graph")
	}
}

func TestAPersonCanNarrowAndRenameAnOfferButNotWidenIt(t *testing.T) {
	g := newRig(t)
	open, _ := g.rec.Organize(context.Background(), chatsIn("w", "/home/u/weather", 5))
	id := open[0].ID
	if _, err := g.rec.Accept(id, AcceptEdit{ChatIDs: []string{"w0", "elsewhere"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("widened: %v", err)
	}
	res, err := g.rec.Accept(id, AcceptEdit{Name: "Forecasts", ChatIDs: []string{"w0", "w1"}})
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := g.store.Snapshot()
	if pl, _ := snap.Place(res.PlaceID); pl.Name != "Forecasts" || len(snap.ChatsIn(pl.ID, false)) != 2 {
		t.Fatalf("%+v %v", pl, snap.ChatsIn(pl.ID, false))
	}
	if _, err := g.rec.Accept(id, AcceptEdit{}); !errors.Is(err, ErrProposalGone) {
		t.Fatal("an offer was accepted twice")
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
	if id := findCycle(&snap.State); id != "" {
		t.Fatalf("cycle through %s", id)
	}
}

func TestOpenOffersNeverExceedTheLimit(t *testing.T) {
	g := newRig(t)
	g.policy.MaxPending = 2
	var lib []ChatEvidence
	for _, f := range []string{"alpha", "beta", "gamma", "delta"} {
		lib = append(lib, chatsIn(f, "/home/u/"+f, 5)...)
	}
	open, _ := g.rec.Organize(context.Background(), lib)
	if len(open) != 2 {
		t.Fatalf("%d open offers on a limit of 2", len(open))
	}
	again, _ := g.rec.Organize(context.Background(), lib)
	if len(again) != 2 {
		t.Fatalf("%d after a second pass", len(again))
	}
}

func TestWithoutAModelOnlyRuleOffersAreMade(t *testing.T) {
	g := newRig(t)
	g.rec.Ask = nil
	open, err := g.rec.Organize(context.Background(), append(releaseChats(), chatsIn("w", "/home/u/weather", 5)...))
	if err != nil || len(open) != 1 || open[0].Basis != BasisFolder {
		t.Fatalf("%+v %v", open, err)
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
