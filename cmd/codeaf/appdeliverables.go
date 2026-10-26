package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The engine half of deliverables.* and branches.list. Everything is read from
// the artifacts index and the session's own folder; nothing takes a path from
// the caller except Keep's destination folder and a snapshot's saved picture,
// and both are checked in internal/session.

const (
	appDeliverListTime = 2 * time.Second
	appDeliverGetTime  = 8 * time.Second
	appKeepTime        = 30 * time.Minute
	// appDeliverReadTime bounds one deliverables.read: deciding the source and
	// reading at most its page.
	appDeliverReadTime = 10 * time.Second
	// appBranchListTime bounds one branches.list page.
	appBranchListTime = 3 * time.Second
)

// appDeliverAgent is what one conversation contributes to deliverable checks:
// its roots and whether the engine knows it at all.
type appDeliverAgent struct {
	roots     []string
	folders   []string
	workspace string
	place     session.Place
	known     bool
}

// appDeliverEnv memoizes the per-conversation lookups of one call.
type appDeliverEnv struct {
	b      *appBackend
	agents map[string]*appDeliverAgent
	// ctx bounds every source decision this request makes (a listing's own
	// deadline reaches each row through it), and cache is what git said,
	// shared by every row of the request.
	ctx   context.Context
	cache *session.DeliverSourceCache
}

func (b *appBackend) deliverEnv() *appDeliverEnv {
	return b.deliverEnvWithin(context.Background())
}

// deliverEnvWithin is one request's environment, bounded by ctx.
func (b *appBackend) deliverEnvWithin(ctx context.Context) *appDeliverEnv {
	env := &appDeliverEnv{b: b, agents: map[string]*appDeliverAgent{}, ctx: ctx, cache: session.NewDeliverSourceCache()}
	appDeliverEnvMade(env)
	return env
}

// appDeliverEnvMade is a test's view of each request's environment.
var appDeliverEnvMade = func(*appDeliverEnv) {}

func (e *appDeliverEnv) agent(id string) *appDeliverAgent {
	if got := e.agents[id]; got != nil {
		return got
	}
	out := &appDeliverAgent{}
	if a, err := e.b.find(id); err == nil {
		var workspace string
		e.b.locked(func() { workspace = a.view.Workspace })
		place := session.Place{Dir: session.DirOf(a.transcript), Owned: true}
		out = &appDeliverAgent{
			roots:     []string{workspace, place.Work(), place.Artifacts()},
			folders:   []string{place.Work(), place.Artifacts()},
			workspace: workspace, place: place, known: true,
		}
	}
	e.agents[id] = out
	return out
}

var appLiveWords = map[string]string{
	session.MediaAvailable:      "available",
	session.MediaChanged:        "changed",
	session.MediaMissing:        "gone",
	session.MediaOutside:        "outside",
	session.MediaUnreadable:     "unreadable",
	session.DeliverLiveOnBranch: "on_branch",
}

// wire is one deliverable as the wire carries it.
func (e *appDeliverEnv) wire(row session.DeliverableRow) appwire.Deliverable {
	out, _ := e.wireOf(row)
	return out
}

