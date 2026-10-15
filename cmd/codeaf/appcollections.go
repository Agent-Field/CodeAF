package main

// ── codeaf app: COLLECTIONS (wave 9, V75) ───────────────────────────────────
//
// A collection is a hand-picked list of references, kept in the same store
// `codeaf collections` reads (<home>/v3/collections.db, internal/workspace).
// This file is the app's door into it and nothing more: the store keeps the
// lists, and what each member is NOW is asked of its owner when a collection
// is opened and never written down.
//
// RESOLUTION ASKS EACH OWNER ONCE PER CALL, NEVER ONCE PER MEMBER. A call
// takes one agents listing (every state, closed and archived included), one
// crews listing, one task list and one process list per distinct agent, and
// one look for a git work tree per directory; a collection of a thousand
// files in one repository costs one walk up to its .git. No transcript is
// read to resolve anything, and a member whose owner cannot be asked is
// "unknown", which says nothing about the member itself, with a reason that
// names the owner and why it could not be asked.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/workspace"
)

// appCollectionsPath is where the collections store is, the CLI's own default.
func (b *appBackend) appCollectionsPath() string {
	if b.collectionsPath != "" {
		return b.collectionsPath
	}
	return home.Join("v3", "collections.db")
}

// collectionStore opens the store once and answers it. With create false a
// missing file answers nil: A READ NEVER CREATES THE DATABASE, the rule the
// CLI keeps, so a fresh home lists no collections and leaves no file behind.
func (b *appBackend) collectionStore(create bool) (*workspace.Store, error) {
	b.collectionsMu.Lock()
	defer b.collectionsMu.Unlock()
	if b.collections != nil {
		return b.collections, nil
	}
	if b.collectionsClosed {
		return nil, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	path := b.appCollectionsPath()
	if !create {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		} else if err != nil {
			return nil, appEngineError(err)
		}
	}
	s, err := workspace.Open(path)
	if err != nil {
		return nil, appCollectionError(err)
	}
	b.collections = s
	return s, nil
}

// existingStore is the store for a change to a collection that must already
// exist: a missing file has none, so the answer is not_found.
func (b *appBackend) existingStore() (*workspace.Store, error) {
	s, err := b.collectionStore(false)
	if err == nil && s == nil {
		err = appwire.Errorf(appwire.CodeNotFound, "collection not found")
	}
	return s, err
}

// closeCollections closes the store at the backend's end.
func (b *appBackend) closeCollections() {
	b.collectionsMu.Lock()
	defer b.collectionsMu.Unlock()
	b.collectionsClosed = true
	if b.collections != nil {
		_ = b.collections.Close()
		b.collections = nil
	}
}

// appCollectionError is a store refusal in the wire's codes.
func appCollectionError(err error) error {
	var wire *appwire.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &wire):
		return wire
	case errors.Is(err, workspace.ErrNameTaken):
		return appwire.Errorf(appwire.CodeConflict, "%s", workspace.ErrNameTaken.Error())
	case errors.Is(err, workspace.ErrCycle):
		return appwire.Errorf(appwire.CodeCycle, "%s", workspace.ErrCycle.Error())
	case errors.Is(err, workspace.ErrNotFound):
		return appwire.Errorf(appwire.CodeNotFound, "%s", workspace.ErrNotFound.Error())
	case errors.Is(err, workspace.ErrNotMember):
		return appwire.Errorf(appwire.CodeNotFound, "%s", workspace.ErrNotMember.Error())
	case errors.Is(err, workspace.ErrInvalid):
		return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	case errors.Is(err, workspace.ErrBusy):
		return appwire.Errorf(appwire.CodeBusy, "%s", err.Error())
	}
	return appEngineError(err)
}

func (b *appBackend) collectionChanged(id string, deleted bool) {
	b.emit.Emit(appwire.EventCollectionChanged, appwire.CollectionChanged{ID: id, Deleted: deleted})
}

func appStoreRef(r appwire.Ref) workspace.Ref {
	return workspace.Ref{Kind: workspace.Kind(r.Kind), ID: r.ID, SessionID: r.SessionID}
}

