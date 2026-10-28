package session

// The package declares a keys() of its own (a test helper), so the scanner
// package is named for what it is here.
import keyscan "github.com/Agent-Field/codeaf/internal/keys"

// KeptOut is a seal's grouped notice as facts ([Event.KeptOut]): the files one
// screen newly kept out of the saved history, more than one of them, and the
// sentence that counts them.
type KeptOut struct {
	// Lead is the notice's one-line summary: [Event.Text] without its list.
	Lead string `json:"Lead"`
	// Files is every file the notice names, in the order the screen found them.
	Files []KeptOutFile `json:"Files"`
}

// KeptOutFile is one file a grouped seal notice names.
type KeptOutFile struct {
	// Path is slash-separated and relative to the conversation's workspace.
	Path string `json:"Path"`
	// Rule is what kept it out: the secret rule that matched it ("api-key-sk"),
	// "dotenv-file" for an environment file, or "unreadable" for a path this
	// machine cannot read. Never the value.
	Rule string `json:"Rule"`
}

// keptOutOf is the facts of a grouped notice, nil for one that names a single
// file or none.
func keptOutOf(lead string, files []keyscan.Finding) *KeptOut {
	if lead == "" || len(files) == 0 {
		return nil
	}
	kept := &KeptOut{Lead: lead, Files: make([]KeptOutFile, 0, len(files))}
	for _, f := range files {
		kept.Files = append(kept.Files, KeptOutFile{Path: f.Path, Rule: f.Rule})
	}
	return kept
}

// SealFailing reports whether the last seal of this conversation's calls
// failed. It is the one predicate every reader asks: the in-process surface
// reads it directly, and an engine states it to a surface in another process on
// [Facts.Unsealed]. A conversation whose calls are not sealed at all, or whose
// seals hold, answers false.
func (a *Agent) SealFailing() bool {
	return a != nil && a.config.Seals != nil && a.config.Seals.Failing()
}

// noticeSealed says on the turn's own stream what the seat's seal watch has to
// say: the files a screen kept out of the saved history, a seal that failed, a
// seal that works again.
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
//
// A NOTICE THAT NAMES SEVERAL FILES TRAVELS AS FACTS TOO. Its words already
// list them, which is all a prose reader needs; the files and the lead ride
// beside the words ([Event.KeptOut]), typed [NoticeKindKeptOut], so a surface
// that folds the list away never has to read it back out of a sentence.
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
	take := func() (string, string, []keyscan.Finding) { return a.config.Seals.Take(), "", nil }
	if filed, ok := a.config.Seals.(SealFiles); ok {
		take = filed.TakeFiled
	}
	for {
		said, lead, files := take()
		if said == "" {
			return
		}
		event := Event{Kind: EventNotice, Text: said, Told: true}
		if kept := keptOutOf(lead, files); kept != nil {
			event.NoticeKind, event.KeptOut = NoticeKindKeptOut, kept
		}
		hub.send(event)
	}
}