// wireOf is wire, saying too whether git could not decide the row's source in
// time (it is then described as unreadable, never gone).
func (e *appDeliverEnv) wireOf(row session.DeliverableRow) (appwire.Deliverable, bool) {
	agent := e.agent(row.Artifact.Session)
	source := session.ResolveDeliverableSourceWith(e.ctx, e.cache, row, agent.roots, agent.workspace)
	live := source.Live
	fact := row.Fact
	out := appwire.Deliverable{
		ID: row.ID, Agent: row.Artifact.Session, Kind: row.Kind, Title: row.Artifact.Title, Path: row.Artifact.Path,
		Created: row.Artifact.Created.UTC().Format(time.RFC3339Nano), Live: appLiveWords[live.State], Origin: "unknown",
		State: fact.State, Turn: fact.Turn, Run: fact.Run, Call: fact.Call, Task: fact.Task, From: fact.From, Profile: fact.Profile,
		Authored: fact.Authored, Version: session.DeliverableVersion(row), LiveVersion: live.Version,
	}
	if row.Recorded {
		if fact.Origin != "" {
			out.Origin = fact.Origin
		}
		if fact.Update {
			out.Updated = fact.At.UTC().Format(time.RFC3339Nano)
		}
	}
	// A legacy generated-media row records its run: that is a recorded fact and
	// feeds Run only.
	if !row.Recorded && row.Artifact.Media != nil {
		out.Run = row.Artifact.Media.Run
		out.From = append([]string(nil), row.Artifact.Media.References...)
		if row.Artifact.Media.Message != "" {
			out.Turn = row.Artifact.Media.Message
		}
	}
	if row.Recorded && out.Turn == "" && row.Artifact.Media != nil {
		out.Turn = row.Artifact.Media.Message
	}
	if out.Live == "" {
		out.Live = "unreadable"
	}
	if row.Recorded && fact.Source != nil {
		out.Original = fact.Source.Canonical
	}
	if fact.Branch != "" && fact.Rel != "" {
		out.Branch, out.Rel = fact.Branch, fact.Rel
	}
	canonical := row.Artifact.Path
	if live.Path != "" {
		canonical = live.Path
		out.Open, out.OpenFrom = live.Path, "source"
		if live.Copy {
			out.OpenFrom = "kept"
		}
	}
	switch {
	case withinAnyApp(agent.folders, canonical):
		out.Place = "agent_folder"
	case withinAnyApp([]string{agent.workspace}, canonical):
		out.Place = "repo"
	default:
		out.Place = "elsewhere"
	}
	switch {
	case fact.File != nil && fact.File.Bytes > 0:
		n := fact.File.Bytes
		out.Bytes = &n
		out.MIME = fact.File.MIME
	case row.Artifact.Media != nil && row.Artifact.Media.Bytes > 0:
		n := row.Artifact.Media.Bytes
		out.Bytes = &n
	}
	if out.MIME == "" && row.Artifact.Media != nil {
		out.MIME = row.Artifact.Media.MIME
	}
	if live.HasBytes {
		n := live.Bytes
		out.LiveBytes = &n
	}
	switch row.Kind {
	case "image", "audio", "video":
		if live.Copy {
			// The original is gone and the media source names it: offering that
			// id as a preview would hand out a dead one.
			out.PreviewUnavailable = "source_gone"
			break
		}
		// State updates restate a source; writes can publish a fresh source
		// while retaining the deliverable's identity.
		bare := row.Artifact
		bare.Deliver = nil
		if media, ok := session.MediaSourceOf(bare); ok {
			out.Media = media
		}
	}
	if fact.Kept != nil {
		kept := &appwire.DeliverableKept{Path: fact.Kept.Path, At: fact.Kept.At.UTC().Format(time.RFC3339Nano), SHA256: fact.Kept.SHA256, How: fact.Kept.How}
		if fact.Kept.Bytes > 0 {
			n := fact.Kept.Bytes
			kept.Bytes = &n
		}
		out.Kept = kept
	}
	return out, source.Undecided
}

// describe is a listing's row, or false when the request's time ran out
// before the row was decided: the page then ends before this row and its
// cursor resumes at it, so no row is ever left out or said to be gone on an
// answer nobody had. A row git could not decide for another reason (a
// workspace that is no repository now) is described, unreadable; stopping on
// it would ask the same question for ever.
func (e *appDeliverEnv) describe(row session.DeliverableRow) (appwire.Deliverable, bool) {
	if e.ctx.Err() != nil {
		return appwire.Deliverable{}, false
	}
	d, undecided := e.wireOf(row)
	if undecided && e.ctx.Err() != nil {
		return appwire.Deliverable{}, false
	}
	return d, true
}

func withinAnyApp(roots []string, path string) bool {
	return session.WithinAny(roots, path)
}

