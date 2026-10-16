package session

import (
	"errors"
	"strings"
)

// AppNameLimit is how many characters a conversation's name may hold on the
// desktop, the ellipsis a cut one ends with INCLUDED. A character is a Unicode
// code point.
const AppNameLimit = 40

// FirstMessageName is a message's words as a name: whitespace folded and the
// result held to [AppNameLimit] characters by [ClipName]. A conversation named
// by intent does not use it for a person's message ([OpeningName] carries the
// attached files' names too); it stays the name of a team member started by a
// brief.
func FirstMessageName(text string) string { return ClipName(text) }

// savedTitle is the name a journal's title line carries: a literal name kept
// exactly, a generated label healed ([healedTitle]).
func (entry sessionEntry) savedTitle() string {
	if entry.LiteralName {
		return strings.TrimSpace(entry.Title)
	}
	return healedTitle(entry.Title)
}

// stampLiteralName writes a rule's name onto the folder's row, marked literal
// so a later read keeps it as written.
func (a *Agent) stampLiteralName(name string) {
	dir, snapshot := a.metaSnapshotAt()
	a.updateMeta(dir, snapshot, func(meta *Meta) {
		meta.Title, meta.ShortTitle, meta.LiteralName = name, "", true
	})
}

// RenameByPerson gives the conversation the name a person typed. It is the
// person's word and so outranks every namer: it is written as a literal name to
// the journal's title line, which a resume reads back, and to the folder's
// row, and a namer running beside the turn finds the conversation already
// named ([Agent.setTitleIfUnnamed]) and leaves it.
func (a *Agent) RenameByPerson(title string) error {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return errors.New("a name needs some words")
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errors.New("this conversation has closed")
	}
	a.title, a.titleProvisional, a.titleLiteral = title, "", true
	// THE LINE IS WRITTEN UNDER THE LOCK, so a name a model earned in the same
	// moment cannot follow this one into the file: an answer reaches the journal
	// only while it still holds the claim this cleared (intentname.go).
	if a.file != nil {
		a.file.appendNamedTitle(title, true)
	}
	// Queue under the mutation lock: every standing/turn subscriber sees a
	// manual rename after any generated event already queued.
	a.announceTitleLocked()
	a.mu.Unlock()
	a.stampCurrentName()
	return nil
}

// stampCurrentName writes the current name, never a captured earlier proposal.
// A newer rename may arrive during disk I/O; its stamp waits here, then takes
// a fresh snapshot, so the last write cannot restore an older name.
func (a *Agent) stampCurrentName() {
	a.titleStampMu.Lock()
	defer a.titleStampMu.Unlock()
	a.mu.Lock()
	dir, snapshot := a.config.Place.Dir, a.fillMetaLocked(Meta{})
	name, literal := a.title, a.titleLiteral
	a.mu.Unlock()
	a.updateMeta(dir, snapshot, func(meta *Meta) {
		meta.Title, meta.ShortTitle, meta.LiteralName = name, "", literal
	})
}
