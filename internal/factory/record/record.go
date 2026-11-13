// Package record keeps the factory's durable account of human decisions and
// outward actions, so publishing can cite consent rather than infer it.
package record

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

const (
	StepEdited           = "step_edited"
	StepStopped          = "step_stopped"
	StepRerun            = "step_rerun"
	HandOffNotNow        = "hand_off_not_now"
	Dismissed            = "dismissed"
	Assigned             = "assigned"
	Unassigned           = "unassigned"
	Nudged               = "nudged"
	PriorityChanged      = "priority_changed"
	BudgetRaised         = "budget_raised"
	BudgetChanged        = "budget_changed"
	StopAfterStepChanged = "stop_after_step_changed"
	Undone               = "undone"
	Started              = "started"
	RunAsNew             = "run_as_new"
	// Held is a person keeping a queued job from starting; Resumed is the
	// same person, or another owner, letting it go on.
	Held        = "held"
	Resumed     = "resumed"
	Stopped     = "stopped"
	AutoStarted = "auto_started"
	AskedFirst  = "asked_first"
	// Routed is the factory's routing table acting on a job without starting
	// or holding it: a rule that filed it in Backlog, a rule that now matches
	// in place of the one that routed it, or a rule that matched and could
	// not be applied. Detail is the sentence a person reads.
	Routed = "routed"
	// RepoSet is a person giving a job that never ran its repository
	// (factory.job_set_repo). Detail names the repository.
	RepoSet       = "repo_set"
	Approved      = "approved"
	Answered      = "answered"
	Posted        = "posted"
	Took          = "took"
	Dropped       = "dropped"
	ClosedLocally = "closed_locally"
	// Released is un-take: the person who took the job let it go. It does not
	// withdraw an approval the way a drop does.
	Released       = "released"
	RequestChanges = "request_changes"
	// PRMerged and PRClosed are the poll noticing a pull request this factory
	// opened. The handle, when GitHub named one, is who merged it.
	PRMerged = "pr_merged"
	// PRReady records a person marking the job's draft ready for review.
	PRReady  = "pr_ready"
	PRClosed = "pr_closed"
	// Restored is a person bringing a dropped job back to Backlog.
	Restored = "restored"
	// SourceClosed is a poll or start finding that the source issue or pull
	// request ended on GitHub. Its verb says closed or merged.
	SourceClosed = "source_closed"
	// MergeAsked is a person pressing Merge, inside its Undo window;
	// MergeQueued a merge the gate passed and sent to the runner (Detail
	// names the rule: the person, or the pipeline's auto-merge); and
	// MergeRefused a merge the gate or the forge refused, with the words.
	MergeAsked   = "merge_asked"
	MergeQueued  = "merge_queued"
	MergeRefused = "merge_refused"
	// StepFailed is a step that errored. It is not a person's decision. The
	// detail is code, step and next, so the audit can show what happened.
	StepFailed      = "step_failed"
	RunnerAdded     = "runner_added"
	RunnerChanged   = "runner_changed"
	ReposChanged    = "repos_changed"
	LinkCreated     = "link_created"
	LinkRevoked     = "link_revoked"
	SessionsRevoked = "sessions_revoked"
	OperatorClaimed = "operator_claimed"
	GitHubGranted   = "github_granted"
	// The organization's own changes, each with Job 0 and an Object naming
	// what changed: "team:<id>", "workstream:<id>", "policy:<group>", or
	// "factory" for the factory-wide rules, routes, spending and settings.
	// The verb is chosen by the writer from [OrgVerbs]; the actor is always
	// the signed-in person the server resolved, never a field a client sent.
	TeamChanged       = "team_changed"
	WorkstreamChanged = "workstream_changed"
	PolicyChanged     = "policy_changed"
	RulesChanged      = "rules_changed"
	RoutesChanged     = "routes_changed"
	SpendingChanged   = "spending_changed"
	SettingsChanged   = "settings_changed"
)

// orgKinds are the organization change kinds; none of them names a job.
var orgKinds = map[string]bool{TeamChanged: true, WorkstreamChanged: true, PolicyChanged: true,
	RulesChanged: true, RoutesChanged: true, SpendingChanged: true, SettingsChanged: true}

// OrgVerbs are the words an organization change may read as, by kind. They
// are what a person reads in an activity list, so each is plain words.
var OrgVerbs = map[string][]string{
	TeamChanged: {"created team", "renamed team", "deleted team", "added to team", "removed from team"},
	WorkstreamChanged: {"created workstream", "imported workstream", "changed workstream", "archived workstream",
		"unarchived workstream", "deleted workstream", "changed owner", "changed sharing",
		"added page", "changed page", "removed page", "restored page", "changed plan",
		"renamed page", "moved page", "added job", "removed job", "added wake", "changed wake",
		"added suggestion", "changed suggestion", "set budget", "posted spend"},
	PolicyChanged:   {"changed go-aheads"},
	RulesChanged:    {"changed rules"},
	RoutesChanged:   {"changed routing"},
	SpendingChanged: {"changed spending defaults"},
	SettingsChanged: {"changed settings"},
}

