package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"strings"
	"time"
)

func (b *appBackend) library() *knowledge.Vault {
	return knowledge.Open(config.ProfilePath(b.profileDir, ""))
}
func libraryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, knowledge.ErrPageChanged):
		return appwire.Errorf(appwire.CodeConflict, "Changed while you were editing. Your text is kept; reopen the page to review.")
	case errors.Is(err, knowledge.ErrInvalidTitle):
		return appwire.Errorf(appwire.CodeBadParams, "%s", err)
	case errors.Is(err, os.ErrNotExist):
		return appwire.Errorf(appwire.CodeNotFound, "This page left your Library")
	case errors.Is(err, knowledge.ErrDisabled):
		return appwire.Errorf(appwire.CodeConflict, "Build your Library first")
	default:
		return appwire.Errorf(appwire.CodeEngine, "Could not read or update your Library. Try again.")
	}
}
func (b *appBackend) KnowledgeSnapshot() (appwire.KnowledgeSnapshot, error) {
	b.watchLibrary()
	s, err := b.library().Snapshot()
	if err != nil {
		return appwire.KnowledgeSnapshot{}, libraryError(err)
	}
	r := appwire.KnowledgeSnapshot{CanRename: true, Revision: s.Revision, Enabled: s.Enabled, Pending: s.Pending, Model: s.Model, Pages: []appwire.KnowledgeHead{}, Build: appwire.KnowledgeBuild{Phase: s.Build.Phase, Chats: s.Build.Chats, Imported: s.Build.Imported, Sources: s.Build.Sources, Problems: s.Build.Problems}}
	if !s.Enabled {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		journals, e := session.KnowledgeHistory(session.PlacesRoot(), config.ProfilePath(b.profileDir, "")).Inventory(ctx)
		cancel()
		if e == nil {
			n := 0
			for _, j := range journals {
				if !j.Job {
					n++
				}
			}
			r.SavedChats = &n
		}
	}
	for _, p := range s.Pages {
		h := appwire.KnowledgeHead{ID: p.ID, Title: p.Title, TitleEdited: p.TitleEdited, Folder: append([]string{}, p.Folder...), Kind: p.Kind, Revision: p.Revision, HumanEdited: p.HumanEdited, NeedsEditorialUpdate: p.NeedsEditorialUpdate, Sections: []string{}}
		for _, e := range p.Evidence {
			if at, err := time.Parse(time.RFC3339Nano, e.TS); err == nil {
				previous, _ := time.Parse(time.RFC3339Nano, h.UpdatedAt)
				if at.After(previous) {
					h.UpdatedAt = e.TS
				}
			}
		}
		if p.HumanEditedAt != "" {
			at, _ := time.Parse(time.RFC3339Nano, p.HumanEditedAt)
			previous, _ := time.Parse(time.RFC3339Nano, h.UpdatedAt)
			if at.After(previous) {
				h.UpdatedAt = p.HumanEditedAt
			}
		}
		for _, section := range p.Sections {
			if h.Summary == "" && len(section.Paragraphs) > 0 {
				text := []rune(section.Paragraphs[0].Text)
				h.Summary = string(text[:min(len(text), 240)])
			}
			h.Sections = append(h.Sections, section.Heading)
		}
		for _, c := range p.Conflicts {
			if c.State == "unresolved" {
				h.HasConflict = true
			}
		}
		r.Pages = append(r.Pages, h)
	}
	return r, nil
}
func (b *appBackend) KnowledgeRead(id string) (knowledge.PageDetail, error) {
	p, e := b.library().ReadDetail(id)
	if e == nil {
		if p.Folder == nil {
			p.Folder = []string{}
		}
		if p.Sections == nil {
			p.Sections = []knowledge.Section{}
		}
		for i := range p.Sections {
			if p.Sections[i].Paragraphs == nil {
				p.Sections[i].Paragraphs = []knowledge.Paragraph{}
			}
		}
	}
	return p, libraryError(e)
}
func (b *appBackend) KnowledgeSearch(q string, limit int) ([]appwire.KnowledgeSearchHit, error) {
	hits, e := b.library().Search(q, limit)
	r := []appwire.KnowledgeSearchHit{}
	for _, h := range hits {
		r = append(r, appwire.KnowledgeSearchHit{ID: h.Page.ID, Title: h.Page.Title, Folder: append([]string{}, h.Page.Folder...), Snippet: h.Snippet})
	}
	return r, libraryError(e)
}
func (b *appBackend) KnowledgeRecall(q, scope string) (knowledge.Answer, error) {
	proc, e := b.process()
	if e != nil {
		return knowledge.Answer{}, libraryError(e)
	}
	ctx, cancel := context.WithTimeout(proc.lifetime(), 2*time.Minute)
	defer cancel()
	a, e := b.library().RecallSaved(ctx, config.APIKeyAt(b.profileDir), q, scope)
	return a, libraryError(e)
}
func (b *appBackend) libraryChanged() {
	b.syncLibraryInbox()
	if b.emit == nil {
		return
	}
	s, e := b.library().Status()
	if e == nil {
		b.emit.Emit(appwire.EventKnowledgeChanged, appwire.KnowledgeRevision{Revision: s.Revision})
	}
}
func (b *appBackend) KnowledgeEdit(p appwire.KnowledgeMutation) (appwire.KnowledgeRevision, error) {
	v := b.library()
	if e := v.EditPage(p.ID, p.Revision, p.Markdown); e != nil {
		return appwire.KnowledgeRevision{}, libraryError(e)
	}
	page, e := v.Read(p.ID)
	b.libraryChanged()
	return appwire.KnowledgeRevision{Revision: page.Revision}, libraryError(e)
}
func (b *appBackend) KnowledgeRename(p appwire.KnowledgeRename) (appwire.KnowledgeRevision, error) {
	v := b.library()
	if err := v.RenamePage(p.ID, p.Revision, p.Title); err != nil {
		return appwire.KnowledgeRevision{}, libraryError(err)
	}
	page, err := v.Read(p.ID)
	b.libraryChanged()
	return appwire.KnowledgeRevision{Revision: page.Revision}, libraryError(err)
}
func (b *appBackend) KnowledgeDelete(p appwire.KnowledgeMutation) error {
	if e := b.library().DeletePage(p.ID, p.Revision); e != nil {
		return libraryError(e)
	}
	b.libraryChanged()
	return nil
}
func (b *appBackend) KnowledgeBuild() error {
	if _, e := b.process(); e != nil {
		return libraryError(e)
	}
	if e := b.library().StartBuild(); e != nil {
		return libraryError(e)
	}
	b.libraryChanged()
	return nil
}

