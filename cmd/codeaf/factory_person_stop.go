package main

import "github.com/Agent-Field/codeaf/internal/factory"

// AutomaticRefusal reads the current durable job for every automatic wake.
// A person opening or reading the conversation does not change that job.
func (d seamRunDoor) AutomaticRefusal(conversation string) string {
	it, err := d.managed(conversation)
	if err == factory.ErrNotManager {
		return ""
	}
	if err != nil {
		return "job could not be read; automatic work stopped"
	}
	if err := factory.CheckPersonStop(it); err != nil {
		return err.Error()
	}
	return ""
}

// WatchedRefusal is AutomaticRefusal for the watcher that looks ten times a
// second for as long as a job's chat is open. It costs one look at the floor's
// folder, not a read of every job ([store.Store.TalkStop]). What grants work
// still asks AutomaticRefusal.
func (d seamRunDoor) WatchedRefusal(conversation string) string {
	want := sameFileKey(conversation)
	if want == "" || d.st == nil {
		return ""
	}
	manager, stopped, err := d.st.TalkStop(want)
	if err != nil {
		return "job could not be read; automatic work stopped"
	}
	if manager && stopped {
		return factory.PersonStopReason
	}
	return ""
}
