package main

// ── looking into a task another conversation is running ─────────────────────
//
// This is the door behind [tui3.Options.OpenTaskOwner]: a person presses a row
// of work on the tasks page, that work belongs to a conversation this window is
// not in, and the page they get is that task's own transcript rather than a card
// explaining why they cannot have one.
//
// IT IS THE ROAD THIS LAUNCH IS ALREADY ON, dialled a second time. `codeaf chat`
// on this machine is a surface talking to the workspace's engine over a unix
// socket (chatv3_local.go), the engine holds every conversation open, and
// internal/remote has served several surfaces onto one conversation since version
// 2. So there is nothing to build: one more connection, saying which conversation
// and that it is here to read.
//
// THREE THINGS MAKE IT SAFE, and each is a flag with its reason written on it in
// internal/remote's wire.go:
//
//   - [remote.Hello.Join] — take the conversation that is ALREADY OPEN under this
//     transcript, and never start one. A hello that could boot would answer a
//     question about running work by starting a model.
//   - [remote.Hello.Watch] — never take the keyboard, not even when it is going
//     spare. `Back` was not enough: it hands the keyboard to a returning surface
//     whenever the driver has stepped away, which is precisely the window that
//     owns this work.
//   - [enginehost.Dial] — connect to a host that is there, and start nothing. The
//     spawn in [localLink.dial] is right for a launch and wrong for a look.
//
// And the surface checks the identity of what comes back before it draws a row.

import (
	"errors"
	"io"
	"strings"

	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// taskOwnerNotHere is what this door says when the workspace has no host, or the
// host is not running that conversation. It is a fact about where the work is,
// not a fault, and the surface puts it beside the recovery card.
var taskOwnerNotHere = errors.New("that conversation is not open on this machine")

// localTaskOwnerDoor is the callback the local launch hands the surface.
//
// The workspace it closes over is the one this window is in, and it is the
// FALLBACK rather than the answer: a row carries the project its conversation
// belongs to, and a person on this page may be looking at work from a folder next
// door.
func localTaskOwnerDoor(workspace string) func(tui3.TaskOwnerAsk) (tui3.TaskOwnerView, error) {
	return func(ask tui3.TaskOwnerAsk) (tui3.TaskOwnerView, error) {
		return openTaskOwnerView(firstEngineWord(strings.TrimSpace(ask.Workspace), workspace), ask)
	}
}

// openTaskOwnerView is the attach itself.
//
// IT IS CALLED OFF THE SURFACE'S PROGRAM LOOP (internal/tui3's taskowner.go runs
// it as a command), so it is free to spend a round trip here — and it must,
// because a socket that does not answer would otherwise freeze the window.
func openTaskOwnerView(workspace string, ask tui3.TaskOwnerAsk) (tui3.TaskOwnerView, error) {
	file := strings.TrimSpace(ask.Session)
	if workspace == "" || file == "" {
		return tui3.TaskOwnerView{}, taskOwnerNotHere
	}
	// NOTHING IS STARTED TO ANSWER THIS. [enginehost.Dial] connects to a host that
	// is already listening and fails otherwise, which is the same question
	// [v3HostAnswers] asks before a headless message and for the same reason: a
	// resident process left behind by somebody glancing at a row is a surprise.
	dial := func() (io.ReadWriteCloser, error) { return dialTaskOwnerHost(workspace) }
	client, err := remote.Roam("", remote.Hello{
		Workspace: workspace,
		Session:   file,
		Join:      true,
		Watch:     true,
	}, remote.Roaming{Dial: dial})
	if err != nil {
		return tui3.TaskOwnerView{}, err
	}
	welcome := client.Welcome()
	agent := client.Agent()
	if agent == nil {
		_ = client.Close()
		return tui3.TaskOwnerView{}, taskOwnerNotHere
	}
	// THE CONNECTION IS HANDED OVER WHOLE OR NOT AT ALL, and the surface makes the
	// identity check itself against what it asked for — this end reports what the
	// engine said and does not decide. Close is THIS client's and nothing else:
	// the conversation, its turn, its tasks and the window that owns it are
	// untouched by it.
	return tui3.TaskOwnerView{
		Session: welcome.SessionFile,
		Room:    agent.TaskRoom,
		// AND THE OWNER'S OWN ACCOUNT OF ITS WORK comes back with it. This is the
		// standing subscription that conversation already publishes to every surface
		// attached to it — the roster replayed on open, then one notice per change —
		// and it is a READ: it is on the watcher's allow-list beside the journal
		// reading (internal/remote's driver.go), and it neither takes the keyboard
		// nor starts anything.
		Watch: agent.WatchTaskUpdates,
		// AND WHETHER THAT CONVERSATION HAS STOPPED AND IS WAITING ON A PERSON.
		// It is the second standing read on the same connection and it is a read
		// in the same sense: [remote.MethodQuestionWatch] is on the watcher's
		// allow-list beside the roster's, and the ANSWERING door deliberately is
		// not — the page draws the question and the window that owns the work
		// answers it (internal/remote's driver.go).
		Questions: agent.WatchQuestions,
		// AND ONE TASK'S STORED PAGE, which is the whole of what a program's task
		// has to read: senior-dev writes no worker journal, and its actions are on
		// its page in the owner's store. It is a read in the same sense —
		// [remote.MethodPlanTaskPage] is on the watcher's allow-list and none of
		// the page's verbs are — and it keeps the engine's refusal, which is how a
		// program's page learns that the conversation under it was replaced.
		TaskPage: agent.ReadPlanTaskPage,
		Close:    client.Close,
	}, nil
}

// dialTaskOwnerHost connects without starting anything. A task row can be
// opened while the already-running surface's host is restarting, so only the
// stale-socket replacement sequence gets the same short grace as a headless
// message: refused first, then refused or absent while the socket is replaced.
func dialTaskOwnerHost(workspace string) (io.ReadWriteCloser, error) {
	// The same startup window as a headless launch: retry a refused connect only
	// while the host lock is held ([enginehost.DialStartingHost]). The nil check
	// keeps a failed dial from returning a non-nil interface around a nil conn.
	conn, err := enginehost.DialStartingHost(workspace)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
