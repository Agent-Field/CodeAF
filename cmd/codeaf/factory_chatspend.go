package main

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/processgroup"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE JOB'S CHAT SPENDS FROM THE JOB'S BUDGET ─────────────────────────────
//
// Everything done for the job spends from the one budget. A step's rounds
// always did: the runner books each round's dollars on the job. The job's own conversation, its manager, is an
// ordinary conversation, and until now its turns reached only the person's
// spend ledger. [session.JobChatDoor] is how they reach the job too, and this
// is that door over this machine's floor.
//
// THE MONEY GOES WHERE THE JOB'S BUDGET IS CHECKED. While a worker of this
// machine runs the job, its runner checks the budget after every round on the
// worker's mirror of the job (internal/factory/worker), so the chat's dollars
// go on the mirror's run, and the worker's report carries them to the factory
// with the round's, once. Otherwise they go on the floor's own job: on its run
// when it has one, and before any run as Item.ChatSpent, which the next start
// carries into the run ([factory.AddChatSpend]). A job that lives on another
// machine's factory and is not running here has no record on this machine to
// spend from, and its chat's dollars stay on the ledger alone.
//
// COUNTED ONCE. The ledger row each call writes stays the person's own record
// of every dollar; the job's figure is a second reading of the same dollars
// that nothing adds to the ledger, as a stage's round already is.

// jobChatDoorOf is the door over floor, or nil with no floor: A CAPABILITY
// THAT CANNOT WORK IS ABSENT.
func jobChatDoorOf(floor *store.Store) session.JobChatDoor {
	if floor == nil {
		return nil
	}
	return jobChatDoor{floor: floor}
}

// jobChatDoor finds the job a conversation is the own chat of, by the job's
// Talk: on the floor, else on a live worker's mirror (a job of another
// machine's factory that a worker here is running).
type jobChatDoor struct{ floor *store.Store }

func (d jobChatDoor) Chat(transcript string) session.JobChat {
	talk := talkPath(transcript)
	if talk.clean == "" {
		return nil
	}
	if items, err := d.floor.List(); err == nil {
		for _, it := range items {
			if talk.is(it.Talk) {
				return &jobChat{floor: d.floor, id: it.ID, talk: talk, name: factory.LedgerName(it)}
			}
			if ownsStageChat(it, talk) {
				return ownedStageBudget{&jobChat{floor: d.floor, id: it.ID, talk: talk, name: factory.LedgerName(it)}}
			}
		}
	}
	for _, mirror := range liveMirrors(d.floor, "*") {
		items, err := mirror.List()
		if err != nil {
			continue
		}
		for _, it := range items {
			if talk.is(it.Talk) {
				return &jobChat{floor: d.floor, id: it.ID, talk: talk, name: factory.LedgerName(it)}
			}
			if ownsStageChat(it, talk) {
				return ownedStageBudget{&jobChat{floor: d.floor, id: it.ID, talk: talk, name: factory.LedgerName(it)}}
			}
		}
	}
	return nil
}

type ownedStageBudget struct{ session.JobChat }

// FactoryJob names the job on this chat's ledger rows.
func (c ownedStageBudget) FactoryJob() string {
	if named, ok := c.JobChat.(session.JobLedgerName); ok {
		return named.FactoryJob()
	}
	return ""
}

// FactoryJob names the job on this chat's ledger rows.
func (c *jobChat) FactoryJob() string { return c.name }

func (c ownedStageBudget) FactoryAuto() bool {
	if p, ok := c.JobChat.(interface{ FactoryAuto() bool }); ok {
		return p.FactoryAuto()
	}
	return false
}

func (c *jobChat) FactoryAuto() bool {
	for _, st := range c.holders() {
		it, err := st.Get(c.id)
		if err == nil && (c.talk.is(it.Talk) || ownsStageChat(it, c.talk)) {
			return factory.Automatic(it)
		}
	}
	return false
}

