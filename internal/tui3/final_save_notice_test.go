package tui3

import (
	"github.com/Agent-Field/codeaf/internal/remote"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

type finalSavingWindowAgent struct {
	*fakeAgent
	lines []string
	final bool
}

func (a *finalSavingWindowAgent) FinishSaving() error {
	if a.final {
		a.lines = append(a.lines, chatlist.Superseded("desk"))
		a.final = false
	}
	return nil
}
func (a *finalSavingWindowAgent) TakeSavingNotices() []string {
	lines := a.lines
	a.lines = nil
	return lines
}

func TestClosingOneChatShowsItsFinalSavingSentenceOnce(t *testing.T) {
	for _, displaced := range []bool{true, false} {
		t.Run(map[bool]string{true: "displaced", false: "clean"}[displaced], func(t *testing.T) {
			agent := &finalSavingWindowAgent{fakeAgent: &fakeAgent{model: "m"}, final: displaced}
			a := newTestApp(agent)
			t.Cleanup(a.leaveEverything)
			a.file = "/tmp/closing/transcript.jsonl"
			emptyMachine(a)
			a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/remaining/transcript.jsonl"}, &aside{})
			before := len(noteTexts(a))
			cmd, more := a.closeFrontFor(session.StopByLeaving)
			if !more {
				t.Fatal("no remaining chat")
			}
			spend(t, a, cmd)
			said := strings.Join(noteTexts(a)[before:], "\n")
			count := strings.Count(said, chatlist.Superseded("desk"))
			if displaced && count != 1 || !displaced && count != 0 {
				t.Fatalf("closing window notes: %q", said)
			}
			if len(agent.TakeSavingNotices()) != 0 {
				t.Fatal("closing window left a duplicate receipt")
			}
		})
	}
}

type slowFinalSavingWindowAgent struct {
	*finalSavingWindowAgent
	release chan struct{}
}

func (a *slowFinalSavingWindowAgent) FinishSaving() error {
	<-a.release
	return a.finalSavingWindowAgent.FinishSaving()
}

func TestASlowIndividualCloseKeepsItsSentenceWhenTheOtherChatQuits(t *testing.T) {
	for _, timing := range []string{"after quit", "before quit without display", "shown live"} {
		t.Run(timing, func(t *testing.T) {
			agent := &slowFinalSavingWindowAgent{finalSavingWindowAgent: &finalSavingWindowAgent{fakeAgent: &fakeAgent{model: "m"}, final: true}, release: make(chan struct{})}
			var release sync.Once
			unblock := func() { release.Do(func() { close(agent.release) }) }
			defer unblock()
			a := newTestApp(agent)
			a.file = "/tmp/closing/transcript.jsonl"
			emptyMachine(a)
			a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/remaining/transcript.jsonl"}, &aside{})
			var mu sync.Mutex
			var report []string
			a.savingNotices = func(lines []string) { mu.Lock(); defer mu.Unlock(); report = append(report, lines...) }
			var done <-chan struct{}
			a.closingStarted = func(ch <-chan struct{}) { done = ch }
			cmd, more := a.closeFront()
			if !more {
				t.Fatal("no second chat")
			}
			spend(t, a, cmd)
			if timing != "after quit" {
				unblock()
				<-done
			}
			if timing == "shown live" {
				a.saidNotices()
			}
			a.leavingWork()()
			unblock()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("individual close never completed")
			}
			a.saidNotices()
			mu.Lock()
			defer mu.Unlock()
			said := strings.Join(report, "\n")
			want := 1
			if timing == "shown live" {
				want = 0
			}
			if strings.Count(said, chatlist.Superseded("desk")) != want {
				t.Fatalf("restored-terminal report %q, want count %d", said, want)
			}
			if strings.Count(strings.Join(noteTexts(a), "\n"), chatlist.Superseded("desk"))+strings.Count(said, chatlist.Superseded("desk")) != 1 {
				t.Fatal("sentence was lost or said twice")
			}
		})
	}
}

func TestSharedSessionSwapsShowBothFinalSentencesOnce(t *testing.T) {
	first, second, third := &fakeAgent{model: "m"}, &fakeAgent{model: "m"}, &fakeAgent{model: "m"}

	wrappedFirst := &windowSwapSavingAgent{servedAgent: &servedAgent{first}, pending: []string{"first final sentence"}}
	wrappedSecond := &windowSwapSavingAgent{servedAgent: &servedAgent{second}, pending: []string{"second final sentence"}}
	loop, err := remote.Loopback(remote.Hello{}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{
			Agent: wrappedFirst, Fresh: func() (remote.WrappedAgent, string, error) { return wrappedSecond, "/two.jsonl", nil }, Open: func(string) (remote.WrappedAgent, bool, error) { return &servedAgent{third}, true, nil },
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	a := newTestApp(loop.Client.Agent())
	defer a.leaveEverything()
	a.shared = true
	a.resume = func(file string) (Agent, error) {
		_, err := loop.Client.OpenSession(file)
		return loop.Client.Agent(), err
	}
	a.fresh = func() (Agent, string, error) {
		w, err := loop.Client.NewSession()
		return loop.Client.Agent(), w.SessionFile, err
	}
	// The shared handle carries receipts across the actual wire and both opening doors.
	cmd, ok := a.renew()
	if !ok {
		t.Fatal("new refused")
	}
	spend(t, a, cmd)
	firstShown := strings.Join(noteTexts(a), "\n")
	cmd, refusal := a.openBeside("/tmp", "/three.jsonl")
	if refusal != "" {
		t.Fatal(refusal)
	}
	spend(t, a, cmd)
	said := firstShown + "\n" + strings.Join(noteTexts(a), "\n")
	for _, line := range []string{"first final sentence", "second final sentence"} {
		if strings.Count(said, line) != 1 {
			t.Fatalf("window notes %q, want %q once", said, line)
		}
	}
	if got := loop.Client.Agent().TakeSavingNotices(); len(got) != 0 {
		t.Fatalf("undisplayed swap receipts: %q", got)
	}
}

type windowSwapSavingAgent struct {
	*servedAgent
	pending []string
}

func (a *windowSwapSavingAgent) TakeSavingNotices() []string {
	lines := a.pending
	a.pending = nil
	return lines
}
