package session

// placemeta.go keeps meta.json true while the conversation is happening.
//
// A session folder's identity file is what a picker reads instead of parsing
// every journal on the machine (place.go), and the two facts it exists to carry
// — WHEN THE PERSON LAST SPOKE and WHAT THE CONVERSATION IS CALLED — are only
// knowable from inside the running session. So this is where they are written,
// on the two lines that establish them: the person's own message, and the name
// the session gives itself.
//
// IT IS THE JOURNAL'S SEAM, NOT THE SURFACE'S. Every door — the terminal, the
// engine at the far end of an ssh pipe, a headless --once — records a user
// message through one place ([Agent.recordUserLocked]), and stamping there is
// what makes the resume order true whichever door was used. A surface-side
// stamp would have to be repeated in each of them, and the one that was
// forgotten would be a door whose conversations never come back.
//
// EVERY FAILURE IS SILENCE. meta.json is a citation and not the record: the id
// is the folder's name, the title and the last-active stamp are recoverable
// from the transcript, and there is nothing a person mid-sentence could do with
// the news that a lookup file could not be written. [LoadMeta] already answers
// a missing or corrupt file with a blank Meta, so a stamp over one is a stamp
// that rebuilds it.

import (
	"strings"
	"time"
)

// metaTitleLimit is how much of the person's first line becomes the folder's
// working name. It is [taskLabelLimit]'s number for taskLabel's reason: this is
// what a picker row shows beside a glyph and an age, and a row is one line.
const metaTitleLimit = 56

// stampUserLocked records that the PERSON has just said something in this
// session. It runs with the agent lock held, from the one place a user message
// reaches the journal.
//
// The title it writes is a PLACEHOLDER — the person's own opening words — and
// only while there is none. The session names itself properly a turn later
// (title.go), and [Agent.stampTitle] replaces this with that. Between the two
// the picker still has a row a person recognizes, which is the whole difference
// between a list of conversations and a list of ids.
func (a *Agent) stampUserLocked(text string) {
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" {
		return
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		return
	}
	meta = a.fillMetaLocked(meta)
	meta.LastUserAt = time.Now()
	if strings.TrimSpace(meta.Title) == "" {
		meta.Title = clip(strings.Join(strings.Fields(firstLine(text)), " "), metaTitleLimit)
	}
	_ = SaveMeta(dir, meta)
}

// stampTitle records the name the session gave itself. It takes no lock of its
// own beyond the one [Agent.fillMetaLocked] needs, which is why it is called
// from [Agent.setTitle] with the agent lock released and re-taken: writing a
// file is not work to do under the lock a turn is waiting on.
func (a *Agent) stampTitle(title string) {
	title = strings.TrimSpace(title)
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" || title == "" {
		return
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		return
	}
	a.mu.Lock()
	meta = a.fillMetaLocked(meta)
	a.mu.Unlock()
	meta.Title = clip(title, metaTitleLimit)
	_ = SaveMeta(dir, meta)
}

// fillMetaLocked fills in everything about a session that does not change while
// it runs, so that a meta.json written by an older build — or lost entirely —
// comes back whole rather than half-filled. The agent lock is held for the
// model, which /model moves.
func (a *Agent) fillMetaLocked(meta Meta) Meta {
	if strings.TrimSpace(meta.ID) == "" {
		meta.ID = a.config.Place.ID()
	}
	if strings.TrimSpace(meta.Workspace) == "" {
		workspace := strings.TrimSpace(a.config.Place.Workspace)
		if workspace == "" {
			workspace = a.config.Workspace
		}
		meta.Workspace = workspace
	}
	meta.Owned = a.config.Place.Owned
	if meta.Created.IsZero() {
		meta.Created = time.Now()
	}
	if model := strings.TrimSpace(a.model); model != "" {
		meta.Model = model
	}
	return meta
}