func (b *appBackend) KnowledgeSettle(p appwire.KnowledgeSettlement) (appwire.KnowledgeRevision, error) {
	if err := b.library().SettlePage(p.ID, p.Revision, p.Conflict, p.Keep, p.Note); err != nil {
		return appwire.KnowledgeRevision{}, libraryError(err)
	}
	page, err := b.library().Read(p.ID)
	b.libraryChanged()
	return appwire.KnowledgeRevision{Revision: page.Revision}, libraryError(err)
}
func (b *appBackend) KnowledgeSource(id, source string) (appwire.KnowledgeLocation, error) {
	page, err := b.library().ReadDetail(id)
	if err != nil {
		return appwire.KnowledgeLocation{}, libraryError(err)
	}
	location, ok := page.Sources[source]
	if !ok || location.Session == "" {
		return appwire.KnowledgeLocation{}, appwire.Errorf(appwire.CodeNotFound, "This source chat is unavailable")
	}
	var quote string
	for _, e := range page.Evidence {
		if e.Source == source {
			quote = e.Quote
			break
		}
	}
	if quote == "" {
		for _, c := range page.ClaimDetails {
			for _, e := range c.Evidence {
				if e.Source == source {
					quote = e.Quote
					break
				}
			}
		}
	}
	before := ""
	for n := 0; n < 10; n++ {
		entries, more, err := b.Entries(location.Session, before, 500)
		if err != nil {
			return appwire.KnowledgeLocation{}, appwire.Errorf(appwire.CodeNotFound, "Chat deleted or unavailable")
		}
		for _, entry := range entries {
			if quote != "" && strings.Contains(entry.Text, quote) && (entry.Kind == appwire.EntryUser || entry.Kind == appwire.EntryAnswer) {
				if stamp, e := time.Parse(time.RFC3339Nano, location.TS); e == nil && !entry.At.Equal(stamp) {
					continue
				}
				return appwire.KnowledgeLocation{Chat: location.Session, Message: entry.ID}, nil
			}
		}
		if !more || len(entries) == 0 {
			break
		}
		before = entries[0].ID
	}
	return appwire.KnowledgeLocation{}, appwire.Errorf(appwire.CodeNotFound, "The source message is unavailable")
}

// The compiler belongs to the process and may publish without a desk command.
// Sampling its small status row keeps every window informed without re-reading
// article bodies or starting another compiler worker.
func (b *appBackend) watchLibrary() {
	if b.emit == nil || b.left == nil {
		return
	}
	b.libraryWatch.Do(func() {
		guard.Go("app/library", func() {
			tick := time.NewTicker(2 * time.Second)
			defer tick.Stop()
			last := ""
			lastRevision := ""
			for {
				select {
				case <-b.left:
					return
				case <-tick.C:
					s, err := b.library().Status()
					if err != nil {
						continue
					}
					fingerprint := fmt.Sprintf("%s:%s:%d:%d:%d:%d:%t:%s", s.Revision, s.Build.Phase, s.Build.Imported, s.Build.Sources, s.Build.Problems, s.Pending, s.Enabled, s.Model)
					if s.Revision != lastRevision {
						lastRevision = s.Revision
						b.syncLibraryInbox()
					}
					if fingerprint != last {
						last = fingerprint
						b.emit.Emit(appwire.EventKnowledgeChanged, appwire.KnowledgeRevision{Revision: s.Revision})
					}
				}
			}
		})
	})
}

func (b *appBackend) KnowledgeCapture(p appwire.KnowledgeCapture) error {
	if !b.library().Enabled() {
		return libraryError(knowledge.ErrDisabled)
	}
	before := ""
	for n := 0; n < 10; n++ {
		entries, more, err := b.Entries(p.Chat, before, 500)
		if err != nil {
			return appwire.Errorf(appwire.CodeNotFound, "This chat is unavailable")
		}
		for _, entry := range entries {
			if entry.ID == p.Message {
				if entry.Kind != appwire.EntryAnswer || !strings.Contains(entry.Text, p.Text) {
					return appwire.Errorf(appwire.CodeBadParams, "Choose words from a saved agent reply")
				}
				sum := sha256.Sum256([]byte(p.Chat + "\x00" + p.Message + "\x00" + p.Text))
				source := knowledge.Source{ID: fmt.Sprintf("desk:%x", sum), Session: p.Chat, Text: p.Text, TS: entry.At.UTC().Format(time.RFC3339Nano), Role: "assistant", Kind: "agent_synthesis"}
				if err = b.library().Capture(source); err != nil {
					return libraryError(err)
				}
				b.libraryChanged()
				return nil
			}
		}
		if !more || len(entries) == 0 {
			break
		}
		before = entries[0].ID
	}
	return appwire.Errorf(appwire.CodeNotFound, "The saved reply is unavailable")
}