// jobChat is one job's chat's money, found again on every call, because the
// store that holds it moves when a worker takes the job and when it lets go.
type jobChat struct {
	floor  *store.Store
	id     int
	talk   talkKey
	failed atomic.Bool
	// name is the job as the spend ledger names it ([factory.LedgerName]).
	name string

	// read is what each holder's document last answered, keyed by its path.
	// THE BUDGET IS WATCHED TEN TIMES A SECOND FOR AS LONG AS THE CHAT IS OPEN
	// (session.watchJobBudget), and a job's document carries its whole step
	// log, so decoding it on every look kept an idle engine busy. A document
	// is decoded again only when the file itself has changed.
	mu   sync.Mutex
	read map[string]chatRead
}

// chatRead is one holder's document as Refusal last read it: the file it
// was, and whether that document is this chat's job and, if so, its line.
type chatRead struct {
	file    os.FileInfo
	ours    bool
	refusal string
}

// sameFile is whether info is still the document read was taken from. Every
// store write renames a fresh file into place, so a write always shows as a
// different file; size and modification time also catch a write in place.
func (r chatRead) sameFile(info os.FileInfo) bool {
	return os.SameFile(r.file, info) && r.file.Size() == info.Size() && r.file.ModTime().Equal(info.ModTime())
}

// Spend books usd on the job where its budget is checked now.
func (c *jobChat) Spend(usd float64) {
	for _, st := range c.holders() {
		err := st.Annotate(c.id, func(it *factory.Item) error {
			if !c.talk.is(it.Talk) && !ownsStageChat(*it, c.talk) {
				return errNotThisChat
			}
			factory.AddChatSpend(it, usd)
			return nil
		})
		if err == nil {
			return
		}
	}
	c.failed.Store(true)
}

// Refusal is the job's budget line once it is spent, read where it is checked.
func (c *jobChat) Refusal() string {
	if c.failed.Load() {
		return "job spending could not be recorded; work stopped"
	}
	for _, st := range c.holders() {
		if r, ok := c.readOf(st); ok && r.ours {
			return r.refusal
		}
	}
	return "job budget could not be read; work stopped"
}

// readOf is st's document for this job, decoded again only when its file has
// changed since the last look. A document that cannot be read is not
// remembered, so the next look tries it again, as every look used to.
func (c *jobChat) readOf(st *store.Store) (chatRead, bool) {
	path := st.ItemPath(c.id)
	info, err := os.Stat(path)
	if err != nil {
		return chatRead{}, false
	}
	if last, seen := c.remembered(path); seen && last.sameFile(info) {
		return last, true
	}
	// The file is stat'd before it is read, so a write landing between the two
	// leaves the newer document under the older file's identity, and the next
	// look reads it again rather than keeping a stale answer.
	it, err := st.Get(c.id)
	if err != nil {
		return chatRead{}, false
	}
	r := chatRead{file: info, ours: c.talk.is(it.Talk) || ownsStageChat(it, c.talk)}
	if r.ours {
		r.refusal = factoryrun.ChatRefusal(it)
	}
	c.remember(path, r)
	return r, true
}

// remembered is the last read of the document at path, if there was one.
func (c *jobChat) remembered(path string) (chatRead, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.read[path]
	return r, ok
}

// remember keeps r as the last read of the document at path.
func (c *jobChat) remember(path string, r chatRead) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.read == nil {
		c.read = map[string]chatRead{}
	}
	c.read[path] = r
}

// holders is where the job's money may be, in the order it is looked for: the
// mirror of a live worker running it, then the floor. A mirror the worker let
// go of between the look and the write refuses the write, and the floor
// takes it.
func (c *jobChat) holders() []*store.Store {
	return append(liveMirrors(c.floor, strconv.Itoa(c.id)+".json"), c.floor)
}

// errNotThisChat refuses a write on a job whose conversation is not this one
// any more: a mirror of another factory's job with the same number, or a job
// whose chat was dropped while a call was in flight.
var errNotThisChat = errors.New("this job's chat is another conversation")

