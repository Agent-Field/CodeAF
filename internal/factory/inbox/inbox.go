// Package inbox is the factory's generic intake: work that arrives from any
// outside system — a ticket tracker, an alert, a form, a script, a person's
// `codeaf factory add` — becomes one job, the same way a GitHub issue does.
// Every connector ([Feed], driven by [Poll]) and every trigger door (the wire,
// the command line, the factory's HTTP endpoint) comes through [Add], so
// checking, deduplication and filing are written once.
//
// THIS PACKAGE NAMES NO VENDOR. A connector lives in its own package, says
// its own name through [Feed.Name], and answers back through a [Replier] it
// registers; nothing here knows which systems exist.
//
// docs/design/factory-general/DESIGN.md is the contract this package fills.
package inbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// Entry is one piece of outside work, in the source's own words.
type Entry struct {
	// Source names the system: "jira", or "inbox:<name>" for a trigger.
	Source string `json:"source"`
	// ExternalID is the item's key on that system ("ENG-142"). Source and
	// ExternalID together are the identity: a second Add of the same pair
	// updates the job it already made and never makes a second one.
	ExternalID string   `json:"external_id"`
	URL        string   `json:"url,omitempty"`
	Title      string   `json:"title"`
	Body       string   `json:"body,omitempty"`
	Author     string   `json:"author,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	Priority   string   `json:"priority,omitempty"` // p0..p3; the source maps its own scale
	// Repo is the repository the sender already knows the work is on. Empty
	// leaves it to the routing rules; with neither, the job waits in Backlog
	// asking which repository.
	Repo string `json:"repo,omitempty"`
	// Trust is how far the sender vouches for the author's words: owner,
	// collaborator or stranger ([factory.Tier]). Empty is owner, because every
	// door but a connector is one the operator holds the key to. A CONNECTOR
	// ALWAYS SAYS: a Jira reporter is never the operator, and a stranger is
	// the one tier an AI yes can never start and whose job never writes.
	Trust factory.Tier `json:"trust,omitempty"`
	// RunOn is an AgentField reasoner, "<node>.<reasoner>", that runs this job
	// instead of the factory's own steps (factory.Item.RunOn).
	RunOn string `json:"run_on,omitempty"`
}

// tier is the [factory.Item.Tier] a first delivery of e files its job with.
func (e Entry) tier() factory.Tier {
	if e.Trust == "" {
		return factory.TierOwner
	}
	return e.Trust
}

// lowers says whether a later delivery's trust is below the job's. A
// delivery may LOWER a job's tier, never raise it: a ticket handed to a
// stranger reporter stops writing at once, while trusting someone more is a
// person's act on the job, not a field an outside system can send.
func lowers(have, sent factory.Tier) bool { return trustRank[sent] < trustRank[have] }

var trustRank = map[factory.Tier]int{factory.TierStranger: 0, factory.TierCollab: 1, factory.TierOwner: 2}

// Result says what Add did.
type Result struct {
	ID      int  `json:"id"`
	Created bool `json:"created"`
}

// SetBySource is the [factory.JobPriority.SetBy] of a priority the outside
// system sent. A priority with any other SetBy is a person's, and a later
// delivery never overwrites it.
const SetBySource = "source"

// Ref is the [factory.Item.SourceRef] an entry from source with id is kept
// under: `<source>:<id>`. The source part is also the item's Origin, which is
// how the two halves are told apart again when the source name itself holds
// a colon (`inbox:ops:A1`).
func Ref(source, id string) string { return source + ":" + id }

// Split is the inverse of [Ref] for a job the inbox filed: its source and its
// key on that source. ok is false for every other job, a GitHub issue and a
// job typed on the floor among them.
func Split(it factory.Item) (source, id string, ok bool) { return factory.SourceKey(it) }

// Option adjusts how [Add] files a job.
type Option func(*options)

type options struct {
	recipe func(repo string) (factory.Recipe, error)
}

// WithRecipe says where a repository's recipe is read, so a job filed on a
// known repository starts with that repository's steps, as one typed on the
// floor does. Without it, or when the read fails, the job takes the default
// steps, which its first start replaces with the file it is decided on.
func WithRecipe(read func(repo string) (factory.Recipe, error)) Option {
	return func(o *options) { o.recipe = read }
}

func (o options) stagesFor(repo string) []factory.Stage {
	if repo != "" && o.recipe != nil {
		if r, err := o.recipe(repo); err == nil {
			if stages := r.For(factory.KindIssue); len(stages) > 0 {
				return factory.CopyStages(stages)
			}
		}
	}
	return factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))
}

// Add files e on the floor, or updates the job it already made.
//
// THE IDENTITY IS (Source, ExternalID), kept as the job's SourceRef ([Ref]).
// A new pair makes a job in Backlog: an issue, Origin the source, Num 0, the
// owner's tier (every door into Add is authenticated as the factory's), and
// the repository's steps when Repo is given. A pair already on the floor has
// its outside words brought up to date the way the GitHub poll brings an
// issue's (github.Merge): title, body, labels, address, and a priority only
// while the source is the one that set it. EVERYTHING A PERSON SET IS KEPT —
// state, steps, assignee, budget, a priority they chose, a repository they
// picked. A dismissed job whose words changed comes back as new, because
// dismissing hides a job until it changes.
//
// NUM STAYS 0, NEVER A SYNTHETIC NUMBER. Every GitHub-only side effect (the
// issue comment, `Closes #N` in the pull request, close on merge, the
// standing recheck) is guarded by `Origin == github && Num > 0`, the same
// guard a job typed on the floor passes by; a made-up number would satisfy
// half of it and one day the other half. The job's own ID names it.
//
// A JOB WITH NO REPOSITORY IS STILL FILED, waiting in Backlog for one; the
// launch refuses it until a rule or a person gives it one.
func Add(ctx context.Context, st *store.Store, e Entry, now time.Time, opts ...Option) (Result, error) {
	if st == nil {
		return Result{}, errors.New("the factory keeps no store here")
	}
	e, err := Validate(e)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	// ONE ADD AT A TIME PER FLOOR, across processes: the look-up and the
	// create are one critical section, or two deliveries of one ticket racing
	// each other would both find nothing and both make a job.
	release, err := lockInbox(st)
	if err != nil {
		return Result{}, err
	}
	defer release()
	ref := Ref(e.Source, e.ExternalID)
	have, err := st.ListSaved()
	if err != nil {
		return Result{}, err
	}
	for _, old := range have {
		if old.SourceRef == ref {
			return Result{ID: old.ID}, update(st, old, e, now, o)
		}
	}
	id, err := st.Add(ctx, newItem(e, now, o))
	if err != nil {
		return Result{}, err
	}
	// The source hears that its work is a job once the job is kept, never
	// before, and never on an update: a second delivery files nothing new.
	Notify(ctx, st, id, Filed(e.Title))
	return Result{ID: id, Created: true}, nil
}

// newItem is the job a first delivery of e makes.
func newItem(e Entry, now time.Time, o options) factory.Item {
	it := factory.Item{
		Kind:      factory.KindIssue,
		Origin:    factory.Origin(e.Source),
		SourceRef: Ref(e.Source, e.ExternalID),
		Title:     e.Title,
		Body:      e.Body,
		Author:    e.Author,
		URL:       e.URL,
		Labels:    slices.Clone(e.Labels),
		Tier:      e.tier(),
		Autonomy:  factory.AutonomyAuto,
		Priority:  priorityOf(e),
		Created:   now,
		Changed:   now,
		Ask:       &factory.JobAsk{Text: askText(e), At: now, Source: e.Source},
		Triage:    factory.Triage{Type: factory.GuessType(e.Title)},
		Stages:    o.stagesFor(e.Repo),
	}
	if e.Repo != "" {
		it.Repo, it.Places = e.Repo, []string{e.Repo}
	}
	if run := strings.TrimSpace(e.RunOn); run != "" {
		it.RunOn = run
	}
	it.Note(now, arrived(e))
	if e.Repo == "" {
		it.Note(now, WaitingWords)
	}
	return it
}

// WaitingWords is the activity line of a job filed with no repository.
const WaitingWords = "waiting for a repository"

// arrived is the first activity line: `arrived from <source>`, and the
// address on the source when it sent one.
func arrived(e Entry) string {
	if e.URL == "" {
		return "arrived from " + e.Source
	}
	return "arrived from " + e.Source + " · " + e.URL
}

// askText is what the pull request quotes as the request ([factory.AskText]).
func askText(e Entry) string {
	if e.Body != "" {
		return e.Body
	}
	return e.Title
}

// update brings an existing job's outside words up to date. A delivery that
// changes nothing writes nothing, so a feed that cannot say "since" and
// hands back every ticket each pass leaves the floor untouched.
func update(st *store.Store, old factory.Item, e Entry, now time.Time, o options) error {
	if !changed(old, e) {
		return nil
	}
	return st.Update(old.ID, func(it *factory.Item) error {
		words := it.Title != e.Title || it.Body != e.Body
		labels := !sameLabels(it.Labels, e.Labels)
		if words {
			factory.ClearRead(&it.Triage)
			it.Triage.Type = factory.GuessType(e.Title)
		}
		if words || labels {
			it.Note(now, "changed on "+e.Source)
		}
		if labels {
			factory.ReaskStart(it)
		}
		it.Title, it.Body = e.Title, e.Body
		it.Labels = slices.Clone(e.Labels)
		if e.URL != "" {
			it.URL = e.URL
		}
		if e.Author != "" {
			it.Author = e.Author
		}
		if lowers(it.Tier, e.tier()) {
			it.Tier = e.tier()
		}
		if it.Ask != nil && it.Ask.Source == e.Source {
			it.Ask.Text = askText(e)
		}
		if sourcePriority(*it) {
			it.Priority = priorityOf(e)
		}
		if factory.NamesNoRepo(*it) && e.Repo != "" {
			it.Repo, it.Places = e.Repo, []string{e.Repo}
			// A job that never ran takes the repository's steps with it; one
			// that ran keeps the steps it ran.
			if it.Stream == nil && len(it.Adapted) == 0 && !it.PipelineIsDecided() {
				it.Stages = o.stagesFor(e.Repo)
			}
		}
		it.DismissedBy = nil
		if it.State == factory.StateDismissed && it.DoneHow != "dropped" {
			it.State = factory.StateNew
		}
		return nil
	})
}

// changed says whether e says anything the job does not already hold.
func changed(it factory.Item, e Entry) bool {
	if it.Title != e.Title || it.Body != e.Body || !sameLabels(it.Labels, e.Labels) || lowers(it.Tier, e.tier()) {
		return true
	}
	if (e.URL != "" && it.URL != e.URL) || (e.Author != "" && it.Author != e.Author) {
		return true
	}
	if factory.NamesNoRepo(it) && e.Repo != "" {
		return true
	}
	if sourcePriority(it) {
		want := priorityOf(e)
		if (want == nil) != (it.Priority == nil) || (want != nil && want.Value != it.Priority.Value) {
			return true
		}
	}
	return false
}

// sourcePriority says the job's priority is the source's to change: it has
// none, or the source is who set it.
func sourcePriority(it factory.Item) bool {
	return it.Priority == nil || it.Priority.SetBy == SetBySource
}

// priorityOf is e's priority as the floor keeps one ([factory.ValidPriority]),
// nil when it sent none.
func priorityOf(e Entry) *factory.JobPriority {
	if word := priorityWords[e.Priority]; word != "" {
		return &factory.JobPriority{Value: word, SetBy: SetBySource}
	}
	return nil
}

func sameLabels(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// lockInbox holds <root>/inbox.lock until release.
func lockInbox(st *store.Store) (release func(), err error) {
	if err := os.MkdirAll(st.Root(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(st.Root(), "inbox.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(f, true, false); err != nil {
		f.Close()
		return nil, fmt.Errorf("the inbox is busy: %w", err)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
	}, nil
}
