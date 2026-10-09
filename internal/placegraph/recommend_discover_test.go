package placegraph

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The shapes below are SYNTHETIC: their subjects were written for these tests.
// What they copy from real saved conversations is the structure measured there
// — a group of chats about one subject whose titles share no word, sitting
// among unrelated quick chats.

// kitchen is five chats about one renovation whose titles share no word.
var kitchen = []string{"Quartz or granite countertop", "Soft-close cabinet hinges", "Backsplash grout colour", "Under-cabinet strip lighting", "Pull-down faucet for a deep sink"}

// strays are unrelated chats.
var strays = []string{"Convert a recipe to grams", "Renew a passport by mail", "Best stretch after cycling"}

func discoverLibrary() []ChatEvidence {
	var out []ChatEvidence
	for i, title := range append(append([]string(nil), kitchen...), strays...) {
		out = append(out, ChatEvidence{ChatID: fmt.Sprintf("k%02d", i), Title: title, Replies: 1,
			UpdatedAt: time.Date(2026, 9, 1, 0, i, 0, 0, time.UTC)})
	}
	return out
}

// labelsOf answers a discovery question with the labels of the shown chats
// whose titles are in want.
func labelsOf(q string, want []string) string {
	var labels []string
	for _, line := range strings.Split(q, "\n") {
		label, rest, ok := strings.Cut(line, ": ")
		if !ok || !strings.HasPrefix(label, "c") {
			continue
		}
		for _, w := range want {
			if strings.HasPrefix(rest, w) {
				labels = append(labels, `"`+label+`"`)
			}
		}
	}
	return strings.Join(labels, ",")
}

// A group no word links is found by ONE question over what the rules left,
// offered for a person's yes, and its strays are left where they were.
func TestDiscoveryFindsAGroupNoWordLinks(t *testing.T) {
	g := newRig(t)
	lib := discoverLibrary()
	if got := findClusters(lib, 5); len(got) != 0 {
		t.Fatalf("rules found %d groups; this shape is meant to have no lexical anchor", len(got))
	}
	g.model.answer = func(q ModelRequest) (string, error) {
		return `{"belong": true, "chats": [` + labelsOf(q.User, kitchen) + `], "use": "", "name": "Kitchen renovation", "under": "root", "confidence": 88}`, nil
	}
	open, err := g.rec.Organize(context.Background(), lib)
	if err != nil {
		t.Fatal(err)
	}
	if g.model.calls() != 1 || g.model.asked[0].Role != "placesuggest" || !strings.Contains(g.model.asked[0].User, "probably unrelated") {
		t.Fatalf("asked %d questions: %+v", g.model.calls(), g.model.asked)
	}
	if len(open) != 1 || open[0].Kind != ProposalCreate || open[0].Name != "Kitchen renovation" || len(open[0].ChatIDs) != 5 || open[0].Basis != BasisModel {
		t.Fatalf("offered %+v", open)
	}
	for _, id := range open[0].ChatIDs {
		if id >= "k05" {
			t.Fatalf("a stray (%s) was offered with the group", id)
		}
	}
	// Nothing is reorganised without a yes.
	if snap, _ := g.store.Snapshot(); len(snap.Places) != 0 {
		t.Fatalf("the graph changed before an accept: %d places", len(snap.Places))
	}
}

