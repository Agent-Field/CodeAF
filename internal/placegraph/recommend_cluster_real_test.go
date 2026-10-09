package placegraph

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// realChat is one conversation from testdata/real-chats-2026-10-09.json: a
// title and a recap a real model wrote for a real turn, exactly as meta.json
// saved them. The group is what the prompt that started it was about.
type realChat struct {
	Group     string `json:"group"`
	ChatID    string `json:"chatId"`
	Title     string `json:"title"`
	Line      string `json:"line"`
	Discussed string `json:"discussed"`
}

func realChats(t *testing.T, withRecaps bool) ([]ChatEvidence, map[string]string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/real-chats-2026-10-09.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Chats []realChat `json:"chats"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	group := map[string]string{}
	var out []ChatEvidence
	for _, c := range file.Chats {
		e := ChatEvidence{ChatID: c.ChatID, Title: c.Title, Replies: 1}
		if withRecaps {
			// The desktop bridge's composition: the recap's line, then what
			// was discussed (internal/desktopbridge's recapSummary).
			e.Summary = c.Line + " " + c.Discussed
		}
		out = append(out, e)
		group[c.ChatID] = c.Group
	}
	return out, group
}

func groupsOf(cls []cluster, group map[string]string) []string {
	var out []string
	for _, cl := range cls {
		var gs []string
		for _, c := range cl.chats {
			gs = append(gs, group[c.ChatID])
		}
		sort.Strings(gs)
		out = append(out, strings.Join(gs, ","))
	}
	return out
}

// On real chats the garden group is found whole, and nothing else is: not the
// Wi-Fi chats (no one topic word reaches all five — the mesh one never says
// Wi-Fi or router), not the strays, and never a mix.
func TestRealChatsGroupByTopicAndOnlyByTopic(t *testing.T) {
	for _, withRecaps := range []bool{false, true} {
		chats, group := realChats(t, withRecaps)
		got := groupsOf(findClusters(chats, 5), group)
		if len(got) != 1 || got[0] != "garden,garden,garden,garden,garden" {
			t.Fatalf("recaps=%v: groups %v", withRecaps, got)
		}
	}
}

// A recap brings in a chat whose title named the subject another way. The
// real "Hose bib drip pressure regulator" chat, titled without "drip", is
// still about the drip system — its own recap says so — and joins only when
// the recap is read.
func TestARecapBringsInAChatWhoseTitleSaysItAnotherWay(t *testing.T) {
	for _, withRecaps := range []bool{false, true} {
		chats, group := realChats(t, withRecaps)
		for i := range chats {
			if chats[i].Title == "Hose bib drip pressure regulator" {
				chats[i].Title = "Hose bib pressure regulator"
			}
		}
		got := groupsOf(findClusters(chats, 5), group)
		switch {
		case withRecaps && (len(got) != 1 || got[0] != "garden,garden,garden,garden,garden"):
			t.Fatalf("with recaps: groups %v", got)
		case !withRecaps && len(got) != 0:
			t.Fatalf("without recaps four titles are not a group of five: %v", got)
		}
	}
}

// A recap's own vocabulary never anchors a group: every recap says
// "discussed" or "recommended", and a group of chats that share only that is
// no group at all.
func TestRecapBoilerplateAnchorsNothing(t *testing.T) {
	var chats []ChatEvidence
	for i, title := range []string{"Buttermilk pancakes", "Roth IRA basics", "Post-run stretching", "Sourdough starter", "Tax bracket math", "Bike chain lube"} {
		chats = append(chats, ChatEvidence{ChatID: string(rune('a' + i)), Title: title, Replies: 1,
			Summary: "Discussed the question; codeaf recommended an approach and the person decided nothing."})
	}
	if got := findClusters(chats, 5); len(got) != 0 {
		t.Fatalf("boilerplate grouped %d chats", len(got[0].chats))
	}
}

func TestAHyphenatedWordCountsJoined(t *testing.T) {
	w := words("Evening 5 GHz Wi-Fi drops; half-inch tubing")
	for _, want := range []string{"wifi", "halfinch", "half", "inch", "evening", "ghz"} {
		if !w[want] {
			t.Errorf("missing %q in %v", want, w)
		}
	}
}

// And Organize, end to end on the real chats: one question, about the garden
// chats only, with their recaps in front of the model.
func TestOrganizeAsksOnceAboutTheRealGardenGroup(t *testing.T) {
	g := newRig(t)
	chats, group := realChats(t, true)
	g.model.answer = func(q ModelRequest) (string, error) {
		return `{"belong": true, "chats": ["c1","c2","c3","c4","c5"], "use": "", "name": "Garden drip irrigation", "under": "root", "confidence": 92}`, nil
	}
	open, err := g.rec.Organize(context.Background(), chats)
	if err != nil {
		t.Fatal(err)
	}
	if g.model.calls() != 1 {
		t.Fatalf("%d questions", g.model.calls())
	}
	q := g.model.asked[0].User
	if !strings.Contains(q, "Tomato drip emitter mineral clogs — Recommended pressure-compensating") {
		t.Fatalf("the question did not carry the recaps:\n%s", q)
	}
	if len(open) != 1 || open[0].Kind != ProposalCreate || open[0].Name != "Garden drip irrigation" || len(open[0].ChatIDs) != 5 {
		t.Fatalf("offered %+v", open)
	}
	for _, id := range open[0].ChatIDs {
		if group[id] != "garden" {
			t.Fatalf("offered a %s chat as garden", group[id])
		}
	}
}

// THE QUESTION IS BOUNDED. However many places could fit, the model is shown
// at most MaxCandidates of them, labelled, and a group is shown at most
// maxClusterShown chats — so a large library never becomes a large prompt.
func TestTheQuestionsAreBounded(t *testing.T) {
	g := newRig(t)
	g.policy.MaxCandidates = 3
	for i := 0; i < 9; i++ {
		if _, _, err := g.store.CreatePlace(NewPlace{Name: "Drip irrigation " + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	g.model.answer = func(ModelRequest) (string, error) { return `{"place":"none","confidence":0}`, nil }
	if _, err := g.rec.FileChat(context.Background(), ChatEvidence{ChatID: "c1", Title: "Drip irrigation emitter clogs", Replies: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if g.model.calls() != 1 {
		t.Fatalf("%d questions", g.model.calls())
	}
	q := g.model.asked[0].User
	if !strings.Contains(q, "p3:") || strings.Contains(q, "p4:") {
		t.Fatalf("shown more than three places:\n%s", q)
	}

	h := newRig(t)
	var many []ChatEvidence
	for i := 0; i < maxClusterShown+10; i++ {
		many = append(many, ChatEvidence{ChatID: "d" + strings.Repeat("x", i+1), Title: "Drip irrigation tubing size", Replies: 1})
	}
	h.model.answer = func(ModelRequest) (string, error) {
		return `{"belong": false, "chats": [], "use": "", "name": "", "under": "root", "confidence": 5}`, nil
	}
	if _, err := h.rec.Organize(context.Background(), many); err != nil {
		t.Fatal(err)
	}
	if h.model.calls() != 1 {
		t.Fatalf("%d questions", h.model.calls())
	}
	if shown := strings.Count(h.model.asked[0].User, "\nc"); shown > maxClusterShown {
		t.Fatalf("shown %d chats", shown)
	}
}