func appWireRef(r workspace.Ref) appwire.Ref {
	return appwire.Ref{Kind: string(r.Kind), ID: r.ID, SessionID: r.SessionID}
}

// appMoment is a stored moment on the wire: absent when unknown.
func appMoment(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	at := time.UnixMilli(ms).UTC()
	return &at
}

func appWireMember(m workspace.Member) appwire.Member {
	return appwire.Member{Ref: appWireRef(m.Ref), AddedAt: appMoment(m.AddedAt)}
}

func appWireCollection(s workspace.Summary) appwire.Collection {
	return appwire.Collection{ID: s.ID, Name: s.Name, CreatedAt: appMoment(s.CreatedAt), Count: s.Count, LastAddedAt: appMoment(s.LastAddedAt), Holds: s.Holds}
}

// describe is one collection as the wire says it.
func describeCollection(s *workspace.Store, id string) (appwire.Collection, error) {
	summary, err := s.Describe(context.Background(), id)
	if err != nil {
		return appwire.Collection{}, appCollectionError(err)
	}
	return appWireCollection(summary), nil
}

// ListCollections is one page of collections, oldest first, with the cursor
// of the next page when more remain and how many there are in all.
func (b *appBackend) ListCollections(holds *appwire.Ref, after string, limit int) (appwire.CollectionsResult, error) {
	s, err := b.collectionStore(false)
	if err != nil {
		return appwire.CollectionsResult{}, err
	}
	if s == nil {
		return appwire.CollectionsResult{Collections: []appwire.Collection{}}, nil
	}
	var ref *workspace.Ref
	if holds != nil {
		r := appStoreRef(*holds)
		ref = &r
	}
	page, err := s.ListPage(context.Background(), ref, after, limit)
	if err != nil {
		return appwire.CollectionsResult{}, appCollectionError(err)
	}
	out := appwire.CollectionsResult{Collections: make([]appwire.Collection, 0, len(page.Collections)), Next: page.Next, Total: page.Total}
	for _, c := range page.Collections {
		out.Collections = append(out.Collections, appWireCollection(c))
	}
	return out, nil
}

// CreateCollection makes a collection, the one call that may create the store.
func (b *appBackend) CreateCollection(name string) (appwire.Collection, error) {
	s, err := b.collectionStore(true)
	if err != nil {
		return appwire.Collection{}, err
	}
	c, err := s.Create(context.Background(), name)
	if err != nil {
		return appwire.Collection{}, appCollectionError(err)
	}
	b.collectionChanged(c.ID, false)
	return describeCollection(s, c.ID)
}

// RenameCollection renames one.
func (b *appBackend) RenameCollection(id, name string) (appwire.Collection, error) {
	s, err := b.existingStore()
	if err != nil {
		return appwire.Collection{}, err
	}
	if err := s.Rename(context.Background(), id, name); err != nil {
		return appwire.Collection{}, appCollectionError(err)
	}
	b.collectionChanged(id, false)
	return describeCollection(s, id)
}

// DuplicateCollection copies one under a new name.
func (b *appBackend) DuplicateCollection(id, name string) (appwire.Collection, error) {
	s, err := b.existingStore()
	if err != nil {
		return appwire.Collection{}, err
	}
	c, err := s.Duplicate(context.Background(), id, name)
	if err != nil {
		return appwire.Collection{}, appCollectionError(err)
	}
	b.collectionChanged(c.ID, false)
	return describeCollection(s, c.ID)
}

// DeleteCollection deletes one, and says so of every collection that lost a
// membership naming it.
func (b *appBackend) DeleteCollection(id string) error {
	s, err := b.existingStore()
	if err != nil {
		return err
	}
	parents, err := s.Delete(context.Background(), id)
	if err != nil {
		return appCollectionError(err)
	}
	b.collectionChanged(id, true)
	for _, parent := range parents {
		b.collectionChanged(parent, false)
	}
	return nil
}