// A model that sees no group says so, and the same set is not shown again —
// not on the next pass, not after the interval — until most of what is left is
// new. A quiet library costs one question, not one an hour.
func TestDiscoveryIsNotRepeatedOverTheSameChats(t *testing.T) {
	g := newRig(t)
	lib := discoverLibrary()
	g.model.answer = func(ModelRequest) (string, error) {
		return `{"belong": false, "chats": [], "use": "", "name": "", "under": "root", "confidence": 20}`, nil
	}
	for pass := 0; pass < 3; pass++ {
		if _, err := g.rec.Organize(context.Background(), lib); err != nil {
			t.Fatal(err)
		}
		g.clock = g.clock.Add(2 * time.Hour)
	}
	if g.model.calls() != 1 {
		t.Fatalf("the same chats were shown %d times", g.model.calls())
	}
	// Nine new chats arrive: most of what is left is now new, so it is shown once more.
	for i := 0; i < 9; i++ {
		lib = append(lib, ChatEvidence{ChatID: fmt.Sprintf("n%02d", i), Title: fmt.Sprintf("Unrelated question number %d about %s", i, strays[i%3]), Replies: 1})
	}
	if _, err := g.rec.Organize(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	if g.model.calls() != 2 {
		t.Fatalf("%d questions after the library more than doubled", g.model.calls())
	}
}

// A group the model offers names only chats it was shown, and an answer that
// lists fewer than the policy's least is no offer at all.
func TestDiscoveryHoldsTheGroupContract(t *testing.T) {
	g := newRig(t)
	lib := discoverLibrary()
	g.model.answer = func(q ModelRequest) (string, error) {
		return `{"belong": true, "chats": [` + labelsOf(q.User, kitchen[:3]) + `], "use": "", "name": "Kitchen", "under": "root", "confidence": 95}`, nil
	}
	open, err := g.rec.Organize(context.Background(), lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("three chats were offered as a place at a least of five: %+v", open)
	}
	h := newRig(t)
	h.model.answer = func(ModelRequest) (string, error) {
		return `{"belong": true, "chats": ["c1","c2","c3","c4","c99"], "use": "", "name": "Kitchen", "under": "root", "confidence": 95}`, nil
	}
	if open, err := h.rec.Organize(context.Background(), lib); len(open) != 0 || err == nil {
		t.Fatalf("an unshown label was accepted: %+v, %v", open, err)
	}
}

// ONE PASS, ONE INTERVAL. A pass that finds three groups asks about all three
// — it used to ask about the first and leave the others an hour each — and
// the next pass inside the interval asks nothing.
func TestOnePassAsksAboutEveryGroupItFound(t *testing.T) {
	g := newRig(t)
	var lib []ChatEvidence
	for gi, topic := range []string{"sourdough", "kayak", "telescope"} {
		for i := 0; i < 5; i++ {
			lib = append(lib, ChatEvidence{ChatID: fmt.Sprintf("%c%d", 'a'+gi, i), Title: fmt.Sprintf("%s %s%d %s%d", topic, topic[:3], i, topic[1:4], i), Replies: 1})
		}
	}
	g.model.answer = func(q ModelRequest) (string, error) {
		return `{"belong": true, "chats": ["c1","c2","c3","c4","c5"], "use": "", "name": "Hobby", "under": "root", "confidence": 90}`, nil
	}
	open, err := g.rec.Organize(context.Background(), lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("duplicate open names would fail on accepting both: %+v", open)
	}
	if g.model.calls() != 3 {
		t.Fatalf("a pass with three groups asked %d questions", g.model.calls())
	}
	g.clock = g.clock.Add(10 * time.Minute)
	if _, err := g.rec.Organize(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	if g.model.calls() != 3 {
		t.Fatalf("a pass inside the interval asked again (%d)", g.model.calls())
	}
}

// The daily budget still binds a pass: with two calls a day left, a pass that
// found three groups asks two questions.
func TestThePassStaysInsideTheDailyBudget(t *testing.T) {
	g := newRig(t)
	g.policy.ClusterCallsPerDay = 2
	var lib []ChatEvidence
	for gi, topic := range []string{"sourdough", "kayak", "telescope"} {
		for i := 0; i < 5; i++ {
			lib = append(lib, ChatEvidence{ChatID: fmt.Sprintf("%c%d", 'a'+gi, i), Title: fmt.Sprintf("%s %s%d %s%d", topic, topic[:3], i, topic[1:4], i), Replies: 1})
		}
	}
	g.model.answer = func(ModelRequest) (string, error) {
		return `{"belong": false, "chats": [], "use": "", "name": "", "under": "root", "confidence": 5}`, nil
	}
	if _, err := g.rec.Organize(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	if g.model.calls() != 2 {
		t.Fatalf("asked %d questions with a budget of two", g.model.calls())
	}
}

// Both group questions say what is not a reason to belong together.
func TestTheGroupQuestionsNameWhatIsNotABond(t *testing.T) {
	g := newRig(t)
	lib := discoverLibrary()
	q := discoverQuestion(&Snapshot{}, lib, nil, nil, 5)
	s := suggestQuestion(&Snapshot{}, lib, nil, nil)
	for _, text := range []string{q.User, s.User} {
		if !strings.Contains(text, groupingGuard) || !strings.Contains(text, suggestShape) {
			t.Fatalf("question lacks the guard or the answer shape:\n%s", text)
		}
	}
	if !strings.Contains(q.User, "at least 5") || q.Role != "placesuggest" {
		t.Fatalf("discovery question: %+v", q)
	}
	_ = g
}

// A large unchanged library advances beyond its newest sample rather than
// hiding its older chats permanently behind a snoozed sample.
func TestDiscoveryExaminesOlderUnseenChats(t *testing.T) {
	g := newRig(t)
	var lib []ChatEvidence
	for i := 0; i < 2*maxDiscoveryShown+8; i++ {
		lib = append(lib, ChatEvidence{ChatID: fmt.Sprintf("d%015x", i), Title: fmt.Sprintf("%c%c%c alpha%c%c%c beta%c%c%c", 'a'+rune(i/26), 'a'+rune(i%26), 'q', 'a'+rune(i/26), 'a'+rune(i%26), 'r', 'a'+rune(i/26), 'a'+rune(i%26), 's'), Replies: 1, UpdatedAt: g.clock.Add(time.Duration(i) * time.Minute)})
	}
	g.model.answer = func(ModelRequest) (string, error) {
		return `{"belong":false,"chats":[],"use":"","name":"","under":"root","confidence":0}`, nil
	}
	if _, err := g.rec.Organize(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	g.clock = g.clock.Add(2 * time.Hour)
	if _, err := g.rec.Organize(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	if g.model.calls() != 2 {
		t.Fatalf("older sample not examined: %d calls", g.model.calls())
	}
	if strings.Contains(g.model.asked[0].User, "c1: aaq") || !strings.Contains(g.model.asked[1].User, "aiq") {
		t.Fatal("older unseen chats were not sampled")
	}
}

// A declined top-ranked group must not consume every future pass's candidate
// slots and permanently hide the smaller groups behind it.
func TestSuppressedGroupsDoNotStarveLaterGroups(t *testing.T) {
	g := newRig(t)
	var lib []ChatEvidence
	for gi, topic := range []string{"sourdough", "kayak", "telescope", "ceramics"} {
		for i := 0; i < 5; i++ {
			lib = append(lib, ChatEvidence{ChatID: fmt.Sprintf("%c%d", 'a'+gi, i), Title: fmt.Sprintf("%s %s%d %s%d", topic, topic[:3], i, topic[1:4], i), Replies: 1})
		}
	}
	g.model.answer = func(ModelRequest) (string, error) {
		return `{"belong":false,"chats":[],"use":"","name":"","under":"root","confidence":0}`, nil
	}
	if _, err := g.rec.Organize(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	if g.model.calls() != maxClustersPass {
		t.Fatalf("first pass asked %d", g.model.calls())
	}
	g.clock = g.clock.Add(2 * time.Hour)
	if _, err := g.rec.Organize(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	if g.model.calls() != 4 {
		t.Fatalf("later group was starved: %d calls", g.model.calls())
	}
}
