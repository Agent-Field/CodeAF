package main

// WORK.GRAPH: the app backend's door onto internal/workgraph.
//
// This file does the three things only the app backend can: it says which
// conversations a scope is made of (an agent is itself, a task is its owner, a
// crew is every member of it and of the crews under it), it says where each
// one's folder is, and it hands the generic builder what the session package
// read off those folders. It reads FILES ONLY — no live agent is asked, no
// session or backend lock is held across any read — so the graph of a crew of
// two hundred conversations costs the running work nothing and answers the same
// for a conversation that is closed.
//
// THE REVISION IS STAT CALLS. It names the stamps of every source the answer
// is built from (each conversation's checkpoint and plan stores, the project
// check indexes, the crews' file, and the conversations the scope's tasks wait
// on), so "nothing moved" is known without reading or building anything and a
// caller holding the revision is answered `unchanged` for the price of the
// stats. A moved conversation is parsed once; the snapshot is rebuilt once per
// revision no matter how many callers ask.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/workgraph"
)

// The bounds a scope is read within.
const (
	// graphMaxOwners is the most conversations one scope reads.
	graphMaxOwners = 2000
	// graphMaxExternal is the most other conversations a scope reads only to
	// say the state of a task its own tasks wait on.
	graphMaxExternal = 64
	// graphCacheEntries and graphCacheNodes bound the snapshots held.
	graphCacheEntries = 8
	graphCacheNodes   = 600_000
	// graphMaxUnread is how many unreadable conversations an answer names.
	graphMaxUnread = 16
)

// workGraphs is this process's graph state, ONE for the process: the engine's
// own frontier reads other conversations' tasks through it (v3OpenSession) and
// the app's work.graph reads through it, so the two see the same cached
// projections and a conversation's files are parsed once however many ask.
var workGraphs appWorkGraph

// appWorkGraph is the process's graph state; its zero value is usable.
type appWorkGraph struct {
	once   sync.Once
	source *session.WorkSource
	cache  *workgraph.Cache

	mu    sync.Mutex
	dirs  map[string]string
	crews crewMemo
}

// crewMemo is the crews' file as last read, by its stamp.
type crewMemo struct {
	stamp string
	file  *teamstore.File
}

func (g *appWorkGraph) init() {
	g.once.Do(func() {
		g.source = session.NewWorkSource()
		g.cache = workgraph.NewCache(graphCacheEntries, graphCacheNodes)
		g.dirs = map[string]string{}
	})
}

// graphOwner is one conversation a scope reads.
type graphOwner struct{ id, dir string }

// graphPlan is what a request resolves to before anything is read.
type graphPlan struct {
	scope   workgraph.GraphScope
	about   workgraph.ScopeFacts
	root    string
	owners  []graphOwner
	missing []workgraph.OwnerIssue
	crews   []workgraph.CrewInput
	members []workgraph.MemberInput
	crewSig string
}

var _ appwire.WorkGrapher = (*appBackend)(nil)

// WorkGraph answers work.graph.
func (b *appBackend) WorkGraph(ctx context.Context, req workgraph.GraphRequest) (*workgraph.GraphResult, error) {
	g := &workGraphs
	g.init()
	plan, err := b.graphPlan(req.Scope)
	if err != nil {
		return nil, err
	}
	loaded, err := g.load(ctx, plan)
	if err != nil {
		return nil, err
	}
	revision := loaded.revision(plan)
	if req.IfRevision != "" && req.IfRevision == revision {
		return unchangedGraph(req, revision), nil
	}
	snap, err := g.cache.Get(ctx, req.Scope, revision, func(ctx context.Context) (workgraph.Input, error) {
		return loaded.input(ctx, g, plan, revision)
	})
	if err != nil {
		return nil, err
	}
	return snap.Answer(ctx, req)
}

func unchangedGraph(req workgraph.GraphRequest, revision string) *workgraph.GraphResult {
	view := req.View
	if view == "" {
		view = workgraph.ViewNodes
	}
	return &workgraph.GraphResult{Contract: workgraph.Contract, View: view, Revision: revision, Unchanged: true, Scope: req.Scope}
}