// AddToCollection files references, all or nothing.
func (b *appBackend) AddToCollection(id string, refs []appwire.Member) (appwire.CollectionAddResult, error) {
	s, err := b.existingStore()
	if err != nil {
		return appwire.CollectionAddResult{}, err
	}
	additions := make([]workspace.Addition, 0, len(refs))
	for _, r := range refs {
		a := workspace.Addition{Ref: appStoreRef(r.Ref)}
		if r.AddedAt != nil {
			a.AddedAt = r.AddedAt.UnixMilli()
		}
		additions = append(additions, a)
	}
	added, already, err := s.AddRefs(context.Background(), id, additions)
	if err != nil {
		return appwire.CollectionAddResult{}, appCollectionError(err)
	}
	out := appwire.CollectionAddResult{Added: make([]appwire.Member, 0, len(added)), Already: make([]appwire.Ref, 0, len(already))}
	for _, m := range added {
		out.Added = append(out.Added, appWireMember(m))
	}
	for _, r := range already {
		out.Already = append(out.Already, appWireRef(r))
	}
	if len(added) > 0 {
		b.collectionChanged(id, false)
	}
	return out, nil
}

// RemoveFromCollection detaches references and answers what was there.
func (b *appBackend) RemoveFromCollection(id string, refs []appwire.Ref) (appwire.CollectionRemoveResult, error) {
	s, err := b.existingStore()
	if err != nil {
		return appwire.CollectionRemoveResult{}, err
	}
	want := make([]workspace.Ref, 0, len(refs))
	for _, r := range refs {
		want = append(want, appStoreRef(r))
	}
	removed, err := s.RemoveRefs(context.Background(), id, want)
	if err != nil {
		return appwire.CollectionRemoveResult{}, appCollectionError(err)
	}
	out := appwire.CollectionRemoveResult{Removed: make([]appwire.Member, 0, len(removed))}
	for _, m := range removed {
		out.Removed = append(out.Removed, appWireMember(m))
	}
	if len(removed) > 0 {
		b.collectionChanged(id, false)
	}
	return out, nil
}

// OpenCollection is one page of a collection's members with what each is
// now, continuing after the cursor a previous page gave.
func (b *appBackend) OpenCollection(id string, depth, limit int, after string) (appwire.CollectionOpenResult, error) {
	s, err := b.existingStore()
	if err != nil {
		return appwire.CollectionOpenResult{}, err
	}
	tree, err := s.Tree(context.Background(), id, depth, limit, after)
	if err != nil {
		return appwire.CollectionOpenResult{}, appCollectionError(err)
	}
	r := newCollectionResolver(b)
	var nodes func(level workspace.Level) []appwire.Node
	nodes = func(level workspace.Level) []appwire.Node {
		out := make([]appwire.Node, 0, len(level.Members))
		for _, n := range level.Members {
			node := appwire.Node{Member: appWireMember(n.Member)}
			if n.Kind == workspace.CollectionKind {
				node.Target = collectionTarget(n)
			} else {
				node.Target = r.target(n.Ref)
			}
			if n.Children != nil {
				node.Children = &appwire.NodeList{Members: nodes(*n.Children), More: n.Children.More}
			}
			out = append(out, node)
		}
		return out
	}
	return appwire.CollectionOpenResult{Collection: appWireCollection(tree.Collection), Members: nodes(tree.Level), More: tree.More, Next: tree.Next}, nil
}

// collectionTarget is a nested collection as the store found it.
func collectionTarget(n workspace.Node) appwire.Target {
	if !n.Found {
		return appwire.Target{State: appwire.TargetDeleted}
	}
	count := n.Count
	return appwire.Target{State: appwire.TargetLive, Title: n.Name, Count: &count}
}

