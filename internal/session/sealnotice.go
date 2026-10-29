package session

// SealNews is what a seal watch may also do so an idle host waits for its news
// instead of asking: Changes is signalled whenever a sentence is queued, and
// Owed says whether the host must look again although nothing woke it (a
// sentence is waiting, or sealing is failing and its recovery is found by
// asking). Owed never looks for that recovery itself.
type SealNews interface {
	Changes() <-chan struct{}
	Owed() bool
}

// SealNotices gives the host a wake for idle saving news. A nil wake with true
// means nothing can be taken here; false keeps watches without signals polling.
func (a *Agent) SealNotices() (<-chan struct{}, bool) {
	if a == nil || a.config.InTask || a.config.Errand || a.config.Seals == nil {
		return nil, true
	}
	if news, ok := a.config.Seals.(SealNews); ok {
		return news.Changes(), true
	}
	return nil, false
}

// SealNoticesOwed keeps a host looking while recovery can change on disk or a
// sentence is held for a running turn or an absent surface. The host's own
// TakeSealNotice is what finds a recovery, as it always was; this only says
// whether to come back.
func (a *Agent) SealNoticesOwed() bool {
	if a == nil || a.config.InTask || a.config.Errand || a.config.Seals == nil {
		return false
	}
	if news, ok := a.config.Seals.(SealNews); ok {
		return news.Owed()
	}
	return a.config.Seals.Failing()
}

// SealFailing reports whether the last seal of this conversation's calls
// failed. It is the one predicate every reader asks: the in-process surface
// reads it directly, and an engine states it to a surface in another process on
// [Facts.Unsealed]. A conversation whose calls are not sealed at all, or whose
// seals hold, answers false.
func (a *Agent) SealFailing() bool {
	return a != nil && a.config.Seals != nil && a.config.Seals.Failing()
}

// TakeSealNotice forwards saving news after a turn's event stream has ended.
// A running turn keeps its own notices on that stream.
func (a *Agent) TakeSealNotice() string {
	if a == nil || a.config.InTask || a.config.Errand || a.config.Seals == nil {
		return ""
	}
	a.mu.Lock()
	running := a.running
	a.mu.Unlock()
	if running {
		return ""
	}
	return a.config.Seals.Take()
}

// noticeSealed says on the turn's own stream what the seat's seal watch has to
// say: a file kept out of the saved history, a seal that failed, a seal that
// works again.
//
// THE WATCH LIVES WHERE THE SEAT DOES. A chat runs its agent in an engine
// process that the screen is only connected to, so a sentence held in the
// watch never reached a screen that was not in that process: a folder with a
// file nobody could read made every seal fail, and the person saw nothing.
// The turn's event stream is the road every other one-line notice takes to the
// screen, so the sentences are handed to it at the boundary where the last seal
// of the turn has just been made, marked as told to the person so the screen
// does not fold them away with the work's dim facts. A turn with nobody
// listening leaves them in the watch for a surface that asks it directly.
func (a *Agent) noticeSealed() {
	if a == nil || a.config.InTask || a.config.Errand || a.config.Seals == nil {
		return
	}
	a.mu.Lock()
	hub, running := a.hub, a.running
	a.mu.Unlock()
	if !running || hub == nil {
		return
	}
	for said := a.config.Seals.Take(); said != ""; said = a.config.Seals.Take() {
		hub.send(Event{Kind: EventNotice, Text: said, Told: true})
	}
}