// scope resolves the conversations a query covers; nil is every conversation.
func (b *appBackend) deliverScope(q appwire.DeliverablesQuery) (map[string]bool, error) {
	switch q.Scope {
	case "agent":
		if _, err := b.find(q.Agent); err != nil {
			return nil, err
		}
		return map[string]bool{q.Agent: true}, nil
	case "crew":
		for _, closed := range []bool{false, true} {
			crews, err := b.CrewList(closed)
			if err != nil {
				return nil, err
			}
			for _, crew := range crews {
				if crew.ID != q.Crew {
					continue
				}
				members := map[string]bool{}
				for _, member := range crew.Members {
					members[member.Agent] = true
				}
				if q.Member != "" {
					if !members[q.Member] {
						return nil, appwire.Errorf(appwire.CodeNotFound, "%s is not a member of this crew", q.Member)
					}
					return map[string]bool{q.Member: true}, nil
				}
				return members, nil
			}
		}
		return nil, appwire.Errorf(appwire.CodeNotFound, "there is no crew %s", q.Crew)
	case appwire.ScopeTeams:
		// The teams union is resolved against the request's own deadline
		// (listTeamsUnion); falling through here would read as every conversation.
		return nil, appwire.Errorf(appwire.CodeEngine, "the teams scope is not resolved here")
	}
	return nil, nil
}

func appDeliverSet(words []string) map[string]bool {
	if len(words) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, word := range words {
		out[strings.ToLower(strings.TrimSpace(word))] = true
	}
	return out
}

// deliverQuery is the wire query as the session reader takes it, and the live
// filter the wire shape needs applied after the reader's own.
func (b *appBackend) deliverQuery(q appwire.DeliverablesQuery, count bool) (session.DeliverableQuery, func(appwire.Deliverable) bool, error) {
	sessions, err := b.deliverScope(q)
	if err != nil {
		return session.DeliverableQuery{}, nil, err
	}
	if sessions != nil && len(sessions) == 0 {
		// A crew with no members has no deliverables; an empty set must not read as "all".
		sessions = map[string]bool{"-": true}
	}
	query := session.DeliverableQuery{Sessions: sessions, Kinds: appDeliverSet(q.Kind), States: appDeliverSet(q.State), Text: q.Query, CountOnly: count}
	lives := appDeliverSet(q.Live)
	return query, func(d appwire.Deliverable) bool { return lives == nil || lives[d.Live] }, nil
}

// ListDeliverables answers deliverables.list.
func (b *appBackend) ListDeliverables(q appwire.DeliverablesQuery) (appwire.DeliverablesPage, error) {
	if q.Scope == appwire.ScopeTeams {
		return b.listTeamsUnion(q)
	}
	query, liveOK, err := b.deliverQuery(q, false)
	if err != nil {
		return appwire.DeliverablesPage{}, err
	}
	deadline := time.Now().Add(appDeliverListTime)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	return b.deliverPage(ctx, deadline, query, liveOK, q.Cursor, q.Limit, nil)
}

// deliverPage is one page of query within ctx and deadline. decorate, when set,
// adds what a scope knows about each row's owner.
func (b *appBackend) deliverPage(ctx context.Context, deadline time.Time, query session.DeliverableQuery, liveOK func(appwire.Deliverable) bool, cursor string, limit int, decorate func(*appwire.Deliverable)) (appwire.DeliverablesPage, error) {
	env := b.deliverEnvWithin(ctx)
	type owned struct{ agent, id string }
	built := map[owned]appwire.Deliverable{}
	scan, err := session.ListDeliverables(appMediaIndex(), query, cursor, limit, deadline,
		appwire.MediaReplyBudget-8192, func(row session.DeliverableRow) (int, bool, error) {
			d, ok := env.describe(row)
			if !ok {
				return 0, false, session.ErrDeliverPageEnd
			}
			if !liveOK(d) {
				return 0, false, nil
			}
			if decorate != nil {
				decorate(&d)
			}
			raw, err := json.Marshal(d)
			if err != nil {
				return 0, false, err
			}
			built[owned{row.Artifact.Session, row.ID}] = d
			return len(raw) + 1, true, nil
		})
	if err != nil {
		return appwire.DeliverablesPage{}, appDeliverError(err)
	}
	page := appwire.DeliverablesPage{Deliverables: make([]appwire.Deliverable, 0, len(scan.Rows)), Next: scan.Next, Incomplete: scan.Incomplete}
	for _, row := range scan.Rows {
		page.Deliverables = append(page.Deliverables, built[owned{row.Artifact.Session, row.ID}])
	}
	return page, nil
}

