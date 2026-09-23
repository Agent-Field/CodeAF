package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAskCommandSelectsAnExchangeFromEitherComposer(t *testing.T) {
	for _, home := range []bool{true, false} {
		for _, line := range []string{"/ask explain this", "explain /ask this"} {
			lab := newErrandLab(t)
			mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())
			a := lab.app(mine)
			a.start = func(string) (Conversation, error) {
				t.Fatal("/ask started a regular conversation")
				return Conversation{}, nil
			}
			if home {
				a.openHome()
				typeHome(a, line)
			} else {
				a.closeHome()
				a.input.setText(line)
			}
			drive(t, a, key("enter"))
			if !a.at(pageHome) || theExchange(a) == nil {
				t.Fatalf("%q, home=%v: no home exchange", line, home)
			}
			if len(lab.agent.sent) != 1 || lab.agent.sent[0] != "explain this" {
				t.Fatalf("sent %v", lab.agent.sent)
			}
			if a.home.box.String() != "" {
				t.Fatal("successful ask left the command in the box")
			}
		}
	}
}

func TestBareAskAndCompletionKeepTheQuestionEditable(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	typeHome(a, "/as")
	drive(t, a, key("up"), key("enter"))
	if a.home.box.String() != "/ask " || len(a.exchanges) != 0 {
		t.Fatalf("completion produced %q", a.home.box.String())
	}
	drive(t, a, key("enter"))
	if a.home.box.String() != "/ask " || len(a.exchanges) != 0 {
		t.Fatal("bare /ask must wait for the question")
	}
	typeHome(a, "explain this")
	drive(t, a, key("enter"))
	if len(lab.agent.sent) != 1 || lab.agent.sent[0] != "explain this" {
		t.Fatalf("sent %v", lab.agent.sent)
	}
}

func TestAskRefusalPreservesTheDraftAndTray(t *testing.T) {
	a, dir := homeDropLab(t, "server.log")
	pasteText(t, a, filepath.Join(dir, "server.log"))
	typeHome(a, "/ask explain this")
	a.errand = nil
	drive(t, a, key("enter"))
	if a.home.box.String() != "/ask explain this" || theExchange(a) != nil || len(a.chips) != 1 {
		t.Fatal("unavailable ask consumed the draft")
	}
	if !strings.Contains(homeText(a), homeAskUnavailableWord) {
		t.Fatal("ask refusal was not visible")
	}
}

func TestHomeSubmissionStaysUnselectedAcrossResizes(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	typeHome(a, "pricing")
	for _, width := range []int{180, 80, 45, 180} {
		a.width = width
		homeText(a)
		if _, ok := a.home.focusedLine(); ok || a.home.picked {
			t.Fatalf("redraw at %d selected a result", width)
		}
		drive(t, a, key("up"))
		if a.home.focused().Transcript != mine {
			t.Fatalf("first up at %d did not select the result", width)
		}
		drive(t, a, key("down"))
		if _, ok := a.home.focusedLine(); ok || a.home.picked {
			t.Fatalf("down at %d did not return to composing", width)
		}
	}
}

func TestRemovingAskRestoresNewConversationAndConflictingTagsKeepTheDraft(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	typeHome(a, "explain /ask /task this")
	drive(t, a, key("enter"))
	if a.home.box.String() != "explain /ask /task this" || len(lab.agent.sent) != 0 {
		t.Fatal("conflicting tags consumed the draft")
	}
	next := &fakeAgent{model: "m"}
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: filepath.Join(t.TempDir(), "transcript.jsonl")}, nil
	}
	a.home.box.setText("explain this")
	a.home.build()
	drive(t, a, key("enter"))
	if a.at(pageHome) || len(next.sent) != 1 || next.sent[0] != "explain this" || len(lab.agent.sent) != 0 {
		t.Fatal("removing the tags did not restore a new conversation")
	}
}