// graphPlan resolves a scope into the conversations it reads.
func (b *appBackend) graphPlan(scope workgraph.GraphScope) (graphPlan, error) {
	plan := graphPlan{scope: scope}
	switch scope.Kind {
	case workgraph.ScopeCrew:
		return plan, b.crewPlan(&plan)
	case workgraph.ScopeTask:
		owner, _ := workgraph.ParseID(scope.ID)
		plan.root = scope.ID
		return plan, b.agentPlan(&plan, owner.Owner)
	}
	return plan, b.agentPlan(&plan, scope.ID)
}

// agentPlan is the plan of a scope that reads one conversation.
func (b *appBackend) agentPlan(plan *graphPlan, id string) error {
	a, err := b.find(id)
	if err != nil {
		return err
	}
	var view appwire.Agent
	var transcript string
	b.locked(func() { view, transcript = a.view, a.transcript })
	plan.owners = []graphOwner{{id: id, dir: filepath.Dir(transcript)}}
	if plan.scope.Kind == workgraph.ScopeAgent {
		plan.about = workgraph.ScopeFacts{Title: view.Title}
		if view.ClosedAt != nil || view.State == "closed" || view.State == "archived" {
			plan.about.State, plan.about.ClosedAt, plan.about.ReadOnly = "closed", view.ClosedAt, true
		} else {
			plan.about.State = "open"
		}
	}
	return nil
}

// crewPlan is the plan of a crew scope: the crew, every crew under it, and
// every conversation that is a member of any of them.
func (b *appBackend) crewPlan(plan *graphPlan) error {
	file, sig, err := workGraphs.crewFile(b.crewProfile())
	if err != nil {
		return err
	}
	root, ok := file.Team(plan.scope.ID)
	if !ok {
		return appwire.Errorf(appwire.CodeNotFound, "there is no crew %s", plan.scope.ID)
	}
	plan.crewSig = sig
	teams := append([]teamstore.Team{root}, file.Descendants(root.ID)...)
	plan.about = workgraph.ScopeFacts{Title: root.Name, State: crewStateWord(root), ReadOnly: root.Closed()}
	if root.Closed() {
		plan.about.ClosedAt = stampPtr(root.ClosedAt)
	}
	plan.crews, plan.members = crewFacts(file, teams)
	seen := map[string]bool{}
	for _, m := range plan.members {
		if seen[m.Agent] {
			continue
		}
		seen[m.Agent] = true
		if len(plan.owners) >= graphMaxOwners {
			break
		}
		if dir, ok := workGraphs.locate(m.Agent); ok {
			plan.owners = append(plan.owners, graphOwner{id: m.Agent, dir: dir})
		} else {
			plan.missing = append(plan.missing, workgraph.OwnerIssue{Owner: m.Agent, Reason: session.WorkIssueMissing})
		}
	}
	sort.Slice(plan.owners, func(i, j int) bool { return plan.owners[i].id < plan.owners[j].id })
	return nil
}

func crewStateWord(t teamstore.Team) string {
	if t.Closed() {
		return "closed"
	}
	return "open"
}

func stampPtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// locked runs fn holding g.mu; the unlock is deferred so an absorbed panic never
// leaves it held (internal/guard's lock law).
func (g *appWorkGraph) locked(fn func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fn()
}

// crewFile is the crews' file, parsed once per change of the file.
func (g *appWorkGraph) crewFile(profile string) (*teamstore.File, string, error) {
	g.init()
	sig := session.FileStamp(teamstore.Path(profile))
	var held *teamstore.File
	g.locked(func() {
		if g.crews.file != nil && g.crews.stamp == sig {
			held = g.crews.file
		}
	})
	if held != nil {
		return held, sig, nil
	}
	file, err := teamstore.Load(profile)
	if err != nil {
		return nil, "", err
	}
	g.locked(func() { g.crews = crewMemo{stamp: sig, file: file} })
	return file, sig, nil
}

// crewFacts turns teams into the generic crews and memberships. A member that
// runs a crew under another is that crew's lead; the manager a member reports
// to is the one its home membership names.
func crewFacts(file *teamstore.File, teams []teamstore.Team) ([]workgraph.CrewInput, []workgraph.MemberInput) {
	leads := map[string]string{}
	for _, t := range teams {
		if t.Parent != "" && t.Manager != "" && !t.Closed() {
			leads[t.Parent+"\x00"+t.Manager] = t.ID
		}
	}
	var crews []workgraph.CrewInput
	var members []workgraph.MemberInput
	for _, t := range teams {
		crews = append(crews, workgraph.CrewInput{CrewFact: crewFact(t)})
		for _, m := range t.Members {
			members = append(members, memberFact(file, t, m, leads[t.ID+"\x00"+m.Key]))
		}
	}
	return crews, members
}