// ExpandCollection is every live reference a collection holds through its
// nested collections, each once, outer levels first, at most limit.
//
// EVERY REFERENCE THE STORE ANSWERED IS ACCOUNTED FOR, past the limit too: a
// live one is answered or cut (more), a gone one is missing (a deleted nested
// collection with them), and one whose owner could not be asked is
// unavailable, the first of them named with the reason. An unavailable ref
// is never missing: nobody knows it is gone.
func (b *appBackend) ExpandCollection(id string, depth, limit int) (appwire.CollectionExpandResult, error) {
	s, err := b.existingStore()
	if err != nil {
		return appwire.CollectionExpandResult{}, err
	}
	e, err := s.Expand(context.Background(), id, depth)
	if err != nil {
		return appwire.CollectionExpandResult{}, appCollectionError(err)
	}
	out := appwire.CollectionExpandResult{
		Collection: appWireCollection(e.Collection), Refs: []appwire.ExpandedRef{}, UnavailableRefs: []appwire.ExpandedRef{},
		Missing: e.Deleted, Unread: e.Unread, Collections: e.Collections, More: e.More,
	}
	r := newCollectionResolver(b)
	for _, m := range e.Refs {
		target := r.target(m.Ref)
		switch target.State {
		case appwire.TargetDeleted, appwire.TargetMissing:
			out.Missing++
		case appwire.TargetUnknown:
			out.Unavailable++
			if len(out.UnavailableRefs) < appwire.CollectionUnavailableMax {
				out.UnavailableRefs = append(out.UnavailableRefs, appwire.ExpandedRef{Member: appWireMember(m), Target: target})
			}
		default:
			if len(out.Refs) == limit {
				out.More = true
				continue
			}
			out.Refs = append(out.Refs, appwire.ExpandedRef{Member: appWireMember(m), Target: target})
		}
	}
	return out, nil
}

// RelinkCollection puts to in from's place, keeping from's added_at and
// place, in one write. to is asked of its owner first and never written
// unless it is there now: a file must be a regular file at that path, any
// other kind must resolve live, and a collection is checked by the store as
// add checks one.
func (b *appBackend) RelinkCollection(id string, from, to appwire.Ref) (appwire.CollectionRelinkResult, error) {
	s, err := b.existingStore()
	if err != nil {
		return appwire.CollectionRelinkResult{}, err
	}
	if err := b.relinkable(to); err != nil {
		return appwire.CollectionRelinkResult{}, err
	}
	m, already, err := s.Relink(context.Background(), id, appStoreRef(from), appStoreRef(to))
	if err != nil {
		return appwire.CollectionRelinkResult{}, appCollectionError(err)
	}
	// Relinking a member to itself changed nothing, so it says nothing.
	if from != to {
		b.collectionChanged(id, false)
	}
	return appwire.CollectionRelinkResult{Member: appWireMember(m), Already: already}, nil
}

// relinkable is nil when to is there now, not_found when it is not, and the
// engine's failure, with the reason, when its owner could not be asked.
func (b *appBackend) relinkable(to appwire.Ref) error {
	switch to.Kind {
	case appwire.RefCollection:
		return nil
	case appwire.RefArtifact:
		info, err := os.Stat(to.ID)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return appwire.Errorf(appwire.CodeNotFound, "nothing is at that path now")
		}
		if err != nil {
			return appwire.Errorf(appwire.CodeEngine, "%s", appUnaskedReason("the file system", err))
		}
		if !info.Mode().IsRegular() {
			return appwire.Errorf(appwire.CodeNotFound, "nothing is at that path now but a folder or another thing that is not a file")
		}
		return nil
	}
	t := newCollectionResolver(b).target(appStoreRef(to))
	switch t.State {
	case appwire.TargetLive:
		return nil
	case appwire.TargetUnknown:
		return appwire.Errorf(appwire.CodeEngine, "%s", t.Reason)
	}
	return appwire.Errorf(appwire.CodeNotFound, "that %s is not there now", to.Kind)
}

// ── reasons ────────────────────────────────────────────────────────────────

// appUnknown is a target whose owner could not be asked, and why.
func appUnknown(owner string, err error) appwire.Target {
	return appwire.Target{State: appwire.TargetUnknown, Reason: appUnaskedReason(owner, err)}
}

// appReasonMax is the longest reason a target carries, in runes.
const appReasonMax = 200

// appPathWords finds a path inside a sentence: an absolute or home-relative
// path standing as its own word.
var appPathWords = regexp.MustCompile(`(^|[\s'"(=])~?/[^\s'"()]*`)

