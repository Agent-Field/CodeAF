package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

type designingAgent struct {
	*fakeAgent
	answers []harnessAnswer
	lane    chan session.Event
}

func (d *designingAgent) ResolveHarness(id uint64, run bool, model string) {
	d.answers = append(d.answers, harnessAnswer{id: id, run: run, model: model})
}
func (d *designingAgent) HarnessDesigns() <-chan session.Event { return d.lane }

func designedPage() subharness.Harness {
	return subharness.Harness{Id: subharness.Id{Name: "research-helper", Desc: "Research a topic with cited sources"}, Program: subharness.Program{Nodes: []subharness.Node{{Id: "plan", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "plan the research"}}, {Id: "fetch", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "fetch the sources"}}, {Id: "verify", Kind: subharness.KindVerify, Fields: subharness.Fields{"check": "every claim cites a source"}}}, Edges: []subharness.Edge{{"plan", "fetch"}, {"fetch", "verify"}}}, Whitelist: []string{"read", "grep"}, Verify: subharness.Verify{Ladder: subharness.VerifyReport}}
}

func TestHarnessProgressCollapsesIntoFeedCard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.height = 60
	a.designEvent(session.Event{Kind: session.EventHarnessDesign, ID: 7, Text: "research a topic"})
	a.designEvent(session.Event{Kind: session.EventHarnessProgress, ID: 7, Goal: "research a topic", Phase: "designing", Attempt: 1, Attempts: 3, ThoughtTail: "choose sources\nthen verify", Hint: "3 steps so far"})
	if got := plain(frame(a)); !strings.Contains(got, "choose sources") || !strings.Contains(got, "3 steps so far") {
		t.Fatalf("live block is absent:\n%s", got)
	}
	p := designedPage()
	a.designEvent(session.Event{Kind: session.EventHarnessDesignDone, ID: 7, Harness: &p})
	got := plain(frame(a))
	for _, want := range []string{"harness designed", "research-helper", "[plan]──▶[fetch]──▶[verify]", "every claim cites a source", "[e] improve"} {
		if !strings.Contains(got, want) {
			t.Fatalf("card is missing %q:\n%s", want, got)
		}
	}
	if a.asksHarness() {
		t.Fatal("the feed card became a modal question")
	}
}

func TestHarnessCardKeysSaveImproveAndDrop(t *testing.T) {
	for _, tc := range []struct {
		key, word string
		run       bool
	}{{"enter", "saved as research-helper v1", true}, {"e", "improvement requested", false}, {"esc", "dropped", false}} {
		t.Run(tc.key, func(t *testing.T) {
			agent := &designingAgent{fakeAgent: &fakeAgent{model: "m"}}
			a := newTestApp(agent)
			p := designedPage()
			a.finishHarnessCard(session.Event{ID: 9, Harness: &p})
			a.sel = len(a.entries) - 1
			a.harnessCardKey(tea.KeyPressMsg{Code: []rune(tc.key)[0], Text: tc.key})
			c := a.entries[a.sel].harness
			if c.state != tc.word {
				t.Fatalf("state %q", c.state)
			}
			if len(agent.answers) != 1 || agent.answers[0].run != tc.run {
				t.Fatalf("answer %+v", agent.answers)
			}
			if tc.key == "e" && !strings.Contains(a.input.String(), "Improve harness research-helper") {
				t.Fatalf("draft %q", a.input.String())
			}
		})
	}
}

func TestHarnessDiagramGoldens(t *testing.T) {
	shapes := map[string]subharness.Harness{
		"linear":  designedPage(),
		"fan":     {Program: subharness.Program{Nodes: []subharness.Node{{Id: "plan"}, {Id: "web"}, {Id: "papers"}, {Id: "join"}}, Edges: []subharness.Edge{{"plan", "web"}, {"plan", "papers"}, {"web", "join"}, {"papers", "join"}}}},
		"single":  {Program: subharness.Program{Nodes: []subharness.Node{{Id: "answer"}}}},
		"clipped": {Program: subharness.Program{Nodes: []subharness.Node{{Id: "extraordinarily-long-step-name"}}}},
	}
	wants := map[string]string{"linear": "[plan]──▶[fetch]──▶[verify]", "fan": "[plan]\n├──▶ [web]\n└──▶ [papers]\n      ▼\n    [join]", "single": "[answer]", "clipped": "[extraordina…]"}
	for name, h := range shapes {
		t.Run(name+"/wide", func(t *testing.T) {
			got := strings.Join(harnessDiagram(h, 80, false), "\n")
			if got != wants[name] {
				t.Fatalf("\n%s", got)
			}
		})
		t.Run(name+"/phone", func(t *testing.T) {
			got := strings.Join(harnessDiagram(h, 40, true), "\n")
			if !strings.Contains(got, "▼") && len(h.Program.Nodes) > 1 {
				t.Fatalf("not vertical:\n%s", got)
			}
			if strings.Contains(got, "──▶") {
				t.Fatalf("horizontal on phone:\n%s", got)
			}
		})
	}
}

func TestHarnessStallShape(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	c := &harnessCard{phase: "designing", stalled: true, began: a.now().Add(-52e9)}
	got := plain(strings.Join(a.harnessFeedRows(c, 140, false), "\n"))
	if !strings.Contains(got, "reasoning models answer in one burst at the end") {
		t.Fatal(got)
	}
}
