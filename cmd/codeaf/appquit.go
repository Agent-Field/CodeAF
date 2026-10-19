package main

// ── what the app's quit leaves behind, and what it picks up again ───────────
//
// QUITTING THE APP PAUSES ITS AGENTS AND STOPS ITS PROCESSES. A reply in the
// middle of its work is stopped where it stands and carries on by itself when
// the app opens again, with "Resumed after quit" in front of it; it is not left
// reading failed or interrupted. A process the model started is sent SIGTERM,
// then SIGKILL after appQuitGraceS seconds (the conversation's own shutdown,
// given the app's grace), and is listed afterwards as "stopped by quit". What
// runs on a schedule is untouched: an automation is the store's, not this
// process's, and fires again once the app is open.
//
// The list of what to pick up is kept in the app's state file, written before
// anything is stopped, so a quit that never finishes still leaves it behind.

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	// appQuitGraceS is how long a running process has between SIGTERM and
	// SIGKILL when the app quits. The window waits a little longer than this
	// before it ends the app's process.
	appQuitGraceS = 5
	// appResumedWord is the line in front of a reply that carries on after a
	// quit, and appQuitStoppedWord is a process's reason after one.
	appResumedWord     = "Resumed after quit"
	appQuitStoppedWord = "stopped by quit"
)

// appResumer is a conversation that can carry on after the app's quit.
type appResumer interface {
	ResumeAfterQuit() (bool, error)
}

// markQuit writes down, before anything stops, which agents were in the
// middle of a reply and which of their processes were running.
func (b *appBackend) markQuit(agents []*appAgent) {
	var resume, tasks []string
	var stopped []appwire.Process
	now := b.now()
	for _, a := range agents {
		live := b.liveOf(a)
		if live == nil || !live.conn.Assembled {
			continue
		}
		id, running := "", false
		b.locked(func() { id, running = a.view.ID, live.running })
		b.locked(func() {
			for key, task := range live.tasks {
				if task.State == appwire.TaskRunning {
					tasks = append(tasks, key)
				}
			}
		})
		if running || len(tasks) > 0 {
			resume = append(resume, id)
		}
		rows, err := b.runningJobs(live)
		if err != nil {
			continue
		}
		for _, row := range rows {
			process := appProcessOf(id, row)
			ended := now
			process.State, process.EndedAt, process.Reason = appwire.ProcessStopped, &ended, appQuitStoppedWord
			stopped = append(stopped, process)
		}
	}
	if len(resume) == 0 && len(stopped) == 0 {
		return
	}
	// Only a reply in the middle of its work is carried on as a turn; an
	// agent listed for its tasks alone has them picked up and nothing said.
	var replies []string
	for _, id := range resume {
		for _, a := range agents {
			if live := b.liveOf(a); live != nil {
				running := false
				b.locked(func() { running = a.view.ID == id && live.running })
				if running {
					replies = append(replies, id)
				}
			}
		}
	}
	_ = b.state.write(func(f *appStateFile) error {
		f.ResumeAfterQuit, f.QuitReplies, f.QuitTasks, f.QuitStopped = resume, replies, tasks, stopped
		return nil
	})
}

// resumeAfterQuit carries on every reply the last quit stopped, once. Each is
// opened, told it carries on, and does so as a turn of its own.
func (b *appBackend) resumeAfterQuit() {
	// Preserve the pending resume list if its actual budget cannot be established.
	if err := b.settleDayLimit(); err != nil {
		return
	}
	var resume, replies, tasks []string
	_ = b.state.write(func(f *appStateFile) error {
		resume, replies, tasks = f.ResumeAfterQuit, f.QuitReplies, f.QuitTasks
		f.ResumeAfterQuit, f.QuitReplies, f.QuitTasks = nil, nil, nil
		return nil
	})
	for _, id := range resume {
		reply := false
		var mine []string
		for _, r := range replies {
			reply = reply || r == id
		}
		for _, t := range tasks {
			if agent, _, ok := appSplitID(t); ok && agent == id {
				mine = append(mine, t)
			}
		}
		guard.Go("app/resume", func() { b.resumeOne(id, reply, mine) })
	}
}

// resumeOne carries on one agent's reply, when it was in the middle of one,
// and every task it had running: a task the quit left with nothing driving it
// is picked up where its record stands.
func (b *appBackend) resumeOne(id string, reply bool, tasks []string) {
	b.resumeOneWith(id, reply, tasks, appwire.FooterResumedAfterQuit, appResumedWord, "quit-"+b.now().UTC().Format("20060102T150405.000000000Z"))
}

// resumeOneWith shares the held conversation, task continuation and private
// wake, while preserving the quit road's own notice and model instruction.
func (b *appBackend) resumeOneWith(id string, reply bool, tasks []string, kind, words, key string) {
	if b.isClosing() {
		return
	}
	a, live, err := b.heldLive(id)
	if err != nil {
		return
	}
	if kind == appwire.FooterResumedAfterClose && !live.conn.Assembled {
		return
	}
	for _, task := range tasks {
		if _, local, ok := appSplitID(task); ok {
			_ = live.conn.Conv.AppTaskAction(session.AppTaskIntent{ID: local, Action: "continue"})
		}
	}
	if !reply {
		return
	}
	var resume func() (bool, error)
	if kind == appwire.FooterResumedAfterClose {
		door, ok := live.conn.Conv.(interface{ ResumeAfterClose() (bool, error) })
		if !ok {
			return
		}
		resume = door.ResumeAfterClose
	} else {
		door, ok := live.conn.Conv.(appResumer)
		if !ok {
			return
		}
		resume = door.ResumeAfterQuit
	}
	// A NOTICE NEVER COSTS THE PERSON THE REPLY. The close road kept its footer
	// before it counted the attempt; a footer the disk will not take here is
	// said live instead, and the reply carries on as the quit road always did.
	if err := b.keepAppFooter(a, key, kind, words); err != nil {
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appFooter(key, id, 0, kind, words, b.now())})
	} else {
		b.emitAppFooter(a, key)
	}
	_, _ = resume()
}

// quitStopped lays the last quit's stopped processes over an agent's list:
// one the list still names is said as stopped by the quit, and one it no
// longer names is added.
func (b *appBackend) quitStopped(agent string, list []appwire.Process) []appwire.Process {
	var stopped []appwire.Process
	_ = b.state.read(func(f *appStateFile) {
		for _, p := range f.QuitStopped {
			if p.Agent == agent {
				stopped = append(stopped, p)
			}
		}
	})
	for _, p := range stopped {
		found := false
		for i := range list {
			if list[i].ID == p.ID {
				found = true
				if list[i].State != appwire.ProcessRunning {
					list[i].State, list[i].Reason, list[i].ExitCode = appwire.ProcessStopped, appQuitStoppedWord, nil
				}
			}
		}
		if !found {
			list = append(list, p)
		}
	}
	return list
}