// appUnaskedReason is one short sentence: which owner could not be asked and
// why. A REASON CARRIES NO SECRET AND NO PATH: the operating system's words
// lose the path they name, what is left goes through the secret redactor,
// and any path still standing in it is said as "a path".
func appUnaskedReason(owner string, err error) string {
	why := "it did not answer"
	if err != nil {
		var wire *appwire.Error
		var pathErr *fs.PathError
		var linkErr *os.LinkError
		var sysErr *os.SyscallError
		switch {
		case errors.As(err, &wire):
			why = wire.Message
		case errors.As(err, &pathErr) && pathErr.Err != nil:
			why = pathErr.Err.Error()
		case errors.As(err, &linkErr) && linkErr.Err != nil:
			why = linkErr.Err.Error()
		case errors.As(err, &sysErr) && sysErr.Err != nil:
			why = sysErr.Err.Error()
		default:
			why = plainWords(err.Error())
		}
		why, _, _ = strings.Cut(why, "\n")
		why = appPathWords.ReplaceAllString(redact.Secrets(why), "${1}a path")
		why = strings.TrimSpace(why)
		if r := []rune(why); len(r) > appReasonMax {
			why = string(r[:appReasonMax]) + "…"
		}
		if why == "" {
			why = "it did not answer"
		}
	}
	return owner + " could not be asked: " + why
}

// ── resolution ─────────────────────────────────────────────────────────────

// collectionResolver answers what each member is now, asking each owner at
// most once for the whole call.
type collectionResolver struct {
	b *appBackend

	agentsRead bool
	agents     map[string]appwire.Agent
	agentsErr  error

	crewsRead bool
	crews     []appwire.Crew
	crewByID  map[string]int
	crewsErr  error

	tasks map[string]appOwned[appwire.Task]
	procs map[string]appOwned[appwire.Process]
	git   map[string]*appGitTree
}

// appOwned is one agent's tasks or processes by wire id, or why they could
// not be read.
type appOwned[T any] struct {
	byID map[string]T
	err  error
}

func newCollectionResolver(b *appBackend) *collectionResolver {
	return &collectionResolver{b: b, tasks: map[string]appOwned[appwire.Task]{}, procs: map[string]appOwned[appwire.Process]{}, git: map[string]*appGitTree{}}
}

func (r *collectionResolver) target(ref workspace.Ref) appwire.Target {
	switch ref.Kind {
	case workspace.ConversationKind:
		a, not := r.agent(ref.ID)
		if not != nil {
			return *not
		}
		return appwire.Target{State: appwire.TargetLive, Title: a.Title, Status: a.State, In: r.crewPathOf(a.ID)}
	case workspace.TaskKind:
		a, not := r.agent(ref.SessionID)
		if not != nil {
			return *not
		}
		n, err := strconv.ParseUint(ref.ID, 10, 64)
		if err != nil {
			return appwire.Target{State: appwire.TargetDeleted}
		}
		task, not := appFindOwned(r.tasks, a.ID, appTaskID(a.ID, n), r.b.ListTasks, "the task list of its conversation")
		if not != nil {
			return *not
		}
		return appwire.Target{State: appwire.TargetLive, Title: task.Title, Status: task.State, In: append(r.crewPathOf(a.ID), a.Title)}
	case workspace.ProcessKind:
		a, not := r.agent(ref.SessionID)
		if not != nil {
			return *not
		}
		p, not := appFindOwned(r.procs, a.ID, ref.ID, r.b.ListProcesses, "the process list of its conversation")
		if not != nil {
			return *not
		}
		title := p.Title
		if strings.TrimSpace(title) == "" {
			title = p.Command
		}
		return appwire.Target{State: appwire.TargetLive, Title: title, Status: p.State, In: append(r.crewPathOf(a.ID), a.Title)}
	case workspace.CrewKind:
		if !r.readCrews() {
			return appUnknown("the crew list", r.crewsErr)
		}
		i, ok := r.crewByID[ref.ID]
		if !ok {
			return appwire.Target{State: appwire.TargetDeleted}
		}
		c := r.crews[i]
		count := len(c.Members)
		return appwire.Target{State: appwire.TargetLive, Title: c.Name, Status: c.State, Count: &count, In: r.crewPath(c.Parent)}
	case workspace.ArtifactKind:
		return r.file(ref.ID)
	case workspace.CollectionKind:
		// A nested collection is found by the store in the same read that
		// found it a member; one reaching here was not, so it is gone.
		return appwire.Target{State: appwire.TargetDeleted}
	}
	// A standing item names nothing codeaf runs any more.
	return appwire.Target{State: appwire.TargetDeleted}
}

