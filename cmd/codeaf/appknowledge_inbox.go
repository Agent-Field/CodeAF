package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"strings"
)

// Library questions are doors to the guide's own choice, not agent requests.
// Keeping them in the same Inbox inventory makes a choice from either place
// remove the other row, without making up a conversation to own the question.
func (b *appBackend) syncLibraryInbox() {
	b.libraryInboxMu.Lock()
	defer b.libraryInboxMu.Unlock()
	in := b.inbox
	if in == nil {
		return
	}
	snapshot, err := b.library().Snapshot()
	if err != nil {
		return
	}
	savedLater := map[string]bool{}
	if b.state != nil {
		if err := b.state.read(func(f *appStateFile) {
			for id, later := range f.LibraryLater {
				savedLater[id] = later
			}
		}); err != nil {
			return
		}
	}
	wanted := map[string]appwire.InboxItem{}
	for _, p := range snapshot.Pages {
		for _, c := range p.Conflicts {
			if c.State != "unresolved" {
				continue
			}
			agent := "library:" + p.ID
			ask := knowledge.ConflictID(c)
			id := appInboxID(agent, ask)
			title := "Two chats disagree"
			if c.Reason != "" {
				title += " about " + c.Reason
			}
			wanted[id] = appwire.InboxItem{ID: id, Ask: appwire.Ask{ID: ask, Agent: agent, Kind: appwire.AskQuestion, Title: title, Detail: "Open the Library guide to compare the sources and keep an account."}, Where: appwire.InboxWhere{Agent: agent, Path: []string{"Library", p.Title}}, At: b.now(), List: appwire.InboxWaiting, Context: []appwire.Entry{}}
		}
	}
	for id, item := range wanted {
		if savedLater[id] {
			item.List = appwire.InboxLater
			item.Ask.Later = true
			wanted[id] = item
		}
	}
	added := []appwire.InboxItem{}
	removed := []string{}
	in.locked(func() {
		for id, item := range wanted {
			if prior := in.items[id]; prior != nil {
				continue
			}
			copy := item
			in.items[id] = &copy
			in.order = append(in.order, id)
			added = append(added, item)
		}
		for id := range in.items {
			if strings.HasPrefix(id, "library:") {
				if _, ok := wanted[id]; !ok {
					removed = append(removed, id)
				}
			}
		}
	})
	for _, item := range added {
		in.next.Emit(appwire.EventInboxAdded, appwire.InboxAddedEvent{Item: item})
	}
	for _, id := range removed {
		b.inboxResolved(id, appwire.Entry{Text: "The Library guide no longer needs this choice."})
	}
	if len(added) > 0 {
		b.inboxCounts()
	}
}
func (b *appBackend) laterLibraryInbox(id string, later bool) error {
	b.libraryInboxMu.Lock()
	defer b.libraryInboxMu.Unlock()
	in := b.inbox
	found := false
	in.locked(func() { found = in.items[id] != nil })
	if !found {
		return appwire.Errorf(appwire.CodeNotFound, "This Library choice is no longer waiting")
	}
	if b.state == nil {
		return appwire.Errorf(appwire.CodeEngine, "Later is unavailable without saved app state")
	}
	if err := b.state.write(func(f *appStateFile) error {
		if f.LibraryLater == nil {
			f.LibraryLater = map[string]bool{}
		}
		if later {
			f.LibraryLater[id] = true
		} else {
			delete(f.LibraryLater, id)
		}
		return nil
	}); err != nil {
		return libraryError(err)
	}

	var item appwire.InboxItem
	in.locked(func() {
		if p := in.items[id]; p != nil {
			p.List = appwire.InboxWaiting
			if later {
				p.List = appwire.InboxLater
			}
			p.Ask.Later = later
			item = *p
			found = true
		}
	})
	if !found {
		return appwire.Errorf(appwire.CodeNotFound, "This Library choice is no longer waiting")
	}
	in.next.Emit(appwire.EventInboxAdded, appwire.InboxAddedEvent{Item: item})
	b.inboxCounts()
	return nil
}