// IsOrgKind is whether kind is one of the organization's own changes.
func IsOrgKind(kind string) bool { return orgKinds[kind] }

// Event is the persisted and wire shape; approval receipts use these exact ids.
// Actor identifies the source of an event independently of its display name.
type Actor struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	Operator bool   `json:"operator,omitempty"`
}

// Event keeps both the stable receipt fields and S2 Activity fields.
type Event struct {
	Actor  Actor     `json:"actor"`
	Verb   string    `json:"verb"`
	Object string    `json:"object"`
	Rule   string    `json:"rule,omitempty"`
	ID     string    `json:"id"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Job    int       `json:"job"`
	Handle string    `json:"handle"`
	Cost   float64   `json:"cost"`
	Detail string    `json:"detail"`
	Commit string    `json:"commit,omitempty"`
}

// Options lets tests advance time without waiting on the wall clock.
type Options struct{ Now func() time.Time }

// Record holds no open files or cached history, so separate servers share truth.
type Record struct {
	root string
	now  func() time.Time
}

func Open(st *store.Store, options Options) (*Record, error) {
	if st == nil || st.Root() == "" {
		return nil, errors.New("factory record: no store")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Record{root: st.Root(), now: options.Now}, nil
}

// Root is where this record and its bounded live replay are persisted.
func (r *Record) Root() string {
	if r == nil {
		return ""
	}
	return r.root
}

// Append mints identity and time here, never from an agent's reported fields.
// SYNC BEFORE CONSENT: a returned receipt must already be on disk.
func (r *Record) Append(ctx context.Context, event Event) (Event, error) {
	// An approval without a handle must explicitly name the factory as actor.
	if event.Kind == Approved && factory.NormalizeHandle(event.Handle) == "" && event.Actor.Kind != "factory" {
		return Event{}, errors.New("factory record: approval requires a forge handle")
	}
	enrich(&event)
	if err := validate(event); err != nil {
		return Event{}, err
	}
	event.Handle = factory.NormalizeHandle(event.Handle)
	err := r.locked(ctx, true, func() error {
		events, err := r.read(ctx)
		if err != nil {
			return err
		}
		event.ID, err = mint(events)
		if err != nil {
			return err
		}
		event.At = r.now().UTC()
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(r.path(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err = f.Write(append(data, '\n')); err != nil {
			return err
		}
		if err = f.Sync(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		// Accepted decisions reach open pages even when they do not rewrite a job.
		kind := ""
		switch event.Kind {
		case Approved:
			kind = "approved"
		case Took:
			kind = "taken"
		case Nudged:
			kind = "nudged"
		}
		notifications := []live.Event{live.New("activity", event.Job, event)}
		if kind != "" {
			notifications = append(notifications, live.New(kind, event.Job, live.Payload{Job: event.Job, By: event.Handle}))
		}
		return live.Append(r.root, notifications...)
	})
	if err != nil {
		return Event{}, err
	}
	return event, nil
}

func validate(e Event) error {
	if orgKinds[e.Kind] {
		return validateOrg(e)
	}
	switch e.Kind {
	case RunAsNew, StopAfterStepChanged, StepEdited, StepStopped, StepRerun, HandOffNotNow, Dismissed, Assigned, Unassigned, Nudged, PriorityChanged, BudgetRaised, BudgetChanged, Undone, Started, Held, Resumed, Stopped, AutoStarted, AskedFirst, Routed, RepoSet, Approved, Answered, Posted, Took, Dropped, ClosedLocally, Restored, Released, RequestChanges, PRMerged, PRClosed, SourceClosed, MergeAsked, MergeQueued, MergeRefused, StepFailed, RunnerAdded, RunnerChanged, ReposChanged, LinkCreated, LinkRevoked, SessionsRevoked, OperatorClaimed, GitHubGranted, PRReady:
	default:
		return fmt.Errorf("factory record: unknown event kind %q", e.Kind)
	}
	switch e.Actor.Kind {
	case "person", "agent", "factory":
	default:
		return errors.New("factory record: actor must be person, agent or factory")
	}
	switch e.Verb {
	case "ran as new", "edited steps", "reran step", "put away hand-off", "started", "held", "resumed", "stopped", "asked", "approved", "answered", "published", "assigned", "dropped", "brought back", "unassigned", "requested changes", "merged", "closed", "closed locally", "pressed merge", "sent merge", "refused merge", "failed", "added runner", "changed runner", "connected repo", "disconnected repo", "created access link", "revoked access link", "revoked sessions", "dismissed", "nudged", "changed priority", "raised budget", "changed budget", "changed spending stop", "undone", "claimed factory", "granted GitHub read access", "marked ready for review", "routed", "set repository":
	default:
		return errors.New("factory record: unknown Activity verb")
	}
	if e.Commit != "" && !publish.ValidCommit(e.Commit) {
		return errors.New("factory record: approval requires a full commit SHA")
	}
	if e.Job <= 0 && e.Kind != RunnerAdded && e.Kind != RunnerChanged && e.Kind != ReposChanged && e.Kind != LinkCreated && e.Kind != LinkRevoked && e.Kind != SessionsRevoked && e.Kind != OperatorClaimed && e.Kind != GitHubGranted {
		return errors.New("factory record: job must be positive")
	}
	if e.Cost < 0 {
		return errors.New("factory record: cost must not be negative")
	}
	handle := factory.NormalizeHandle(e.Handle)
	if e.Kind == Approved && handle == "" && e.Actor.Kind != "factory" {
		return errors.New("factory record: approval requires a forge handle")
	}
	for _, c := range handle {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return errors.New("factory record: invalid forge handle")
		}
	}
	return nil
}

// validateOrg checks an organization change: a known verb for its kind, an
// object, no job, and a person who made it.
func validateOrg(e Event) error {
	known := false
	for _, v := range OrgVerbs[e.Kind] {
		known = known || v == e.Verb
	}
	if !known {
		return fmt.Errorf("factory record: %q is not a verb of %s", e.Verb, e.Kind)
	}
	if e.Job != 0 || e.Object == "" {
		return errors.New("factory record: an organization change names an object and no job")
	}
	if e.Actor.Kind != "person" {
		return errors.New("factory record: an organization change is made by a person")
	}
	for _, c := range factory.NormalizeHandle(e.Handle) {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return errors.New("factory record: invalid forge handle")
		}
	}
	return nil
}

func mint(events []Event) (string, error) {
	used := make(map[string]bool, len(events))
	for _, event := range events {
		used[event.ID] = true
	}
	for {
		var bits [4]byte
		if _, err := rand.Read(bits[:]); err != nil {
			return "", err
		}
		id := "evt_" + hex.EncodeToString(bits[:])
		if !used[id] {
			return id, nil
		}
	}
}

// Since returns events strictly after the supplied instant in append order.
func (r *Record) Since(ctx context.Context, since time.Time) ([]Event, error) {
	out := []Event{}
	err := r.locked(ctx, false, func() error {
		events, err := r.read(ctx)
		if err != nil {
			return err
		}
		for _, event := range events {
			if since.IsZero() || event.At.After(since) {
				out = append(out, event)
			}
		}
		return nil
	})
	return out, err
}

// Trailer reads the most recent consent for this job. A fresh run, a drop or
// requested changes withdraws it, so another job or an earlier run cannot attest.
func (r *Record) Trailer(ctx context.Context, id int) (string, error) {
	st, err := store.Open(r.root)
	if err != nil {
		return "", err
	}
	// Consent is read inside job mutations; author hydration would write
	// through the same item lock that the caller already holds.
	it, err := st.GetSaved(id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	return r.TrailerFor(ctx, id, it.HeadCommit)
}

// TrailerFor refuses consent for a different commit, including older unbound
// receipts when the job now carries a head.
func (r *Record) TrailerFor(ctx context.Context, id int, commit string) (string, error) {
	events, err := r.Since(ctx, time.Time{})
	if err != nil {
		return "", err
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Job != id {
			continue
		}
		switch e.Kind {
		case Approved:
			if err := validate(e); err != nil {
				return "", err
			}
			if len(e.ID) != 12 || !strings.HasPrefix(e.ID, "evt_") {
				return "", errors.New("factory record: invalid approval id")
			}
			if _, err := hex.DecodeString(e.ID[4:]); err != nil {
				return "", err
			}
			if e.Commit != commit {
				return "", publish.ErrCommitChanged
			}
			actor := "@" + factory.NormalizeHandle(e.Handle)
			if e.Actor.Kind == "factory" {
				actor = "factory"
			}
			trailer := "Factory-Approval: " + e.ID + " " + actor
			if e.Commit != "" {
				trailer += " " + e.Commit
			}
			return trailer, nil
		case Started, AutoStarted, Dropped, Restored, RequestChanges, Released:
			return "", errors.New("job requires a recorded approval before publishing")
		}
	}
	return "", errors.New("job requires a recorded approval before publishing")
}

func (r *Record) path() string { return filepath.Join(r.root, "events.jsonl") }

func (r *Record) locked(ctx context.Context, exclusive bool, action func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(r.root, "events.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := filelock.Lock(f, exclusive, false); err != nil {
		return err
	}
	defer func() { _ = filelock.Unlock(f) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return action()
}

// A malformed or unfinished tail refuses new writes: silently appending would
// erase the distinction between a complete decision and an interrupted write.
func (r *Record) read(ctx context.Context) ([]Event, error) {
	data, err := os.ReadFile(r.path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return nil, errors.New("factory record: incomplete events.jsonl tail")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	var events []Event
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var event Event
		err := decoder.Decode(&event)
		if err == io.EOF {
			return events, nil
		}
		if err != nil {
			return nil, fmt.Errorf("factory record: events.jsonl: %w", err)
		}
		enrich(&event)
		events = append(events, event)
	}
}