// agent is one agent off the one listing, or the target a member of it is
// when there is no such agent (deleted) or no listing (unknown, and why).
func (r *collectionResolver) agent(id string) (appwire.Agent, *appwire.Target) {
	if !r.agentsRead {
		r.agentsRead = true
		list, err := r.b.ListAgents("")
		r.agentsErr = err
		r.agents = make(map[string]appwire.Agent, len(list))
		for _, a := range list {
			r.agents[a.ID] = a
		}
	}
	if r.agentsErr != nil {
		t := appUnknown("the conversation list", r.agentsErr)
		return appwire.Agent{}, &t
	}
	a, ok := r.agents[id]
	if !ok {
		return appwire.Agent{}, &appwire.Target{State: appwire.TargetDeleted}
	}
	return a, nil
}

// appFindOwned is one task or process of an agent, read with list once per
// agent per call, or the target it is when it is not there; owner names the
// list in an unknown target's reason.
func appFindOwned[T any](cache map[string]appOwned[T], agent, id string, list func(string) ([]T, error), owner string) (T, *appwire.Target) {
	owned, ok := cache[agent]
	if !ok {
		items, err := list(agent)
		owned = appOwned[T]{byID: make(map[string]T, len(items)), err: err}
		for _, item := range items {
			owned.byID[appOwnedID(item)] = item
		}
		cache[agent] = owned
	}
	var zero T
	if owned.err != nil {
		var wire *appwire.Error
		if errors.As(owned.err, &wire) && wire.Code == appwire.CodeNotFound {
			return zero, &appwire.Target{State: appwire.TargetDeleted}
		}
		t := appUnknown(owner, owned.err)
		return zero, &t
	}
	item, ok := owned.byID[id]
	if !ok {
		return zero, &appwire.Target{State: appwire.TargetDeleted}
	}
	return item, nil
}

func appOwnedID(item any) string {
	switch v := item.(type) {
	case appwire.Task:
		return v.ID
	case appwire.Process:
		return v.ID
	}
	return ""
}

// readCrews takes the one crews listing, open and closed, and says whether
// there is one.
func (r *collectionResolver) readCrews() bool {
	if !r.crewsRead {
		r.crewsRead = true
		open, err := r.b.CrewList(false)
		var closed []appwire.Crew
		if err == nil {
			closed, err = r.b.CrewList(true)
		}
		r.crewsErr = err
		r.crews = append(open, closed...)
		r.crewByID = make(map[string]int, len(r.crews))
		for i, c := range r.crews {
			if _, dup := r.crewByID[c.ID]; !dup {
				r.crewByID[c.ID] = i
			}
		}
	}
	return r.crewsErr == nil
}

