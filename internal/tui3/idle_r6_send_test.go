package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
)

type r6OfflineSendAgent struct {
	*walkSendAgent
	offline bool
}

func (a *r6OfflineSendAgent) CheckWork() error {
	if a.offline {
		return &executor.WorkRefusal{Line: chatlist.TakeBackUnreachable, Retryable: true}
	}
	return a.walkSendAgent.CheckWork()
}

func TestIdleRelease_R6_ReleasedWindowOfflineSendSaysWhy(t *testing.T) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	source := walkSendSource{&continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Idle, Mine: true, Released: true, AutoOpen: true, Fence: 2, Head: "saved", DeviceID: "A"}}}
	taker := &fakeTaker{}
	a, old := heldLab(t, source, taker)
	keepSurfaceWorkersInTest(t, a)
	a.closeHome()
	root := session.DirOf(a.file)
	if err := os.MkdirAll(filepath.Join(root, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cell", "adopted.json"), []byte(`{"V":1,"head":"saved","turns_size":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	agent := &r6OfflineSendAgent{walkSendAgent: &walkSendAgent{fakeAgent: old, source: source}, offline: true}
	a.agent = agent
	const words = "keep my unsent words"
	a.input.setText(words)
	drive(t, a, key("enter"))
	if a.input.String() != words || len(old.sent) != 0 || len(taker.cells) != 0 {
		t.Fatal("offline refusal spent the draft or took the chat")
	}
	notes := strings.Join(noteTexts(a), "\n")
	if strings.Count(notes, chatlist.TakeBackUnreachable) != 1 || strings.Contains(notes, chatlist.Available) {
		t.Fatalf("offline send must explain the cause once: %s", notes)
	}
	if _, card := a.homeAsking(); card {
		t.Fatal("offline send offered a displacement card")
	}
	agent.offline = false
	drive(t, a, key("enter"))
	rows, _ := source.Rows(t.Context())
	if len(old.sent) != 1 || old.sent[0] != words || rows[0].Fence != 3 || len(taker.cells) != 0 {
		t.Fatalf("retry did not send the kept draft at a new fence: sent=%v rows=%+v", old.sent, rows)
	}
}

func TestIdleOpen_R6_LapsedWorkingSendKeepsTheCard(t *testing.T) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	source := walkSendSource{&continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Off, Fence: 2, Head: "saved", DeviceID: "B"}}}
	taker := &returningTaker{}
	a, old := heldLab(t, source, taker)
	keepSurfaceWorkersInTest(t, a)
	a.closeHome()
	root := session.DirOf(a.file)
	if err := os.MkdirAll(filepath.Join(root, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cell", "adopted.json"), []byte(`{"V":1,"head":"saved","turns_size":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	a.input.setText("keep the words until I confirm")
	drive(t, a, key("enter"))
	if len(taker.cells) != 0 || len(old.sent) != 0 || string(a.mainComposer().box.value) != "keep the words until I confirm" {
		t.Fatalf("send silently took a lapsed hold: takes=%v sends=%v", taker.cells, old.sent)
	}
	if _, card := a.homeAsking(); !card {
		t.Fatal("a lapsed holder's unfinished work lost its Continue-here card")
	}
}