func crewFact(t teamstore.Team) workgraph.CrewFact {
	f := workgraph.CrewFact{ID: t.ID, Name: t.Name, Parent: t.Parent, State: crewStateWord(t)}
	hue := t.Hue
	f.Hue = &hue
	if t.Closed() {
		f.ClosedAt = stampPtr(t.ClosedAt)
	}
	for _, m := range t.Members {
		if m.Key == t.Manager {
			f.Manager = appMemberAgent(m)
		}
	}
	return f
}

func memberFact(file *teamstore.File, t teamstore.Team, m teamstore.Member, leads string) workgraph.MemberInput {
	f := workgraph.MemberFact{
		Agent: appMemberAgent(m), Handle: m.Handle, Title: m.Word, Crew: t.ID,
		Manager: m.Key == t.Manager, Leads: leads, JoinedAt: stampPtr(m.JoinedAt),
	}
	if home, ok := file.Home(m.Key); ok {
		if team, ok := file.Team(home.Team); ok {
			if boss, ok := team.Member(home.Manager); ok {
				f.ReportsTo = appMemberAgent(boss)
			}
		}
	}
	return workgraph.MemberInput{MemberFact: f, Home: m.Home}
}

// locate is the folder of conversation id: one directory listing of the places
// root and one stat per project, remembered, and checked again by the stat of
// every read made from it.
func (g *appWorkGraph) locate(id string) (string, bool) {
	if !workgraph.ValidOwner(id) {
		return "", false
	}
	g.init()
	var dir string
	var known bool
	g.locked(func() { dir, known = g.dirs[id] })
	if known {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, true
		}
	}
	found, ok := findConversation(id)
	if ok {
		g.locked(func() { g.dirs[id] = found })
	}
	return found, ok
}