// CountDeliverables answers deliverables.count.
func (b *appBackend) CountDeliverables(q appwire.DeliverablesQuery) (appwire.DeliverablesCount, error) {
	if q.Scope == appwire.ScopeTeams {
		return b.countTeamsUnion(q)
	}
	query, liveOK, err := b.deliverQuery(q, true)
	if err != nil {
		return appwire.DeliverablesCount{}, err
	}
	deadline := time.Now().Add(appDeliverListTime)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	return b.deliverCount(ctx, deadline, query, liveOK, q.Cursor)
}

func (b *appBackend) deliverCount(ctx context.Context, deadline time.Time, query session.DeliverableQuery, liveOK func(appwire.Deliverable) bool, cursor string) (appwire.DeliverablesCount, error) {
	env := b.deliverEnvWithin(ctx)
	scan, err := session.ListDeliverables(appMediaIndex(), query, cursor, 0, deadline, 0,
		func(row session.DeliverableRow) (int, bool, error) {
			d, ok := env.describe(row)
			if !ok {
				return 0, false, session.ErrDeliverPageEnd
			}
			return 0, liveOK(d), nil
		})
	if err != nil {
		return appwire.DeliverablesCount{}, appDeliverError(err)
	}
	return appwire.DeliverablesCount{Count: scan.Count, Next: scan.Next, Incomplete: scan.Incomplete}, nil
}

func (b *appBackend) deliverRow(agent, id string) (session.DeliverableRow, error) {
	// The index scopes a deliverable by its session, so a conversation this
	// machine can no longer locate still answers for what it recorded (a global
	// list shows it, and get must agree); acting on its file is refused later,
	// because no root vouches for it.
	row, err := session.FindDeliverable(appMediaIndex(), agent, id, time.Now().Add(appDeliverGetTime))
	if err != nil {
		return session.DeliverableRow{}, appDeliverError(err)
	}
	if row.Fact.Removed {
		return session.DeliverableRow{}, appwire.Errorf(appwire.CodeNotFound, "there is no such deliverable in this conversation")
	}
	return row, nil
}

// GetDeliverable answers deliverables.get.
func (b *appBackend) GetDeliverable(agent, id string) (appwire.Deliverable, error) {
	row, err := b.deliverRow(agent, id)
	if err != nil {
		return appwire.Deliverable{}, err
	}
	return b.deliverEnv().wire(row), nil
}

