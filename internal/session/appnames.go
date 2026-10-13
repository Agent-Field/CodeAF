package session

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// AppNameLimit is the app's deterministic name and rename limit, in characters.
const AppNameLimit = 40

// FirstMessageName folds whitespace and keeps the first forty Unicode characters.
// Empty agents stay unnamed until a first message or an explicit rename arrives.
func FirstMessageName(text string) string {
	name := []rune(strings.Join(strings.Fields(text), " "))
	if len(name) > AppNameLimit {
		name = name[:AppNameLimit]
	}
	return strings.TrimSpace(string(name))
}

// Rename gives the person's words precedence over an outstanding naming errand.
// The transcript and its lookup metadata retain the same name across reopening.
func (a *Agent) Rename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > AppNameLimit || strings.ContainsAny(name, "\r\n\x00") {
		return errors.New("agent name must be one line of 1 to 40 characters")
	}
	a.titleWriteMu.Lock()
	defer a.titleWriteMu.Unlock()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errAgentClosed
	}
	a.titleJobs.Add(1)
	defer a.titleJobs.Done()
	a.title, a.titleTried = name, true
	file, hub := a.file, a.hub
	watchers := append([]*eventStream(nil), a.titleWatchers...)
	a.mu.Unlock()
	if file != nil {
		file.appendNamedTitle(name, true)
	}
	a.stampPersonName(name)
	ev := Event{Kind: EventTitleChanged, Text: name}
	if hub != nil {
		hub.send(ev)
	}
	for _, watcher := range watchers {
		watcher.send(ev)
	}
	return nil
}

// Explicit names keep their exact Unicode words; AI label repair must not
// strip a person's prefix or cut a forty-character name at an eighty-byte cap.
func (entry sessionEntry) savedTitle() string {
	if entry.LiteralName {
		return strings.TrimSpace(entry.Title)
	}
	return healedTitle(entry.Title)
}

func (a *Agent) stampPersonName(name string) {
	dir, snapshot := a.metaSnapshotAt()
	a.updateMeta(dir, snapshot, func(meta *Meta) {
		meta.Title, meta.ShortTitle, meta.LiteralName = name, "", true
	})
}