// liveMirrors is every mirror of a live worker process on this machine
// holding a file named file (a glob; "*" is any), as
// `<floor>/workers/<pid>/<factory>/`, the layout appFactoryHost.runWorker
// writes. A dead process's mirror is not a worker running anything.
func liveMirrors(floor *store.Store, file string) []*store.Store {
	if floor == nil {
		return nil
	}
	held, _ := filepath.Glob(filepath.Join(floor.Root(), "workers", "*", "*", file))
	seen := map[string]bool{}
	var out []*store.Store
	for _, path := range held {
		dir := filepath.Dir(path)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		pid, err := strconv.Atoi(filepath.Base(filepath.Dir(dir)))
		if err != nil || pid <= 0 || !processgroup.ProcessAlive(pid) {
			continue
		}
		if mirror, err := store.Open(dir); err == nil {
			out = append(out, mirror)
		}
	}
	return out
}

// talkKey is a conversation's file as the job names it: its clean path, and
// the path with its links followed when the two differ.
type talkKey struct{ clean, real string }

func talkPath(transcript string) talkKey {
	t := strings.TrimSpace(transcript)
	if t == "" {
		return talkKey{}
	}
	k := talkKey{clean: filepath.Clean(t)}
	if real, err := filepath.EvalSymlinks(k.clean); err == nil {
		k.real = filepath.Clean(real)
	}
	return k
}

// is says whether a job's Talk names this conversation. Links are followed
// only for a Talk in a session folder of the same name, so a floor of many
// jobs costs one comparison each.
func (k talkKey) is(talk string) bool {
	talk = strings.TrimSpace(talk)
	if talk == "" || k.clean == "" {
		return false
	}
	other := filepath.Clean(talk)
	if other == k.clean || other == k.real {
		return true
	}
	if k.real == "" || filepath.Base(filepath.Dir(other)) != filepath.Base(filepath.Dir(k.clean)) {
		return false
	}
	real, err := filepath.EvalSymlinks(other)
	return err == nil && filepath.Clean(real) == k.real
}

// Only the manager's own persistent job association exposes this door.
func (c *jobChat) MaintainPR(ctx context.Context, words string) (string, error) {
	history, err := record.Open(c.floor, record.Options{})
	if err != nil {
		return "", err
	}
	trailer, err := history.Trailer(ctx, c.id)
	if err != nil {
		return "", err
	}
	err = c.floor.Update(c.id, func(it *factory.Item) error {
		if !c.talk.is(it.Talk) {
			return errNotThisChat
		}
		if it.Manual {
			return errors.New("Manual control is active. Finish the working chat and use Resume pipeline.")
		}
		if it.PullRequest == nil || it.PullRequest.State != factory.PROpen {
			return errors.New("There is no open PR to maintain.")
		}
		if it.State == factory.StateRunning || it.State == factory.StateQueued {
			return errors.New("The pipeline is already working. Your request is steering for its next step.")
		}
		if !factoryrun.ValidTrailer(trailer) {
			return errors.New("Publish the initial PR with approval before authorizing maintenance.")
		}
		if it.FollowUp == nil {
			factory.WatchPublishedPR(it)
		}
		f := it.FollowUp
		f.PublishConsent = trailer
		f.AutoPush = true
		f.Request = words
		f.State = "watching"
		f.EventKey = ""
		f.Attempt = 0
		it.Notes = append(it.Notes, words)
		it.State = factory.StateShipped
		factory.QueueCIFollowUp(it, time.Now().UTC())
		return nil
	})
	if err != nil {
		return "", err
	}
	return "Follow PR resumed with your request. Verified repairs will update the existing PR; merge remains yours.", nil
}

func ownsStageChat(it factory.Item, talk talkKey) bool {
	if it.Stream == nil {
		return false
	}
	for _, ph := range it.Stream.Phases {
		if talk.is(ph.Chat) {
			return true
		}
		for _, r := range ph.Rounds {
			if talk.is(r.Chat) {
				return true
			}
		}
	}
	return false
}