// ReadDeliverable answers deliverables.read: the text of a document the engine
// recorded, bounded, and only at the version the caller names when it names one.
func (b *appBackend) ReadDeliverable(agent, id, version string, max int) (appwire.DeliverableText, error) {
	row, err := b.deliverRow(agent, id)
	if err != nil {
		return appwire.DeliverableText{}, err
	}
	switch row.Kind {
	case session.DeliverableMarkdown, session.DeliverableMermaid, session.DeliverableExport:
	default:
		return appwire.DeliverableText{}, appwire.Errorf(appwire.CodeBadParams, "only a document or diagram is read as text; open a %s through its media source", row.Kind)
	}
	env := b.deliverEnv()
	known := env.agent(agent)
	ctx, cancel := context.WithTimeout(context.Background(), appDeliverReadTime)
	defer cancel()
	source := session.ResolveDeliverableSource(ctx, row, known.roots, known.workspace)
	if source.Undecided {
		return appwire.DeliverableText{}, appwire.Errorf(appwire.CodeBusy, "the file source could not be checked in time; try again")
	}
	live := source.Live
	if source.OnBranch() {
		return readBranchDeliverable(ctx, source, version, max)
	}
	if live.Path == "" || (live.State != session.MediaAvailable && live.State != session.MediaChanged) {
		return appwire.DeliverableText{}, appwire.Errorf(appwire.CodeNotFound, "the file is not there to read (%s)", appLiveWords[live.State])
	}
	file, err := os.Open(live.Path)
	if err != nil {
		return appwire.DeliverableText{}, appwire.Errorf(appwire.CodeNotFound, "the file is not there to read")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return appwire.DeliverableText{}, appwire.Errorf(appwire.CodeNotFound, "the file is not there to read")
	}
	// The descriptor must be the file that was checked, not a link or a
	// replacement put there since: compared by identity and by version.
	if named, lerr := os.Lstat(live.Path); lerr != nil || !os.SameFile(named, info) {
		return appwire.DeliverableText{}, &appwire.Error{Code: appwire.CodeConflict, Message: "the file changed since it was checked", Detail: appwire.ConflictDetail{Reason: "changed"}}
	}
	ctime, dev, ino := session.FileIdentity(info)
	at := session.FileVersion(info.Size(), info.ModTime().UnixNano(), ctime, dev, ino)
	if at != live.Version || (version != "" && version != at) {
		return appwire.DeliverableText{}, &appwire.Error{Code: appwire.CodeConflict, Message: "the file changed since you looked", Detail: appwire.ConflictDetail{Reason: "changed"}}
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(max)+1))
	if err != nil {
		return appwire.DeliverableText{}, appEngineError(err)
	}
	truncated := len(data) > max
	// A whole file whose digest the source knows must BE those bytes; a part
	// is held to the pinned version checked above.
	if sum := sha256.Sum256(data); !truncated && live.State == session.MediaAvailable && source.Expect != "" && hex.EncodeToString(sum[:]) != source.Expect {
		return appwire.DeliverableText{}, &appwire.Error{Code: appwire.CodeConflict, Message: "the file changed since it was checked", Detail: appwire.ConflictDetail{Reason: "changed"}}
	}
	if truncated {
		data = data[:max]
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	if !utf8.Valid(data) {
		return appwire.DeliverableText{}, appwire.Errorf(appwire.CodeBadParams, "that file is not text")
	}
	return appwire.DeliverableText{Text: string(data), Bytes: int64(len(data)), Truncated: truncated, Version: at}, nil
}

func (b *appBackend) deliverEvent(agent string, id, reason string) {
	a, err := b.find(agent)
	if err != nil {
		return
	}
	if live := b.liveOf(a); live != nil {
		live.box.Event(appwire.EventDeliverableChanged, appwire.DeliverableChanged{Agent: agent, ID: id, Reason: reason})
	}
}

// KeepDeliverable answers deliverables.keep; see session.KeepDeliverable for
// every guarantee. dir is a folder the native host's picker returned.
func (b *appBackend) KeepDeliverable(agent, id, dir, name, expect string) (appwire.Deliverable, error) {
	row, err := b.deliverRow(agent, id)
	if err != nil {
		return appwire.Deliverable{}, err
	}
	env := b.deliverEnv()
	ctx, cancel := context.WithTimeout(context.Background(), appKeepTime)
	defer cancel()
	updated, _, err := session.KeepDeliverable(ctx, appMediaIndex(), row, env.agent(agent).roots, home.Join("v3"),
		session.KeepRequest{Dir: dir, Name: name, ExpectVersion: expect, Workspace: env.agent(agent).workspace})
	if err != nil {
		return appwire.Deliverable{}, appKeepError(err)
	}
	b.deliverEvent(agent, updated.ID, "kept")
	return env.wire(updated), nil
}

func appKeepError(err error) error {
	var exists *session.KeepExistsError
	switch {
	case errors.As(err, &exists):
		return &appwire.Error{Code: appwire.CodeConflict, Message: "that name is already taken there; choose another", Detail: appwire.ConflictDetail{Reason: "exists", Path: exists.Path}}
	case errors.Is(err, session.ErrKeepAlready):
		return &appwire.Error{Code: appwire.CodeConflict, Message: err.Error(), Detail: appwire.ConflictDetail{Reason: "already_kept"}}
	case errors.Is(err, session.ErrKeepChanged):
		return &appwire.Error{Code: appwire.CodeConflict, Message: err.Error(), Detail: appwire.ConflictDetail{Reason: "changed"}}
	case errors.Is(err, session.ErrKeepSource):
		return appwire.Errorf(appwire.CodeNotFound, "%s", err.Error())
	case errors.Is(err, session.ErrKeepDest), errors.Is(err, session.ErrKeepName):
		return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	case errors.Is(err, session.ErrKeepTooLarge):
		return appwire.Errorf(appwire.CodeTooLarge, "%s", err.Error())
	case errors.Is(err, session.ErrKeepUnconfirmed):
		// A fixed sentence and no detail: the outcome is unknown, and the
		// engine names no path it cannot vouch for.
		return appwire.Errorf(appwire.CodeUncertain, "the copy may have been kept; look at the folder and this file again before keeping it again")
	case errors.Is(err, session.ErrMediaBusy):
		return appwire.Errorf(appwire.CodeBusy, "the file could not be checked or kept yet; try again")
	}
	return appDeliverError(err)
}