// findConversation lists the places root once and stats the id under each
// project.
func findConversation(id string) (string, bool) {
	root := session.PlacesRoot()
	buckets, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		candidate := filepath.Join(root, bucket.Name(), id)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// externalTasks is the reader the app's conversations wait on other
// conversations' tasks through. It answers from the same cached projections the
// graph reads, so the engine's frontier and the graph can never disagree.
func (g *appWorkGraph) externalTasks() session.ExternalTasks {
	g.init()
	return g.source.ExternalReader(g.locate)
}

// graphLoaded is every conversation of a plan, read.
type graphLoaded struct {
	owners   []*session.OwnerFacts
	unread   []workgraph.OwnerIssue
	external []externalOwner
	indexes  []string
}

// externalOwner is a conversation read only to say the state of a target.
type externalOwner struct {
	id    string
	facts *session.OwnerFacts
	stamp string
}

// load reads (or takes from memory) each conversation of the plan, then the
// other conversations its tasks wait on. A cancelled ctx ends it.
func (g *appWorkGraph) load(ctx context.Context, plan graphPlan) (*graphLoaded, error) {
	out := &graphLoaded{unread: append([]workgraph.OwnerIssue(nil), plan.missing...)}
	inScope := map[string]bool{}
	indexes := map[string]bool{}
	for _, o := range plan.owners {
		inScope[o.id] = true
		facts, err := g.source.Owner(ctx, o.id, o.dir)
		switch {
		case err == session.ErrWorkOwnerMissing:
			out.unread = append(out.unread, workgraph.OwnerIssue{Owner: o.id, Reason: session.WorkIssueMissing})
			continue
		case err != nil:
			return nil, err
		case facts.Issue != "":
			out.unread = append(out.unread, workgraph.OwnerIssue{Owner: o.id, Reason: facts.Issue})
		}
		out.owners = append(out.owners, facts)
		indexes[session.TaskIndexPath(session.Place{Dir: o.dir}.Transcript())] = true
	}
	for path := range indexes {
		out.indexes = append(out.indexes, path)
	}
	sort.Strings(out.indexes)
	return out, g.loadExternal(ctx, out, inScope)
}

// loadExternal reads the conversations the scope's tasks wait on that the scope
// does not hold, at most [graphMaxExternal] of them.
func (g *appWorkGraph) loadExternal(ctx context.Context, out *graphLoaded, inScope map[string]bool) error {
	wanted := map[string]bool{}
	for _, o := range out.owners {
		for _, id := range o.Externals {
			if !inScope[id] {
				wanted[id] = true
			}
		}
	}
	ids := make([]string, 0, len(wanted))
	for id := range wanted {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > graphMaxExternal {
		ids = ids[:graphMaxExternal]
	}
	for _, id := range ids {
		dir, ok := g.locate(id)
		if !ok {
			continue
		}
		facts, err := g.source.Owner(ctx, id, dir)
		if err == session.ErrWorkOwnerMissing {
			continue
		}
		if err != nil {
			return err
		}
		out.external = append(out.external, externalOwner{id: id, facts: facts, stamp: facts.Stamp})
	}
	return nil
}

// revision names every source the answer is built from.
func (l *graphLoaded) revision(plan graphPlan) string {
	h := sha256.New()
	put := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	put("work.graph/1", string(plan.scope.Kind), plan.scope.ID, plan.root, plan.crewSig)
	for _, o := range l.owners {
		put("o", o.Owner, o.Stamp)
	}
	for _, o := range l.external {
		put("x", o.id, o.stamp)
	}
	for _, path := range l.indexes {
		put("i", path, session.FileStamp(path))
	}
	for _, u := range l.unread {
		put("u", u.Owner, u.Reason)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// input is the generic builder's input for a loaded plan. It runs inside the
// cache's build, with no lock held, and honours ctx.
func (l *graphLoaded) input(ctx context.Context, g *appWorkGraph, plan graphPlan, revision string) (workgraph.Input, error) {
	in := workgraph.Input{
		Scope: plan.scope, About: plan.about, Root: plan.root, Crews: plan.crews, Members: plan.members,
		Revision: revision, AsOf: time.Now(), Owners: len(l.owners) + len(l.unread), Excluded: session.WorkExcluded,
	}
	if len(l.unread) > graphMaxUnread {
		in.Unread = l.unread[:graphMaxUnread]
	} else {
		in.Unread = l.unread
	}
	checks := map[string]*session.CheckIndex{}
	for _, path := range l.indexes {
		index, err := g.source.Checks(ctx, path)
		if err != nil {
			return in, err
		}
		checks[path] = index
	}
	total := 0
	for _, o := range l.owners {
		total += len(o.Facts)
	}
	in.Facts = make([]workgraph.Fact, 0, min(total, workgraph.MaxScopeNodes))
	for _, o := range l.owners {
		for _, f := range o.Facts {
			if len(in.Facts) == workgraph.MaxScopeNodes {
				in.Truncated = true
				break
			}
			in.Facts = append(in.Facts, f)
		}
	}
	l.overlayChecks(&in, plan, checks)
	in.Resolve = l.resolver()
	return in, nil
}

// overlayChecks gives a graph node the recorded check runs its project's index
// holds for it. A plan row carries its own and is left alone.
func (l *graphLoaded) overlayChecks(in *workgraph.Input, plan graphPlan, checks map[string]*session.CheckIndex) {
	dirs := map[string]string{}
	for _, o := range plan.owners {
		dirs[o.id] = session.TaskIndexPath(session.Place{Dir: o.dir}.Transcript())
	}
	for i := range in.Facts {
		n := &in.Facts[i].Node
		if n.Checks != nil || n.Kind == "plan" {
			continue
		}
		if c := checks[dirs[n.Ref.Owner]].Of(n.Ref.Owner, n.Ref.Task); c != nil {
			n.Checks = c
		}
	}
}

// resolver answers about a target outside the scope from the conversations
// already read: found with its state, absent when the conversation was read and
// lacks the task, unknown when it was not read or could not be.
func (l *graphLoaded) resolver() func(workgraph.TaskRef) workgraph.Resolution {
	by := make(map[string]*session.OwnerFacts, len(l.external))
	for _, o := range l.external {
		by[o.id] = o.facts
	}
	return func(r workgraph.TaskRef) workgraph.Resolution {
		facts, ok := by[r.Owner]
		if !ok {
			return workgraph.Resolution{Standing: workgraph.StandingUnknown}
		}
		fact, ok := facts.Find(r.Task)
		switch {
		case ok:
			return workgraph.Resolution{Standing: workgraph.StandingFound, State: fact.Node.State}
		case facts.Issue != "":
			return workgraph.Resolution{Standing: workgraph.StandingUnknown}
		}
		return workgraph.Resolution{Standing: workgraph.StandingAbsent}
	}
}