// crewPath is the names from the outermost crew down to id, inclusive; empty
// for none. The walk is bounded and never revisits a crew.
func (r *collectionResolver) crewPath(id string) []string {
	if id == "" || !r.readCrews() {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for id != "" && !seen[id] && len(names) < 32 {
		seen[id] = true
		i, ok := r.crewByID[id]
		if !ok {
			break
		}
		names = append([]string{r.crews[i].Name}, names...)
		id = r.crews[i].Parent
	}
	return names
}

// crewPathOf is the crew path of the first crew agent is a member of, open
// crews before closed ones; empty when it is in none.
func (r *collectionResolver) crewPathOf(agent string) []string {
	if !r.readCrews() {
		return nil
	}
	for _, c := range r.crews {
		for _, m := range c.Members {
			if m.Agent == agent {
				return r.crewPath(c.ID)
			}
		}
	}
	return nil
}

// file is a path as it is now: missing when nothing is there, and inside a
// git work tree its repository, branch and path within it. The file itself is
// never read and a link is never followed.
func (r *collectionResolver) file(path string) appwire.Target {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return appwire.Target{State: appwire.TargetMissing}
	}
	if err != nil {
		return appUnknown("the file system", err)
	}
	t := appwire.Target{State: appwire.TargetLive, Title: filepath.Base(path)}
	dir := filepath.Dir(path)
	if info.IsDir() {
		dir = path
	}
	if tree := r.gitTreeOf(dir); tree != nil {
		t.Repo, t.Branch = tree.repo, tree.branch
		if rel, err := filepath.Rel(tree.root, path); err == nil {
			t.Path = filepath.ToSlash(rel)
		}
	}
	return t
}

// appGitTree is a git work tree: its folder, the origin's owner/name (or the
// folder's name), and the branch checked out (empty when detached).
type appGitTree struct {
	root, repo, branch string
}

// appGitWalkMax is how many folders a look for .git climbs.
const appGitWalkMax = 64

// gitTreeOf is the work tree dir is inside, or nil. Every folder the walk
// passes is remembered with the answer, so the next file in the same tree is
// one map look.
func (r *collectionResolver) gitTreeOf(dir string) *appGitTree {
	var passed []string
	var found *appGitTree
	d := filepath.Clean(dir)
	for i := 0; i < appGitWalkMax; i++ {
		if tree, ok := r.git[d]; ok {
			found = tree
			break
		}
		passed = append(passed, d)
		dotgit := filepath.Join(d, ".git")
		if info, err := os.Lstat(dotgit); err == nil {
			found = appReadGitTree(d, dotgit, info)
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	for _, p := range passed {
		r.git[p] = found
	}
	return found
}

// appReadGitTree reads what a work tree says of itself: HEAD for the branch,
// the shared config for the origin. A .git file is a linked work tree's
// pointer to its own git folder, whose commondir names the shared one.
func appReadGitTree(root, dotgit string, info fs.FileInfo) *appGitTree {
	gitdir := dotgit
	if !info.IsDir() {
		line := strings.TrimSpace(appSmallFile(dotgit))
		pointed, ok := strings.CutPrefix(line, "gitdir:")
		if !ok {
			return nil
		}
		gitdir = strings.TrimSpace(pointed)
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(root, gitdir)
		}
	}
	tree := &appGitTree{root: root, repo: filepath.Base(root)}
	if ref, ok := strings.CutPrefix(strings.TrimSpace(appSmallFile(filepath.Join(gitdir, "HEAD"))), "ref:"); ok {
		tree.branch = strings.TrimPrefix(strings.TrimSpace(ref), "refs/heads/")
	}
	common := gitdir
	if c := strings.TrimSpace(appSmallFile(filepath.Join(gitdir, "commondir"))); c != "" {
		if !filepath.IsAbs(c) {
			c = filepath.Join(gitdir, c)
		}
		common = c
	}
	if repo := appOriginRepo(appSmallFile(filepath.Join(common, "config"))); repo != "" {
		tree.repo = repo
	}
	return tree
}

// appSmallFile is at most 64 KB of a file, and nothing when it cannot be read.
func appSmallFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, 64<<10))
	return string(data)
}

// appOriginRepo is the origin remote's owner/name out of a git config, or
// nothing when there is no origin or its address has no two parts.
func appOriginRepo(config string) string {
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(config))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			section = strings.Join(strings.Fields(line), " ")
			continue
		}
		if section != `[remote "origin"]` {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "url" {
			continue
		}
		return appOwnerName(strings.TrimSpace(value))
	}
	return ""
}

// appOwnerName is the last two parts of a remote address, without .git:
// git@github.com:owner/name.git and https://github.com/owner/name are both
// owner/name.
func appOwnerName(url string) string {
	url = strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	parts := strings.FieldsFunc(url, func(r rune) bool { return r == '/' || r == ':' || r == '\\' })
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}