// RemoveDeliverable answers deliverables.remove. It appends a row; the file is
// never touched.
func (b *appBackend) RemoveDeliverable(agent, id string) error {
	row, err := b.deliverRow(agent, id)
	if err != nil {
		return err
	}
	if !session.AppendDeliverableUpdate(appMediaIndex(), row, func(f *session.DeliverableFact) { f.Removed = true }) {
		return appwire.Errorf(appwire.CodeEngine, "the removal could not be recorded")
	}
	b.deliverEvent(agent, row.ID, "removed")
	return nil
}

// SnapshotDeliverable answers deliverables.snapshot; see session.RegisterSnapshot.
func (b *appBackend) SnapshotDeliverable(agent, path, url, process string) (appwire.Deliverable, error) {
	if _, err := b.find(agent); err != nil {
		return appwire.Deliverable{}, err
	}
	env := b.deliverEnv()
	known := env.agent(agent)
	if process != "" {
		owner, row, err := b.processRow(process)
		if err != nil {
			return appwire.Deliverable{}, err
		}
		if owner != agent {
			return appwire.Deliverable{}, appwire.Errorf(appwire.CodeBadParams, "that process belongs to another conversation")
		}
		want, ok := session.LoopbackOrigin(row.URL)
		if !ok {
			return appwire.Deliverable{}, appwire.Errorf(appwire.CodeBadParams, "that process has not printed an address to take a snapshot of")
		}
		if got, ok := session.LoopbackOrigin(url); !ok || got != want {
			return appwire.Deliverable{}, appwire.Errorf(appwire.CodeBadParams, "that page is not the one the process serves")
		}
	}
	row, created, err := session.RegisterSnapshot(context.Background(), appMediaIndex(), agent, path, url, known.folders)
	if err != nil {
		switch {
		case errors.Is(err, session.ErrSnapshotFile), errors.Is(err, session.ErrSnapshotURL):
			return appwire.Deliverable{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
		case errors.Is(err, session.ErrMediaBusy), errors.Is(err, session.ErrDeliverIndexTooLarge):
			// Whether this picture is already registered is unknown, and
			// nothing was recorded: busy (or the history's own refusal).
			return appwire.Deliverable{}, appDeliverError(err)
		}
		return appwire.Deliverable{}, appEngineError(err)
	}
	if created {
		b.deliverEvent(agent, row.ID, "snapshot")
	}
	return env.wire(row), nil
}

// TaskBranches answers branches.list: one page, within appBranchListTime.
func (b *appBackend) TaskBranches(q appwire.BranchesQuery) (appwire.BranchesResult, error) {
	known := b.deliverEnv().agent(q.Agent)
	if !known.known {
		if _, err := b.find(q.Agent); err != nil {
			return appwire.BranchesResult{}, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), appBranchListTime)
	defer cancel()
	page, err := session.ListTaskBranches(ctx, known.place, known.workspace, session.TaskBranchQuery{Cursor: q.Cursor, Limit: q.Limit})
	if err != nil {
		return appwire.BranchesResult{}, appBranchError(err)
	}
	out := appwire.BranchesResult{Repo: page.Repo, Head: page.Head, Next: page.Next, Incomplete: page.Incomplete, Branches: make([]appwire.TaskBranch, 0, len(page.Branches))}
	if page.Repo {
		recorded := page.Recorded
		out.Recorded = &recorded
	}
	for _, f := range page.Branches {
		branch := appwire.TaskBranch{Name: f.Name, Task: f.Task, Title: f.Title, TitleTruncated: f.TitleTruncated, Head: f.Head, Base: f.Base, CountsCapped: f.Capped}
		if f.CountsKnown {
			files, added, removed := f.Files, f.Added, f.Removed
			branch.Files, branch.Added, branch.Removed = &files, &added, &removed
		}
		out.Branches = append(out.Branches, branch)
	}
	return out, nil
}

// appBranchError is a branch listing's refusal as the wire says it. None of
// them is an empty answer: a listing that could not be had is not "none".
func appBranchError(err error) error {
	switch {
	case errors.Is(err, session.ErrTaskBranchCursor):
		return appwire.Errorf(appwire.CodeBadParams, "that cursor is no longer valid; list again from the start")
	case errors.Is(err, session.ErrTaskBranchBusy):
		return appwire.Errorf(appwire.CodeBusy, "git did not answer in time; try again")
	case errors.Is(err, session.ErrTaskBranchRecordsTooLarge):
		return appwire.Errorf(appwire.CodeTooLarge, "%s", err.Error())
	}
	return appwire.Errorf(appwire.CodeEngine, "%s", err.Error())
}

var _ appwire.Deliverer = (*appBackend)(nil)

// appDeliverError is appMediaError plus the one refusal deliverables add: a
// history with more changes than can be listed exactly is too_large, said so.
func appDeliverError(err error) error {
	if errors.Is(err, session.ErrDeliverIndexTooLarge) {
		return appwire.Errorf(appwire.CodeTooLarge, "%s", err.Error())
	}
	return appMediaError(err)
}

// artifactCreated says deliverable.changed (created) for an authored row the
// session reported after a successful append. The agent is the conversation
// whose own stream carried it, so ownership is the engine's and not a join the
// app makes. The same tool call arriving twice is said once, while later calls
// updating the same deliverable invalidate its card again.
func (b *appBackend) artifactCreated(agent string, live *appLive, change session.ArtifactChange) {
	if live == nil || change.ID == "" || len(change.ID) > 128 || len(change.Run) > 1024 || len(change.Call) > 1024 {
		return
	}
	id := strings.Clone(change.ID)
	sum := sha256.Sum256([]byte(change.Run + "\x00" + change.Call))
	key := id + ":" + hex.EncodeToString(sum[:])
	fresh := false
	b.locked(func() {
		if live.artifactSeen == nil {
			live.artifactSeen = map[string]bool{}
		}
		if live.artifactSeen[key] {
			return
		}
		live.artifactSeen[key] = true
		live.artifactRecent = append(live.artifactRecent, key)
		fresh = true
		if len(live.artifactRecent) > appMediaRecentMax {
			delete(live.artifactSeen, live.artifactRecent[0])
			copy(live.artifactRecent, live.artifactRecent[1:])
			live.artifactRecent = live.artifactRecent[:appMediaRecentMax]
		}
	})
	if fresh {
		live.box.Event(appwire.EventDeliverableChanged, appwire.DeliverableChanged{Agent: agent, ID: id, Reason: "created"})
	}
}

// readBranchDeliverable reads the blob the source pinned. A version the caller
// saw that is not that blob is a change since they looked, refused before any
// byte is read.
func readBranchDeliverable(ctx context.Context, source session.DeliverableSource, version string, max int) (appwire.DeliverableText, error) {
	at := source.Live.Version
	if version != "" && version != at {
		return appwire.DeliverableText{}, &appwire.Error{Code: appwire.CodeConflict, Message: "the file changed since you looked", Detail: appwire.ConflictDetail{Reason: "changed"}}
	}
	data, truncated, err := session.ReadDeliverableBlob(ctx, source, max)
	if err != nil {
		return appwire.DeliverableText{}, appwire.Errorf(appwire.CodeNotFound, "the file is not there to read")
	}
	for truncated && len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	if !utf8.Valid(data) {
		return appwire.DeliverableText{}, appwire.Errorf(appwire.CodeBadParams, "that file is not text")
	}
	return appwire.DeliverableText{Text: string(data), Bytes: int64(len(data)), Truncated: truncated, Version: at}, nil
}
