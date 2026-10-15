package session

import (
	"errors"
	"strings"
)

// AppNameLimit is how many characters of the first message a deterministic
// name keeps before it is cut and marked with an ellipsis.
const AppNameLimit = 40

// FirstMessageName is the desktop app's naming rule: the first message with its
// whitespace folded, kept whole when it fits in [AppNameLimit] characters and
// otherwise cut at the last word boundary inside the limit (or at the limit,
// for one long word) and ended with "…". It is a rule rather than a model's
// summary so the app can show the same name the moment the message is sent.
func FirstMessageName(text string) string {
	folded := []rune(strings.Join(strings.Fields(text), " "))
	if len(folded) <= AppNameLimit {
		return string(folded)
	}
	cut := folded[:AppNameLimit]
	if at := strings.LastIndex(string(cut), " "); at > 0 {
		cut = []rune(string(cut)[:at])
	}
	return strings.TrimSpace(string(cut)) + "…"
}

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
	a.title = title
	file := a.file
	a.mu.Unlock()
	if file != nil {
		file.appendNamedTitle(title, true)
	}
	a.stampLiteralName(title)
	return nil
}
