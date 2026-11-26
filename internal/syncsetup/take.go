package syncsetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellindex"
	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatfiles"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// TakeOptions is what the surface tells the take side about this machine.
type TakeOptions struct {
	// OnPhase receives local continuation measurements when requested.
	OnPhase func(handoff.Phase)

	// WorkspaceRoots explicitly associates incoming working-root identities with
	// chosen local folders in the ownership prototype. It grants no landing
	// permission; task work is restored into private copies. Nil reuses only
	// associations already admitted in this device's registry.
	WorkspaceRoots map[string]string
	// DeviceName is this machine's name: the kept-edits sentence names it.
	DeviceName string
	// FromDevice is the holder's device name when Home or the caller already knows it (C6).
	FromDevice string
	// RootFor is where this machine keeps the chat with an id: the folder it
	// already has, or the one a new copy gets.
	RootFor func(id string) string
	// BucketFor files an arriving copy under its origin once the sealed
	// inventory has arrived. Nil keeps the root the caller supplied.
	BucketFor func(origin string) string
	// Notify hears the one sentence the vault hook says about what it left alone.
	Notify func(string)
	// NotifyAt is Notify for a surface that holds several chats and files each
	// sentence under the one it is about, as the app does. It is told which chat
	// the sentence belongs to, and Notify is not called when it is set.
	NotifyAt func(cell.Cell, string)
	// OnArrived hears the look at this computer that a background after-work
	// ends with, once the files are in. A chat that opens before its files
	// arrive carries only the part of that look that needs no files (what the
	// other computer left out or left running); what this computer lacks and
	// what is uncommitted in its folder are known only here. It is not called
	// for a take the caller waited on, whose Continued.Resume is already whole.
	OnArrived func(cell.Cell, preflight.Resume)
	// OnMove hears how far a move has got, a few times a second while it
	// changes.
	OnMove func(MoveProgress)
	// OnShown is called as soon as the conversation records and lease are in
	// place and the chat is ready to open (C1).
	OnShown func(Continued)
	// AsyncFiles runs workspace fetching, materialization and after-work in the
	// background even for single-frame chats without OnMove/OnShown.
	AsyncFiles bool
	// IdleWithin stops a move that waited this long on the relay with nothing
	// coming through, with ErrMoveStalled; 0 waits as long as the caller does.
	IdleWithin time.Duration
}

// Continued is a takeover that worked, with what the surface says about it.
type Continued struct {
	Taken handoff.Taken
	// AlreadyHere keeps an active local session attached when no move was needed.
	AlreadyHere bool
	// LocalOnly permits immediate local work without waiting for an uploading writer.
	LocalOnly bool
	// KeptTurns is how many turns the kept branch holds, 0 when nothing was kept.
	KeptTurns uint32
	// Device is this machine's name.
	Device string
	// TaskCopies is the names of the tasks whose working copies came along and
	// are back at work here, empty when none did.
	TaskCopies []string
	// Resume is what the chat left behind on the machine it came from that this
	// one lacks: folders a seal leaves out, commands that were running, tools it
	// needs. It is empty when there is nothing to say.
	Resume preflight.Resume
	// Shown is the elapsed time from the start of the take until the chat was ready to open (C9).
	Shown time.Duration
	// Files is the elapsed time from the start of the take until the workspace finished arriving (C9).
	Files time.Duration
	// Wait blocks until any background workspace fetch and after-work complete.
	Wait func(context.Context) error `json:"-"`
	// Retry retries a failed background workspace fetch (C4).
	Retry func() bool `json:"-"`
	// KeptBranch returns the kept branch id and turn count once Wait completes (F2).
	KeptBranch func() (string, uint32) `json:"-"`
}

// Continuer is the take side of one machine: `move here`.
type Continuer struct {
	sync     *Sync
	eng      cellstore.Engine
	opt      TakeOptions
	freeOnly bool
	// restartOnly makes a take do nothing but restart the files of a failed
	// arrival: when none is abandoned it stops before it reads or claims anything.
	restartOnly bool
}

// say puts one sentence in front of the person for chat at. EVERY AFTER-WORK
// FAILURE AND NOTICE GOES THROUGH HERE, because on dev these steps ran before
// the chat opened and a failure refused the take, so the person saw it; they
// now run behind the open, and a sentence with nowhere to go is a failure
// nobody is told about. A surface that files sentences per chat takes
// [TakeOptions.NotifyAt]; the terminal's one desk takes [TakeOptions.Notify].
func (c *Continuer) say(at cell.Cell, line string) {
	switch {
	case c.opt.NotifyAt != nil:
		c.opt.NotifyAt(at, line)
	case c.opt.Notify != nil:
		c.opt.Notify(line)
	}
}

// IdleHandoffAllowed reports whether the opt-in idle handoff setting is on.
func (c *Continuer) IdleHandoffAllowed() bool {
	return c != nil && c.sync != nil && c.sync.IdleHandoffAllowed()
}

// Continuer builds the take side over eng, the engine every chat this machine
// continues is sealed and restored by. It is the real Fetcher over the real
// SyncEngine, and the After hooks bring the chat's derived files and its
// secrets to the folder the tree landed in.
func (s *Sync) Continuer(eng cellstore.Engine, opt TakeOptions) *Continuer {
	eng.WorkspaceOf = func(at cell.Cell) string {
		// A staged tree can carry derived metadata from a borrowed folder. Its
		// restore must still target private scratch until admission and acquisition.
		if strings.HasSuffix(at.Root, handoff.StagingSuffix) && opt.RootFor != nil && filepath.Clean(at.Root) == filepath.Clean(opt.RootFor(at.ID)+handoff.StagingSuffix) {
			return session.Place{Dir: at.Root, Owned: true}.Work()
		}
		return workspaceOf(at.Root)
	}
	return &Continuer{sync: s, eng: eng, opt: opt}
}

// taker is the Taker for chat id. It is built per takeover because the store
// it fetches through is counted under the chat it serves, and so is the branch
// that keeps this machine's unsent edits: both are the cost of taking this chat.
func (c *Continuer) taker(ctx context.Context, sc *Scope) handoff.Taker {
	return c.takerWithDir(ctx, sc, c.sync.Dir)
}

func (c *Continuer) takerWithDir(ctx context.Context, sc *Scope, dir directory.Client) handoff.Taker {
	s := c.sync
	sync := c.eng.Sync(cellstore.SyncKeys{CellKey: s.Identity.CellKey(), Dedup: s.Identity.DedupSecret()}, s.Ledger)
	engine := landing{Engine: sync, workspace: c.eng.WorkspaceOf, rootFor: c.opt.RootFor}
	pub := &cellsync.Publisher{Engine: engine, Store: sc.Store, Dir: dir}
	// THE TAKEOVER'S BRANCHER RUNS WITH Map NIL. The chat being taken keeps its
	// id on this machine; only the edits set aside live in the branch, and a
	// mapping from the chat to that branch would make the chat open as the
	// branch. (The drive side's brancher records the mapping, because there the
	// local chat does become the branch.)
	brancher := cellsync.Brancher{Dir: dir, Publisher: pub, NewID: newCellID}
	return handoff.Taker{
		FreeOnly: c.freeOnly,
		OnPhase:  c.opt.OnPhase,
		Dir:      dir,
		Fetch:    &cellsync.Fetcher{Engine: engine, Store: sc.Store, Dir: dir, Inbox: sync.Inbox, ManyMax: cellsync.ManyMaxFor(s.Relay)},
		Local:    engineLocal{eng: c.eng, ctx: ctx},
		Branch:   c.branchWithDir(brancher, dir),
		RootFor:  c.opt.RootFor,
		InPlace:  func(c cell.Cell) bool { return borrowsProject(c.Root) },
		After:    []func(context.Context, cell.Cell) error{rebuildIndexes, recordOrigin, restoreChatFiles, c.pullVaultVia(sc)},
	}
}

// holds says this machine already holds most of chat id's history, so a copy
// fetches only what changed and the record's size says nothing about it.
func (c *Continuer) holds(id, root string) bool {
	s := c.sync
	return c.eng.Sync(cellstore.SyncKeys{CellKey: s.Identity.CellKey(), Dedup: s.Identity.DedupSecret()}, s.Ledger).Holds(cell.Cell{ID: id, Root: root})
}

// Take continues chat id here. Everything the surface says about it comes back
// with it.
func (c *Continuer) Take(ctx context.Context, id string) (Continued, error) {
	return c.TakeSeen(ctx, id, nil)
}

// TakeSeen preserves the holder and fence the continuation card displayed,
// rather than silently consenting to displace a different device.
func (c *Continuer) TakeSeen(ctx context.Context, id string, seen *directory.LeaseExpectation) (Continued, error) {
	return c.take(ctx, id, seen, false)
}

// OpenAvailable is an open gesture from a paired set's free-chat row. It
// restores through the ordinary continuation path, but never forces a hold.
func (c *Continuer) OpenAvailable(ctx context.Context, id string, seen *directory.LeaseExpectation) (Continued, error) {
	copy := *c
	copy.freeOnly = true
	return copy.take(ctx, id, seen, false)
}

// OpenAvailableWithWorkspaceRoots keeps a receiver's explicit folder choices
// while retaining the automatic open's prohibition on displacing a live hold.
func (c *Continuer) OpenAvailableWithWorkspaceRoots(ctx context.Context, id string, seen *directory.LeaseExpectation, roots map[string]string) (Continued, error) {
	copy := *c
	copy.freeOnly = true
	return copy.TakeSeenWithWorkspaceRoots(ctx, id, seen, roots)
}

// TakeSeenWithWorkspaceRoots applies only this person's selected receiver
// bindings to one takeover, retaining the holder expectation they approved.
func (c *Continuer) TakeSeenWithWorkspaceRoots(ctx context.Context, id string, seen *directory.LeaseExpectation, roots map[string]string) (Continued, error) {
	copy := *c
	copy.opt.WorkspaceRoots = make(map[string]string, len(c.opt.WorkspaceRoots)+len(roots))
	for uri, path := range c.opt.WorkspaceRoots {
		copy.opt.WorkspaceRoots[uri] = path
	}
	for uri, path := range roots {
		copy.opt.WorkspaceRoots[uri] = path
	}
	return copy.take(ctx, id, seen, false)
}

// OpenBranch opens kept turns through the continuation door, under the
// branch's own id and folder. It never takes or restores the main chat.
func (c *Continuer) OpenBranch(ctx context.Context, id string) (Continued, error) {
	return c.take(ctx, id, nil, true)
}

// RetryFiles retries a failed background workspace fetch for chat id (C4).
func (c *Continuer) RetryFiles(id string) bool {
	if c == nil || c.opt.RootFor == nil {
		return false
	}
	root := c.opt.RootFor(id)
	if a := cell.LookupWorkspaceArrival(root, workspaceOf(root)); a != nil {
		return a.Retry()
	}
	return false
}

// ErrNothingToRestart is what a restart answers when the chat has no abandoned
// arrival any more: the files landed, or a live owner has them in flight.
var ErrNothingToRestart = errors.New("this chat's files have no abandoned arrival to restart")

// RestartFiles is what OPENING A CHAT THIS COMPUTER ALREADY HOLDS does about
// files that failed to arrive. A chat that moved here opens before its files do;
// if the fetch fails and the window that was fetching them closes, the gate stays
// shut for every tool and every save, and the refusal says to continue the chat
// here again, which for a chat this computer holds there is nothing to press for.
// So the open is the restart: this starts the very same background fetch a take
// starts, through the same code, with the same rules for the person's local
// files, and answers once the files are on their way, not when they have landed.
//
// IT IS THE ONE PLACE EVERY DOOR THAT OPENS A SAVED CHAT ASKS: the app's open,
// the terminal's local open and its launch. A door calls it BEFORE it opens the
// conversation, because the take it runs asks a window holding the chat's journal
// to let go, and a conversation opened first would be that window.
//
// It restarts only an ABANDONED failure (the files did not arrive and the owner
// that was fetching them is gone), and only for a chat whose lease and send
// receipt are this computer's, or whose lease still names this computer after
// its hold ended (released at a quit, or lapsed): it claims, fences and releases
// nothing. It answers false, with no error, for a chat with no such failure
// (nothing is read from the relay then), for one a live owner still has in flight
// (a second arrival would race it), and for one another computer holds or that
// this one cannot prove is the shared head (the refusal stands, because a fetch
// there would set the local copy aside and seal the partial tree as edits). An
// error means the restart was tried and could not start; the failure then stays
// as it was.
func (c *Continuer) RestartFiles(ctx context.Context, id string) (restarted bool, err error) {
	if c == nil || c.opt.RootFor == nil {
		return false, nil
	}
	if cell.AbandonedArrival(c.opt.RootFor(id)) == nil {
		return false, nil
	}
	only := *c
	only.restartOnly = true
	if _, err := only.take(ctx, id, nil, false); err != nil {
		if errors.Is(err, ErrNothingToRestart) || errors.Is(err, cell.ErrWorkspaceArrivalAbandoned) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *Continuer) take(ctx context.Context, id string, seen *directory.LeaseExpectation, branchOnly bool) (out Continued, err error) {
	// A computer that serves this chat's frames must be able to answer for them
	// once it has taken the chat, and a take that cannot find a chat's frames
	// says why in words when live transfer is the reason.
	c.sync.ensureFilesDoor()
	defer func() { err = c.sync.liveTransferRefusal(err) }()
	// Measure one attempt on its own copy: observers must not leak into another
	// continue, and diagnostics must survive both early refusal and late failure.
	started := time.Now()
	type openStep struct {
		name   string
		dur    time.Duration
		detail string
	}
	var phaseMu sync.Mutex
	phases := map[string]time.Duration{}
	var openSteps []openStep
	stepCursor := started
	var shownElapsed, filesElapsed time.Duration
	var openGets int64
	var openRounds int
	recordStep := func(name, detail string) {
		now := time.Now()
		phaseMu.Lock()
		if shownElapsed == 0 {
			dur := now.Sub(stepCursor)
			stepCursor = now
			openSteps = append(openSteps, openStep{name: name, dur: dur, detail: detail})
		}
		phaseMu.Unlock()
	}
	formatOpenPath := func(steps []openStep) string {
		if len(steps) == 0 {
			return "none"
		}
		var b strings.Builder
		for i, s := range steps {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(s.name)
			b.WriteByte(':')
			b.WriteString(s.dur.String())
			b.WriteString(s.detail)
		}
		return b.String()
	}
	loggedPhases := false
	logPhases := func(outcome string) {
		phaseMu.Lock()
		if loggedPhases {
			phaseMu.Unlock()
			return
		}
		loggedPhases = true
		shown, files := shownElapsed, filesElapsed
		getsSnap, roundsSnap := openGets, openRounds
		stepsSnap := append([]openStep(nil), openSteps...)
		snap := make(map[string]time.Duration, len(phases))
		for k, v := range phases {
			snap[k] = v
		}
		phaseMu.Unlock()
		log.Printf("continue %s phases capture=%s directory=%s retirement=%s fetch=%s acquire=%s restore=%s hooks=%s filing=%s unpublished=%s dirty=%s kept_upload=%s admission=%s open_seed=%s open_gets=%d open_rounds=%d open_path=%s shown=%s files=%s total=%s outcome=%s", id,
			snap["local capture"], snap["directory"], snap["retirement"], snap["fetch"], snap["acquire"], snap["restore"], snap["hooks"], snap["filing"], snap["unpublished check"], snap["dirty check"], snap["kept-branch upload"], snap["staging/preflight"], snap["open seed"], getsSnap, roundsSnap, formatOpenPath(stepsSnap), shown, files, time.Since(started), outcome)
	}
	parent := ctx
	moveCtx, cancelMove := context.WithCancelCause(context.WithoutCancel(parent))
	var bgMuWatch sync.Mutex
	bgActive := false
	bgDetached := false
	go func() {
		select {
		case <-parent.Done():
			bgMuWatch.Lock()
			detached := bgDetached
			bgMuWatch.Unlock()
			if !detached {
				cancelMove(context.Cause(parent))
			}
		case <-moveCtx.Done():
		}
	}()
	ctx, watch, stopWatch := watchMove(moveCtx, c.opt.OnMove, c.opt.IdleWithin)
	defer func() {
		bgMuWatch.Lock()
		active := bgActive
		bgMuWatch.Unlock()
		if !active {
			stopWatch()
			cancelMove(nil)
		}
	}()
	defer func() {
		if err != nil && context.Cause(ctx) == ErrMoveStalled && parent.Err() == nil {
			err = fmt.Errorf("%w: %w", ErrMoveStalled, err)
		}
	}()
	sc := c.sync.chatScope(id)
	settleSync := true
	defer func() {
		if settleSync {
			defer handoff.Trace(c.opt.OnPhase, "filing")()
			sc.Settle()
		}
	}()
	var fetcher *cellsync.Fetcher
	var lastFetchRounds, lastFetchGets int
	observed := *c
	report := c.opt.OnPhase
	observed.opt.OnPhase = func(p handoff.Phase) {
		if !p.Ended.IsZero() {
			phaseMu.Lock()
			phases[p.Name] += p.Ended.Sub(p.Started)
			isBeforeShown := shownElapsed == 0
			phaseMu.Unlock()
			if isBeforeShown {
				switch p.Name {
				case "unpublished check":
					recordStep("unpublished", "")
				case "dirty check":
					recordStep("dirty", "")
				case "kept-branch upload":
					recordStep("kept_upload", "")
				case "open seed":
					recordStep("open_seed", "")
				case "fetch":
					rNow := 0
					if fetcher != nil {
						rNow = fetcher.OpenRounds()
					}
					gNow := int(sc.Gets())
					rDelta := max(rNow-lastFetchRounds, 0)
					gDelta := max(gNow-lastFetchGets, 0)
					lastFetchRounds, lastFetchGets = rNow, gNow
					recordStep("fetch", fmt.Sprintf("(r%d,g%d)", rDelta, gDelta))
				case "acquire":
					recordStep("acquire", "")
				}
			}
		}
		watch.phase(p)
		if report != nil {
			report(p)
		}
	}
	c = &observed
	defer func() {
		if bgActive {
			return
		}
		outcome := "done"
		if err != nil {
			outcome = fmt.Sprintf("failed reason=%q", err.Error())
		}
		logPhases(outcome)
	}()
	dir := &countingDir{Client: c.sync.Dir}
	root := c.opt.RootFor(id)
	_, localRootErr := cell.OpenAt(root, id)
	keepRoot := localRootErr == nil
	wasWarm := c.holds(id, root)
	retirer := newHolderRetirer(root)
	defer func() {
		if !bgActive {
			retirer.cancel()
		}
	}()
	// Ask a local reader to let go immediately at the keypress when this device
	// does not hold an active send lease, overlapping retirement with Dir.Cell.
	//
	// ONLY A WINDOW THAT IS JUST OPEN IS ASKED THIS EARLY. At the keypress the take
	// has not been allowed yet: the card may be stale, the relay unreachable, the
	// chat held by somebody else now, and a refused take must leave a window that
	// is working here working. So a window mid-turn, one waiting on its person, or
	// one that cannot say what it is doing is asked later, by retire(), once the
	// refusals that need only the directory's record are behind us, exactly where
	// the old road asked. A refusal withdraws whatever was asked (retirer.cancel,
	// deferred above). With CODEAF_MOVE_FILES_FIRST=1 nothing is asked early at all:
	// that switch is the old road entire.
	if keepRoot && !MoveFilesFirst() && (seen == nil || seen.Device != c.sync.Device.ID()) {
		if _, fence, lErr := c.sync.SendLease(id); (lErr != nil || fence == 0) && retirer.isIdleReader() {
			retireDone := handoff.Trace(c.opt.OnPhase, "retirement")
			retirer.ask()
			retireDone()
		}
	}
	preKept := ""
	if branches, err := cellsync.OpenBranchMap(c.sync.Home); err == nil {
		if resolved := branches.Resolve(id); resolved != id {
			preKept = resolved
		}
	}
	arriveRootFence := handoff.Arrive(root)
	defer func() {
		if !bgActive && arriveRootFence != nil {
			arriveRootFence()
		}
	}()
	localCapture := handoff.Trace(c.opt.OnPhase, "local capture")
	// A FAILED ARRIVAL WHOSE WINDOW CLOSED IS NOT A REFUSAL HERE, IT IS THE RESTART.
	// The files of this chat did not arrive and nothing is left to retry them, so
	// every seal and tool in every process refuses. This take is the process that
	// fetches them again: it carries on, but it never seals or captures the
	// partial tree (see the restart below), and it never forgets the failure
	// unless the files land.
	var abandonedErr error
	if c.restartOnly {
		// AN OPEN THAT FOUND A FAILURE DOES NOT WAIT FOR ONE. Between its look and
		// this one the failure may have been replaced by a live owner's arrival or
		// by files that landed, and then there is nothing to restart: it must neither
		// stand behind that arrival nor go on as an ordinary take, which could claim
		// a chat the open never meant to.
		if abandonedErr = cell.AbandonedArrival(root); abandonedErr == nil {
			localCapture()
			return Continued{}, ErrNothingToRestart
		}
	} else if err := cell.WaitWorkspace(ctx, root); err != nil {
		if !errors.Is(err, cell.ErrWorkspaceArrivalAbandoned) {
			localCapture()
			return Continued{}, err
		}
		abandonedErr = err
	}
	if err := cellstore.Await(ctx, cell.Cell{Root: root}); err != nil {
		localCapture()
		return Continued{}, err
	}
	localCapture()
	recordStep("capture", "")
	callerSeen := seen
	localCache := &localWorkCache{eng: c.eng}
	var view directory.CellView
	supplied, explicitSupplied, hasSupplied := c.sync.takeCellView(id)
	if MoveFilesFirst() {
		// The old road reads the directory itself, in series. A record the list
		// supplied or cached (consumed above, so it cannot serve a later take
		// either) is never trusted in its place.
		hasSupplied = false
	}
	if hasSupplied && !explicitSupplied {
		lease := supplied.Cell.Lease
		eligible := supplied.Cell.Head != "" && !supplied.Cell.Archived && !c.sync.ChatStorageOff()
		if branchOnly {
			eligible = eligible && supplied.Cell.ParentCell != ""
		} else {
			eligible = eligible && supplied.Cell.ParentCell == ""
		}
		if c.freeOnly && lease.Expires != 0 {
			eligible = false
		}
		if callerSeen != nil && !callerSeen.Matches(supplied.Cell) {
			eligible = false
		}
		if lease.Device == c.sync.Device.ID() && lease.Expires > supplied.Now {
			eligible = false
		}
		if eligible && keepRoot {
			if unshared, _, uErr := cellstore.Unpublished(cell.Cell{ID: id, Root: root}, supplied.Cell.Head); uErr != nil || unshared != "" {
				eligible = false
			}
		}
		if !eligible {
			hasSupplied = false
		}
	}
	if hasSupplied {
		view = supplied
	} else {
		if keepRoot && (MoveFilesFirst() || branchOnly || c.sync.ChatStorageOff() || !c.eng.HasComposedCellDir(cell.Cell{ID: id, Root: root})) {
			localCache.start(ctx, cell.Cell{ID: id, Root: root})
		}
		directoryRead := handoff.Trace(c.opt.OnPhase, "directory")
		readCtx := ctx
		cancelRead := func() {}
		if c.freeOnly {
			readCtx, cancelRead = context.WithTimeout(ctx, chatlist.OpenReadTimeout)
		}
		view, err = dir.Cell(readCtx, id)
		cancelRead()
		directoryRead()
		if err != nil {
			if c.freeOnly && errors.Is(err, context.DeadlineExceeded) {
				err = fmt.Errorf("%w: %w", directory.ErrUnreachable, err)
			}
			return Continued{}, err
		}
	}
	if err := c.sync.reconcileSendLease(id, id, view); err != nil {
		return Continued{}, err
	}
	if branchOnly && (view.Cell.ParentCell == "" || view.Cell.Archived) {
		return Continued{}, errors.New("these turns are no longer available to open separately")
	}
	lease := view.Cell.Lease
	// A live store asks the computer that held the chat when this take read its
	// record; once the take acquires it, the record names this computer instead.
	sc.Holder(lease.Device)
	if c.freeOnly && lease.Expires != 0 {
		return Continued{}, directory.ErrLeaseHeld
	}
	// Kept turns may name this device before their separate copy exists here.
	known, err := cellstore.KnowsHead(cell.Cell{ID: id, Root: root}, view.Cell.Head)
	if err != nil {
		return Continued{}, err
	}
	heldHere := !branchOnly && known && view.Cell.ParentCell == "" && lease.Device == c.sync.Device.ID() && lease.Expires > view.Now
	if heldHere {
		// A revived own-device fence cannot restore a retired writer. Only
		// its matching send receipt allows the active-session shortcut.
		remote, fence, err := c.sync.SendLease(id)
		if err != nil && !os.IsNotExist(err) {
			return Continued{}, err
		}
		heldHere = err == nil && remote == id && fence == lease.Fence
	}
	// RESTARTING THE FILES OF A CHAT THIS COMPUTER HOLDS. The lease is this
	// computer's own (lapsed or not: the holder may only be late with a heartbeat)
	// and its send receipt still names the fence, so nothing has to be claimed or
	// swapped: the conversation here is the record at the head, and only the
	// workspace is missing files. The take goes on to the background stage that
	// fetches them, and never answers AlreadyHere without fetching.
	restart := false
	if abandonedErr != nil {
		restart = !branchOnly && known && view.Cell.ParentCell == "" && lease.Device == c.sync.Device.ID()
		if restart {
			remote, fence, rErr := c.sync.SendLease(id)
			restart = rErr == nil && remote == id && fence == lease.Fence
			if !restart && c.restartOnly && lease.Expires <= view.Now {
				// AN OPEN FINDS THE HOLD ENDED, NOT LIVE. A quit releases the lease and
				// forgets the send receipt, and the read above forgets it for a lease that
				// lapsed, so the chat a person opens the next morning has no receipt at
				// all, yet the lease still names this computer: nobody has taken the chat
				// since. The restart claims nothing, so there is no fence to revive and no
				// writer to restore, and the conversation that opens afterwards takes the
				// hold up the ordinary way. A LIVE lease without its receipt stays refused.
				restart = true
			}
		}
		if !restart {
			// Another computer holds the chat now, or this one was retired, or the
			// head here is not the shared one. A normal take would set the local
			// copy aside and seal its "edits", which is the partial tree, so the
			// refusal stands until the chat is whole again.
			return Continued{}, abandonedErr
		}
	}
	if heldHere && !restart {
		opened, err := cell.OpenAt(root, id)
		if err != nil {
			return Continued{}, err
		}
		// This computer held the chat before the take began, possibly for a session
		// that is running now, so a failed record keeps that hold rather than
		// releasing a lease this call never acquired.
		if err := c.sync.recordSendLease(opened.ID, sendLease{Cell: id, Fence: lease.Fence}); err != nil {
			return Continued{}, err
		}
		if err := c.restoreTakenIdentity(ctx, opened); err != nil {
			return Continued{}, err
		}
		return Continued{AlreadyHere: true, LocalOnly: c.sync.ChatStorageOff(), Device: c.opt.DeviceName, Taken: handoff.Taken{
			Cell: opened, Driving: cellsync.Driving{Cell: opened, Fence: lease.Fence, Head: view.Cell.Head, Frames: view.Cell.Frames, Size: view.Cell.Size},
		}}, nil
	}
	if seen != nil && !seen.Matches(view.Cell) {
		return Continued{}, directory.ErrLeaseHeld
	}
	if seen == nil {
		seen = &directory.LeaseExpectation{Device: lease.Device, Fence: lease.Fence}
	}
	if watch != nil && view.Cell.Size > 0 && !wasWarm {
		watch.expect(int64(view.Cell.Size))
	}
	var from string
	var fromResolved bool
	var fromMu sync.Mutex
	switch {
	case lease.Device == "" || lease.Device == c.sync.Device.ID():
		from, fromResolved = "", true
	case c.opt.FromDevice != "":
		from, fromResolved = c.opt.FromDevice, true
	default:
		if cached, ok := c.sync.cachedDeviceName(lease.Device); ok {
			from, fromResolved = cached, true
		}
	}
	resolveFrom := func() string {
		fromMu.Lock()
		defer fromMu.Unlock()
		if !fromResolved {
			from = c.sync.deviceName(lease.Device)
			fromResolved = true
		}
		return from
	}
	if MoveFilesFirst() {
		// The old road names the other computer before it fetches, in series with
		// the rest; the fast road leaves that lookup for after the chat is open.
		resolveFrom()
	}
	storageOff := c.sync.ChatStorageOff()
	// An own free chat can return without replacing its open local conversation.
	keepOpen := storageOff && lease.Device == c.sync.Device.ID() && lease.Expires <= view.Now
	filesFirst := (MoveFilesFirst() || branchOnly || view.Cell.ParentCell != "") && !restart
	wantAsync := !filesFirst
	fastLocalOnly := !filesFirst && !storageOff && c.eng.HasComposedCellDir(cell.Cell{ID: id, Root: root})
	var unknownClean bool
	alreadyBranched := ""
	if preKept != "" && MoveFilesFirst() {
		// The old road always reads the kept branch's record, never a cached one.
		if kv, err := dir.Cell(ctx, preKept); err == nil && kv.Cell.ParentCell == id {
			alreadyBranched = kv.Cell.Head
		}
	} else if preKept != "" && keepRoot {
		if unshared, _, uErr := cellstore.Unpublished(cell.Cell{ID: id, Root: root}, view.Cell.Head); uErr == nil && unshared != "" {
			if kv, ok := c.sync.CachedCellView(preKept); ok && kv.Cell.ParentCell == id {
				alreadyBranched = kv.Cell.Head
			} else if fastLocalOnly {
				alreadyBranched = unshared
				unknownClean = true
			} else if kv, err := dir.Cell(ctx, preKept); err == nil && kv.Cell.ParentCell == id {
				alreadyBranched = kv.Cell.Head
			}
		}
	}
	recordStep("directory", "")
	retire := func() {
		defer handoff.Trace(c.opt.OnPhase, "retirement")()
		retirer.ask()
		if !wantAsync || retirer.isWorkingWriter() {
			retirer.wait(ctx, releaseWithin)
			recordStep("retire_wait", "")
		}
	}
	if !keepOpen {
		retire()
	}
	var openRetireWait func(context.Context)
	if filesFirst || storageOff {
		openRetireWait = func(wCtx context.Context) {
			defer handoff.Trace(c.opt.OnPhase, "retirement")()
			retirer.wait(wCtx, releaseWithin)
			recordStep("retire_wait", "")
		}
	}
	localWork := engineLocal{
		eng:             c.eng,
		ctx:             ctx,
		alreadyBranched: alreadyBranched,
		cache:           localCache,
		fastOnly:        fastLocalOnly,
		unknownClean:    &unknownClean,
		retireWait:      openRetireWait,
		deferWhileInUse: func() bool {
			return !filesFirst && !storageOff && !retirer.isDone()
		},
	}
	if storageOff && !restart {
		local := cell.Cell{ID: id, Root: root}
		if _, err := os.Stat(root); err == nil {
			head, _, err := localWork.Unpublished(local, view.Cell.Head)
			if err != nil {
				return Continued{}, err
			}
			dirty, err := localWork.Dirty(ctx, local)
			if err != nil {
				return Continued{}, err
			}
			// An own free chat whose shared turn is here can keep its live reader;
			// an absent or clean older copy still reads the recorded history normally.
			if head != "" || dirty || keepOpen && known {
				return c.takeLocalStorageOff(ctx, id, root, view, seen)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return Continued{}, err
		}
	}
	if keepOpen {
		// A read-only fetch still replaces files, so it must retire their live reader.
		retire()
	}
	recordStep("retire", "")
	intendedPrimary := session.Place{Dir: root, Owned: true}.Work()
	// WHERE THE CHAT WORKS IS READ NOW, FROM THIS COMPUTER'S OWN RECORD, because the
	// record the open swaps in is the head's, and a head the other computer sealed
	// as a copy of its own says "owned" whatever this computer's folder holds.
	// InstallState asks this answer, not the record in front of it.
	project, worksInPlace := projectOf(root)
	if worksInPlace {
		intendedPrimary = project
	}
	taker := c.takerWithDir(ctx, sc, dir)
	taker.View = &view
	fetcher, _ = taker.Fetch.(*cellsync.Fetcher)
	if fetcher != nil {
		fetcher.View = &view
		if l, ok := fetcher.Engine.(landing); ok {
			l.warm = map[string]bool{id: wasWarm}
			fetcher.Engine = l
		}
	}
	taker.Local = localWork
	var branchedTurns uint32
	origBranch := taker.Branch
	taker.Branch = func(ctx context.Context, from *cellsync.Driving, head string, turns uint32) (string, error) {
		if opened, openErr := cell.OpenAt(from.Cell.Root, from.Cell.ID); openErr == nil {
			if _, t, uErr := cellstore.Unpublished(opened, from.Head); uErr == nil {
				branchedTurns = t
			}
		}
		if branchedTurns == 0 {
			branchedTurns = turns
		}
		return origBranch(ctx, from, head, turns)
	}
	preflightCheck := func(_ context.Context, staged cell.Cell) error {
		place := session.Place{Dir: staged.Root, Workspace: intendedPrimary}
		plan, err := session.TaskWorkspaceTransferPlan(place)
		if err != nil {
			return err
		}
		// Independent branches extract settled results in branchIdentity; they
		// never inherit the original chat's executable roots or source authority.
		if view.Cell.ParentCell != "" {
			return nil
		}
		if plan == nil {
			return session.ValidateTaskRestoreRoots(place, intendedPrimary)
		}
		if !session.WorkspaceOwnershipEnabled(place) {
			return errors.New("this chat needs supported private task placement on the receiving device")
		}
		return session.ValidateTaskWorkspaceSetBindingsForDestination(place, root, c.opt.WorkspaceRoots)
	}
	var stageComplete bool
	var preRoot string
	preWorkspace := keptFolderOf(root)
	stallBound := func() time.Duration {
		if c.opt.IdleWithin > 0 {
			return c.opt.IdleWithin
		}
		return MoveIdleWithin
	}
	var earlyArrival *cell.WorkspaceArrival
	defer func() {
		if !bgActive {
			if earlyArrival != nil {
				if restart {
					// A replacement arrival over an abandoned failure that could not
					// go on has NOT brought the files: Complete would declare them here.
					// The failure goes back as it was, for the next take to restart.
					reason, _ := cell.AbandonedReason(abandonedErr)
					earlyArrival.Abandon(reason)
				} else {
					earlyArrival.Complete()
				}
			}
			if preRoot != "" {
				_ = os.RemoveAll(preRoot)
			}
		}
	}()
	var revalidated bool
	if filesFirst {
		taker.Prepare = func(ctx context.Context, staged cell.Cell, head string) error {
			if err := c.eng.StageAdmission(ctx, staged, head); err != nil {
				return err
			}
			needed, err := session.TaskWorkspaceAdmissionNeeded(session.Place{Dir: staged.Root, Workspace: intendedPrimary})
			if err != nil || !needed {
				return err
			}
			if err := c.eng.StageWorkspace(ctx, staged, head); err != nil {
				return err
			}
			return preflightCheck(ctx, staged)
		}
		if view.Cell.ParentCell != "" {
			// The branch's snapshot was sealed before it had its own id. Rebind
			// its journal before rebuilding the row, so two chats never share an id.
			taker.After = append([]func(context.Context, cell.Cell) error{branchIdentity}, taker.After...)
		}
		if worksInPlace {
			// Ownership of this computer's project folder is local. A snapshot
			// arriving from an owned copy must not turn an in-place return into
			// tools pointed at a different, empty work/ directory.
			taker.After = append([]func(context.Context, cell.Cell) error{func(_ context.Context, at cell.Cell) error {
				meta, err := session.LoadMeta(at.Root)
				if err != nil {
					return err
				}
				meta.Workspace, meta.Owned, meta.Origin = project, false, ""
				return session.SaveMeta(at.Root, meta)
			}}, taker.After...)
		}
		// Restoration has acquired the requested chat and retired its old writer.
		// Persist its identity before any hook resolves the vault's chat scope.
		taker.After = append([]func(context.Context, cell.Cell) error{c.restoreTakenIdentity}, taker.After...)
	} else {
		taker.StagePrepare = func(ctx context.Context, staged cell.Cell, head string) error {
			stageState := c.eng.StageState
			if fetcher != nil {
				if ss, ok := fetcher.Engine.(interface {
					StageState(context.Context, cell.Cell, string) error
				}); ok {
					stageState = ss.StageState
				}
				var err error
				stageComplete, err = fetcher.StageChat(ctx, staged, head, stageState)
				if err != nil {
					return err
				}
			} else {
				if err := taker.Fetch.Complete(ctx, staged, head); err != nil {
					return err
				}
				if err := stageState(ctx, staged, head); err != nil {
					return err
				}
				stageComplete = true
			}
			needed, err := session.TaskWorkspaceAdmissionNeeded(session.Place{Dir: staged.Root, Workspace: intendedPrimary})
			if err != nil {
				return err
			}
			if needed {
				if !stageComplete {
					if err := taker.Fetch.Complete(ctx, staged, head); err != nil {
						return err
					}
					stageComplete = true
				}
				if err := c.eng.StageWorkspace(ctx, staged, head); err != nil {
					return err
				}
			}
			return preflightCheck(ctx, staged)
		}
		taker.InstallState = func(_ context.Context, staged, target cell.Cell, head string) error {
			if err := os.MkdirAll(target.Root, 0o700); err != nil {
				return err
			}
			if keepRoot && (unknownClean || !retirer.isDone()) && preRoot == "" {
				// A COPY HERE THAT CANNOT BE SET ASIDE IS NOT REPLACED. The check for
				// edits it holds runs against that set-aside copy, so without it the
				// incoming state would go in with the edits unchecked and unkept.
				saved, saveErr := savePreTakeState(target.Root, preWorkspace, !retirer.isDone(), retirer)
				if saveErr != nil {
					log.Printf("continue %s could not set the older copy aside, so nothing is replaced: %v", id, saveErr)
					return fmt.Errorf("could not set this computer's older copy of the chat aside to check it for unsaved edits, so nothing was replaced (%w)", saveErr)
				}
				preRoot = saved
			}
			if earlyArrival == nil {
				earlyArrival = cell.BeginWorkspaceArrival(target.Root, workspaceOf(target.Root), stallBound, nil)
				if target.Root != root {
					earlyArrival.BindPaths(root)
				}
			}
			carried := preserveExistingSubdirs(filepath.Join(target.Root, cell.StateDir), filepath.Join(staged.Root, cell.StateDir))
			if err := handoff.Swap(filepath.Join(staged.Root, cell.StateDir), filepath.Join(target.Root, cell.StateDir)); err != nil {
				return err
			}
			// What was carried over is the OLDER copy of those records until the
			// restore that follows replaces it with the head's; the restore needs
			// to know which files are still exactly that copy.
			if err := c.eng.NoteCarried(target, carried); err != nil {
				return err
			}
			if !worksInPlace {
				installOpenInstructions(staged.Root, target.Root)
			}
			if err := cellstore.NoteAdopted(target, head); err != nil {
				return err
			}
			return cellstore.NotePublished(target, head)
		}
		canConcurrent := (hasSupplied || c.opt.OnShown != nil || c.opt.AsyncFiles) && len(c.opt.WorkspaceRoots) == 0
		// BLIND PASS-THROUGH ASKS FOR THE LEASE AFTER THE FRAMES, NEVER BESIDE THEM.
		// Here the fetch can fail for a reason that is common and not an outage: the
		// computer that has the chat is off, and nothing else holds its frames. An
		// acquire sent beside that fetch lands, and handing the lease back after the
		// failure leaves THIS computer named as the chat's last holder, so the real
		// holder's next launch is offered a move from a computer that never had the
		// chat. Fetching first costs one more directory request when the holder is
		// on, and a take that fails leaves the record exactly as it was. The road
		// with approved sync storage is unchanged: its frames come from the relay,
		// and a failed fetch there is a real fault.
		if c.sync.LiveTransfer() {
			canConcurrent = false
		}
		if canConcurrent && keepRoot {
			if needed, nErr := session.TaskWorkspaceAdmissionNeeded(session.Place{Dir: root, Workspace: intendedPrimary}); nErr != nil || needed {
				canConcurrent = false
			}
		}
		taker.ConcurrentClaim = canConcurrent
		taker.Revalidate = func(rCtx context.Context) (directory.CellView, error) {
			revalidated = true
			directoryRead := handoff.Trace(c.opt.OnPhase, "directory")
			readCtx := rCtx
			cancelRead := func() {}
			if c.freeOnly {
				readCtx, cancelRead = context.WithTimeout(rCtx, chatlist.OpenReadTimeout)
			}
			freshView, rErr := dir.Cell(readCtx, id)
			cancelRead()
			directoryRead()
			recordStep("directory", "")
			if rErr != nil {
				if c.freeOnly && errors.Is(rErr, context.DeadlineExceeded) {
					rErr = fmt.Errorf("%w: %w", directory.ErrUnreachable, rErr)
				}
				return directory.CellView{}, rErr
			}
			if err := c.sync.reconcileSendLease(id, id, freshView); err != nil {
				return directory.CellView{}, err
			}
			if freshView.Cell.Archived || freshView.Cell.ParentCell != "" {
				return directory.CellView{}, directory.ErrLeaseHeld
			}
			if c.freeOnly && freshView.Cell.Lease.Expires != 0 {
				return directory.CellView{}, directory.ErrLeaseHeld
			}
			if callerSeen != nil && !callerSeen.Matches(freshView.Cell) {
				return directory.CellView{}, directory.ErrLeaseHeld
			}
			view = freshView
			taker.View = &view
			if fetcher != nil {
				fetcher.View = &view
			}
			lease = freshView.Cell.Lease
			fromMu.Lock()
			switch {
			case lease.Device == "" || lease.Device == c.sync.Device.ID():
				from, fromResolved = "", true
			case c.opt.FromDevice != "":
				from, fromResolved = c.opt.FromDevice, true
			default:
				if cached, ok := c.sync.cachedDeviceName(lease.Device); ok {
					from, fromResolved = cached, true
				} else {
					from, fromResolved = "", false
				}
			}
			fromMu.Unlock()
			return freshView, nil
		}
		after := []func(context.Context, cell.Cell) error{c.restoreTakenIdentity}
		if view.Cell.ParentCell != "" {
			after = append(after, branchIdentity)
		}
		after = append(after, seedMeta)
		if worksInPlace {
			after = append(after, func(_ context.Context, at cell.Cell) error {
				meta, err := session.LoadMeta(at.Root)
				if err != nil {
					return err
				}
				meta.Workspace, meta.Owned, meta.Origin = project, false, ""
				return session.SaveMeta(at.Root, meta)
			})
		}
		after = append(after, recordOrigin)
		taker.After = after
	}
	var taken handoff.Taken
	// giveBackTaken hands the lease back after a failed open, but only a lease
	// this take acquired. A restart acquired nothing: the hold was this
	// computer's before the take began, and a failure of the refetch must not
	// release it.
	giveBackTaken := func() error {
		if restart {
			return nil
		}
		return c.giveBack(ctx, id, taken.Driving.Fence)
	}
	if restart {
		opened, openErr := cell.OpenAt(root, id)
		if openErr != nil {
			return Continued{}, openErr
		}
		if err := c.restoreTakenIdentity(ctx, opened); err != nil {
			return Continued{}, err
		}
		taken = handoff.Taken{Cell: opened, Driving: cellsync.Driving{Cell: opened, Fence: lease.Fence, Head: view.Cell.Head, Frames: view.Cell.Frames, Size: view.Cell.Size}}
		// THE NEW ARRIVAL REPLACES THE STALE FAILED MARKER, and from here the
		// waiters in every process wait on it. The background stage below is the
		// ordinary one: fetch, restore the workspace to the head, run the hooks, and
		// only then Complete.
		earlyArrival = cell.BeginWorkspaceArrival(opened.Root, workspaceOf(opened.Root), stallBound, nil)
		if opened.Root != root {
			earlyArrival.BindPaths(root)
		}
	} else {
		var takeErr error
		taken, takeErr = taker.TakeSeen(ctx, id, seen)
		if takeErr != nil {
			return Continued{}, takeErr
		}
	}
	place := session.Place{Dir: taken.Cell.Root, Workspace: intendedPrimary}
	workspacePlan, err := session.TaskWorkspaceTransferPlan(place)
	if err != nil {
		return Continued{}, errors.Join(err, giveBackTaken())
	}
	if workspacePlan != nil {
		if !session.WorkspaceOwnershipEnabled(place) {
			err = errors.New("this chat needs supported private task placement on the receiving device")
		} else {
			err = session.ValidateTaskWorkspaceSetBindings(place, c.opt.WorkspaceRoots)
		}
	} else {
		err = session.ValidateTaskRestoreRoots(place, intendedPrimary)
	}
	if err != nil {
		return Continued{}, errors.Join(err, giveBackTaken())
	}
	if filesFirst {
		defer handoff.Trace(c.opt.OnPhase, "filing")()
		if err := cellstore.AdoptedFrom(taken.Cell, resolveFrom()); err != nil {
			return Continued{}, errors.Join(err, giveBackTaken())
		}
		if err := cellstore.NotePublished(taken.Cell, taken.Driving.Head); err != nil {
			return Continued{}, errors.Join(err, giveBackTaken())
		}
		recordStep("install_state", "")
		var filed func()
		defer func() {
			if filed != nil {
				filed()
			}
		}()
		if taken.Cell, filed, err = c.fileCopy(ctx, taken.Cell, keepRoot); err != nil {
			return Continued{}, errors.Join(err, giveBackTaken())
		}
		taken.Driving.Cell = taken.Cell
		recordStep("file_copy", "")
		var taskCopies []string
		if workspacePlan != nil {
			taskCopies, err = session.RestoreTaskWorkspaceSets(ctx, session.Place{Dir: taken.Cell.Root, Workspace: workspaceOf(taken.Cell.Root)}, c.opt.WorkspaceRoots)
			if err != nil {
				return Continued{}, errors.Join(err, giveBackTaken())
			}
		} else {
			if err := session.AdoptChatWorkspaceIdentity(session.Place{Dir: taken.Cell.Root}); err != nil {
				return Continued{}, errors.Join(err, giveBackTaken())
			}
			taskCopies = c.restoreCopies(taken.Cell)
		}
		recordStep("adopt", "")
		if err := c.sync.recordSendLease(taken.Cell.ID, sendLease{Cell: id, Fence: taken.Driving.Fence}); err != nil {
			return Continued{}, errors.Join(err, giveBackTaken())
		}
		if c.sync.teamsSharing() {
			if err := (teamKeys{c.sync.profile(), c.sync.Home, c.sync.Device.ID(), nil}).rebindChat(id, taken.Cell); err != nil {
				c.say(taken.Cell, "This chat moved here, but its teams could not be updated yet: "+err.Error())
			}
		}
		recordStep("send_lease", "")
		res := c.arrive(taken.Cell, resolveFrom())
		recordStep("fast_resume", "")
		phaseMu.Lock()
		shownElapsed = stepCursor.Sub(started)
		filesElapsed = shownElapsed
		openGets = sc.Gets()
		openRounds = dir.Count()
		if fetcher != nil {
			openRounds += fetcher.OpenRounds()
		}
		phaseMu.Unlock()
		out = Continued{LocalOnly: storageOff, Taken: taken, Device: c.opt.DeviceName, TaskCopies: taskCopies, Resume: res, Shown: shownElapsed, Files: filesElapsed, Wait: func(context.Context) error { return nil }, Retry: func() bool { return false }}
		c.eng.RecordMaterializedStat(taken.Cell, taken.Driving.Head)
		if taken.Kept != "" && taken.Kept != preKept {
			if branchedTurns != 0 {
				out.KeptTurns = branchedTurns
			} else {
				out.KeptTurns = c.orphanTurns(ctx, taken.Kept)
			}
		}
		if c.opt.OnShown != nil {
			c.opt.OnShown(out)
		}
		return out, nil
	}

	fromMu.Lock()
	fastFrom := from
	fromMu.Unlock()
	if err := cellstore.AdoptedFrom(taken.Cell, fastFrom); err != nil {
		return Continued{}, errors.Join(err, giveBackTaken())
	}
	if err := cellstore.NotePublished(taken.Cell, taken.Driving.Head); err != nil {
		return Continued{}, errors.Join(err, giveBackTaken())
	}
	recordStep("install_state", "")
	var filed func()
	if taken.Cell, filed, err = c.fileCopy(ctx, taken.Cell, keepRoot); err != nil {
		if filed != nil {
			filed()
		}
		return Continued{}, errors.Join(err, giveBackTaken())
	}
	taken.Driving.Cell = taken.Cell
	recordStep("file_copy", "")
	if err := os.MkdirAll(workspaceOf(taken.Cell.Root), 0o700); err != nil {
		if filed != nil {
			filed()
		}
		return Continued{}, errors.Join(err, giveBackTaken())
	}
	if workspacePlan == nil {
		if err := session.AdoptChatWorkspaceIdentity(session.Place{Dir: taken.Cell.Root}); err != nil {
			if filed != nil {
				filed()
			}
			return Continued{}, errors.Join(err, giveBackTaken())
		}
	}
	recordStep("adopt", "")
	if err := c.sync.recordSendLease(taken.Cell.ID, sendLease{Cell: id, Fence: taken.Driving.Fence}); err != nil {
		if filed != nil {
			filed()
		}
		return Continued{}, errors.Join(err, giveBackTaken())
	}
	recordStep("send_lease", "")
	var keptTurns uint32
	if taken.Kept != "" && taken.Kept != preKept {
		keptTurns = branchedTurns
	}
	initialResume, initialStashed := c.fastResume(taken.Cell, fastFrom)
	recordStep("fast_resume", "")
	phaseMu.Lock()
	shownElapsed = stepCursor.Sub(started)
	openGets = sc.Gets()
	openRounds = dir.Count()
	if fetcher != nil {
		if hasSupplied && !revalidated {
			openRounds = max(openRounds, fetcher.OpenRounds())
		} else {
			openRounds += fetcher.OpenRounds()
		}
	}
	phaseMu.Unlock()
	arrival := earlyArrival
	if arrival == nil {
		arrival = cell.BeginWorkspaceArrival(taken.Cell.Root, workspaceOf(taken.Cell.Root), stallBound, nil)
	} else {
		arrival.BindPaths(taken.Cell.Root, workspaceOf(taken.Cell.Root))
	}
	if taken.Cell.Root != root {
		arrival.BindPaths(root)
	}
	if filed != nil {
		filed()
		filed = nil
	}
	if arriveRootFence != nil {
		arriveRootFence()
		arriveRootFence = nil
	}
	if watch != nil {
		watch.setArrival(arrival)
		watch.setArriving()
	}
	bgMuWatch.Lock()
	bgActive = true
	bgMuWatch.Unlock()
	settleSync = false

	runAsync := (c.opt.OnShown != nil || c.opt.AsyncFiles) && c.eng.HasComposedCellDir(taken.Cell)
	syncEng := c.eng.Sync(cellstore.SyncKeys{CellKey: c.sync.Identity.CellKey(), Dedup: c.sync.Identity.DedupSecret()}, c.sync.Ledger)
	var bgMu sync.Mutex
	var bgRunning bool
	var bgDone chan struct{}
	var bgErr error
	var finalTaskCopies []string
	var finalResume preflight.Resume
	finalKept := taken.Kept

	var startBg func(initial bool)
	startBg = func(initial bool) {
		bgMu.Lock()
		if bgRunning || arrival.Ready() {
			bgMu.Unlock()
			return
		}
		bgRunning = true
		doneCh := make(chan struct{})
		bgDone = doneCh
		bgErr = nil
		bgMu.Unlock()
		if !initial {
			arrival.BeginRetry()
		}
		// keepLocalBeforeSwap seals this computer's older copy if it holds a change
		// that was never shared, uploads it as a kept branch, records that branch in
		// the directory and says so, all BEFORE the incoming files are put in place.
		// Every way it can fail comes back as an error: a check that could not run,
		// a seal that did not finish, a branch the directory would not record.
		keepLocalBeforeSwap := func(runCtx context.Context) error {
			preCell := cell.Cell{ID: id, Root: preRoot}
			preEng := c.eng
			preEng.WorkspaceOf = func(cell.Cell) string { return preWorkspace }
			bgBranched := alreadyBranched
			if preKept != "" {
				if kv, ok := c.sync.CachedCellView(preKept); ok && kv.Cell.ParentCell == id {
					bgBranched = kv.Cell.Head
				} else if kv, err := dir.Cell(runCtx, preKept); err == nil && kv.Cell.ParentCell == id {
					bgBranched = kv.Cell.Head
				} else {
					bgBranched = ""
				}
			}
			bgLocal := engineLocal{
				eng:             preEng,
				ctx:             runCtx,
				alreadyBranched: bgBranched,
			}
			unshared, turns, err := bgLocal.Unpublished(preCell, taken.Driving.Head)
			if err != nil {
				return fmt.Errorf("check %s for edits that were never shared: %w", id, err)
			}
			dirtyCheck := handoff.Trace(c.opt.OnPhase, "dirty check")
			dirty, err := bgLocal.Dirty(runCtx, preCell)
			dirtyCheck()
			if err != nil {
				return fmt.Errorf("check %s for unsaved edits: %w", id, err)
			}
			if dirty {
				sealed, added, err := bgLocal.Seal(runCtx, preCell)
				if err != nil {
					return fmt.Errorf("seal local edits of %s: %w", id, err)
				}
				unshared, turns = sealed, turns+added
			}
			if unshared == "" {
				return nil
			}
			if bgBranched != "" && unshared == bgBranched && preKept != "" {
				bgMu.Lock()
				finalKept = preKept
				bgMu.Unlock()
				return nil
			}
			fromDrv := &cellsync.Driving{Cell: preCell, Head: taken.Driving.Head, Frames: taken.Driving.Frames}
			keptUpload := handoff.Trace(c.opt.OnPhase, "kept-branch upload")
			keptID, err := taker.Branch(runCtx, fromDrv, unshared, turns)
			keptUpload()
			if err != nil {
				return fmt.Errorf("keep local edits of %s: %w", id, err)
			}
			if keptID == "" {
				return fmt.Errorf("keep local edits of %s: the directory named no branch for them", id)
			}
			var kt uint32
			if branchedTurns != 0 {
				kt = branchedTurns
			} else {
				kt = c.orphanTurns(runCtx, keptID)
			}
			bgMu.Lock()
			finalKept = keptID
			keptTurns = kt
			bgMu.Unlock()
			// Said in the log as well as to the person, so a record of a move can
			// tell a kept change from one that was never found.
			log.Printf("continue %s kept local edits as %s (%d turns) before the files arrived", id, keptID, kt)
			if runAsync && keptID != preKept {
				c.say(taken.Cell, chatlist.KeptEdits(kt, c.opt.DeviceName))
			}
			return nil
		}
		go func() {
			var runCtx context.Context
			var runWatch *moveWatch
			var runStop func()
			defer func() {
				bgMu.Lock()
				bgRunning = false
				close(doneCh)
				bgMu.Unlock()
				if runStop != nil {
					runStop()
				}
			}()
			if initial && !runAsync {
				runCtx, runWatch = ctx, watch
				runStop = func() {
					stopWatch()
					cancelMove(nil)
				}
			} else {
				if initial {
					stopWatch()
					cancelMove(nil)
				}
				runCtx, runWatch, runStop = watchMove(context.WithoutCancel(ctx), c.opt.OnMove, c.opt.IdleWithin)
				if runWatch != nil {
					runWatch.setArrival(arrival)
					runWatch.setArriving()
				}
			}
			runCtx = cell.WithArrivalOwner(runCtx)
			fetchDone := handoff.Trace(c.opt.OnPhase, "fetch")
			fetchErr := taker.Fetch.Complete(runCtx, taken.Cell, taken.Driving.Head)
			fetchDone()
			if fetchErr != nil {
				// THE SET-ASIDE COPY IS KEPT ONLY WHILE IT STILL GUARDS AN EDIT. The
				// files did not come, so nothing is replaced, and the next retry would
				// otherwise find it again. Finding out now whether it holds anything
				// lets it go when it does not, and when the edit is kept it goes too.
				// It stays when the check could not finish, or a session on this
				// computer may still be writing to the copy.
				if preRoot != "" && retirer.isDone() {
					settleCtx, stopSettle := context.WithTimeout(context.WithoutCancel(runCtx), 15*time.Second)
					if keepErr := keepLocalBeforeSwap(settleCtx); keepErr == nil {
						_ = os.RemoveAll(preRoot)
						preRoot = ""
					} else {
						log.Printf("continue %s keeps the older copy aside because its edits could not be checked yet: %v", id, keepErr)
					}
					stopSettle()
				}
				retirer.cancel()
				if context.Cause(runCtx) == ErrMoveStalled {
					fetchErr = fmt.Errorf("%w: %w", ErrMoveStalled, fetchErr)
				}
				plain := plainFetchFailure(fetchErr, c.sync.Relay)
				bgMu.Lock()
				bgErr = fetchErr
				bgMu.Unlock()
				arrival.Fail(errors.New(plain))
				if runWatch != nil {
					runWatch.setFailed(plain)
				}
				logPhases(fmt.Sprintf("failed reason=%q", fetchErr.Error()))
				return
			}
			retireBgDone := handoff.Trace(c.opt.OnPhase, "retirement")
			retirer.wait(runCtx, releaseWithin)
			retireBgDone()
			if preRoot != "" {
				// THE LOCAL EDITS ARE KEPT, OR THE FILES DO NOT ARRIVE. The older copy
				// here is about to be replaced, and a take that has already opened the
				// chat cannot be refused the way a take that checks first is, so a keep
				// that did not finish stops this stage with the gate closed and the
				// folder as it was. It never goes on to replace files with the change
				// sealed nowhere or uploaded with no record of it, which is how a change
				// was lost without a word. The pre-take state stays for the retry.
				if keepErr := keepLocalBeforeSwap(runCtx); keepErr != nil {
					log.Printf("continue %s kept nothing of the local edits, so the files stay as they are: %v", id, keepErr)
					plain := keepFailureWords(plainFetchFailure(keepErr, c.sync.Relay))
					bgMu.Lock()
					bgErr = keepErr
					bgMu.Unlock()
					arrival.Fail(errors.New(plain))
					if runWatch != nil {
						runWatch.setFailed(plain)
					}
					if runAsync {
						c.say(taken.Cell, plain+", so the files here are left as they are")
					}
					logPhases(fmt.Sprintf("failed reason=%q", keepErr.Error()))
					return
				}
				_ = os.RemoveAll(preRoot)
				preRoot = ""
			}
			restoreDone := handoff.Trace(c.opt.OnPhase, "restore")
			var matErr error
			if fetcher != nil {
				if mw, ok := fetcher.Engine.(interface {
					MaterializeWorkspace(context.Context, cell.Cell, string) error
				}); ok {
					matErr = mw.MaterializeWorkspace(runCtx, taken.Cell, taken.Driving.Head)
				} else {
					matErr = syncEng.MaterializeWorkspace(runCtx, taken.Cell, taken.Driving.Head)
				}
			} else {
				matErr = syncEng.MaterializeWorkspace(runCtx, taken.Cell, taken.Driving.Head)
			}
			restoreDone()
			if matErr != nil {
				bgMu.Lock()
				bgErr = matErr
				bgMu.Unlock()
				arrival.Fail(matErr)
				if runWatch != nil {
					runWatch.setFailed(matErr.Error())
				}
				logPhases(fmt.Sprintf("failed reason=%q", matErr.Error()))
				return
			}
			phaseMu.Lock()
			filesElapsed = time.Since(started)
			phaseMu.Unlock()

			for _, hook := range []func(context.Context, cell.Cell) error{restoreChatFiles, rebuildIndexes, c.pullVaultVia(sc)} {
				hookDone := handoff.Trace(c.opt.OnPhase, "hooks")
				hErr := hook(runCtx, taken.Cell)
				hookDone()
				if hErr != nil {
					c.say(taken.Cell, "This chat moved here, but part of its state could not be updated yet: "+hErr.Error())
				}
			}

			filingDone := handoff.Trace(c.opt.OnPhase, "filing")
			var copies []string
			if workspacePlan != nil {
				var rErr error
				copies, rErr = session.RestoreTaskWorkspaceSets(runCtx, session.Place{Dir: taken.Cell.Root, Workspace: workspaceOf(taken.Cell.Root)}, c.opt.WorkspaceRoots)
				if rErr != nil {
					c.say(taken.Cell, "a task's working copy could not be set up again here: "+rErr.Error())
				}
			} else {
				copies = c.restoreCopies(taken.Cell)
			}
			actualFrom := resolveFrom()
			if actualFrom != fastFrom {
				_ = cellstore.AdoptedFrom(taken.Cell, actualFrom)
			}
			if c.sync.teamsSharing() {
				if tErr := (teamKeys{c.sync.profile(), c.sync.Home, c.sync.Device.ID(), nil}).rebindChat(id, taken.Cell); tErr != nil {
					c.say(taken.Cell, "This chat moved here, but its teams could not be updated yet: "+tErr.Error())
				}
			}
			stashPath := filepath.Join(taken.Cell.Root, "resume.pending")
			_, stashStillThere := os.Stat(stashPath)
			res := c.arrive(taken.Cell, actualFrom)
			if initialStashed && os.IsNotExist(stashStillThere) {
				_ = os.Remove(stashPath)
			}
			c.eng.RecordMaterializedStat(taken.Cell, taken.Driving.Head)
			bgMu.Lock()
			curKept := finalKept
			curKeptTurns := keptTurns
			bgMu.Unlock()
			if curKept != "" && curKept != preKept && curKeptTurns == 0 {
				curKeptTurns = c.orphanTurns(runCtx, curKept)
				bgMu.Lock()
				keptTurns = curKeptTurns
				bgMu.Unlock()
			}
			sc.Settle()
			filingDone()

			bgMu.Lock()
			finalTaskCopies = copies
			finalResume = res
			bgMu.Unlock()

			arrival.Complete()
			if runWatch != nil {
				runWatch.setDone()
			}
			if filed != nil {
				filed()
				filed = nil
			}
			if arriveRootFence != nil {
				arriveRootFence()
				arriveRootFence = nil
			}
			// THE OPEN CARRIED ONLY THE PART OF THE LOOK THAT NEEDS NO FILES.
			// What this computer lacks (tools, versions) and what is uncommitted
			// in the folder are read from the files, so they exist only now; a
			// surface that opened the chat on the early Resume is told the whole
			// of it here, as dev's open, which looked after the files, said it.
			if runAsync && c.opt.OnArrived != nil {
				c.opt.OnArrived(taken.Cell, res)
			}
			logPhases("done")
		}()
	}

	arrival.SetRetry(func() { startBg(false) })
	waitFn := func(waitCtx context.Context) error {
		bgMu.Lock()
		ch := bgDone
		bgMu.Unlock()
		if ch != nil {
			select {
			case <-ch:
			case <-waitCtx.Done():
				return waitCtx.Err()
			}
		}
		bgMu.Lock()
		defer bgMu.Unlock()
		return bgErr
	}

	out = Continued{
		LocalOnly: storageOff,
		Taken:     taken,
		Device:    c.opt.DeviceName,
		KeptTurns: keptTurns,
		Resume:    initialResume,
		Shown:     shownElapsed,
		Wait:      waitFn,
		Retry:     arrival.Retry,
		KeptBranch: func() (string, uint32) {
			bgMu.Lock()
			defer bgMu.Unlock()
			return finalKept, keptTurns
		},
	}
	if c.opt.OnShown != nil {
		c.opt.OnShown(out)
	}
	startBg(true)

	if !runAsync {
		if waitErr := waitFn(parent); waitErr != nil {
			if filed != nil {
				filed()
			}
			if arriveRootFence != nil {
				arriveRootFence()
			}
			// A restart acquired nothing: the hold was this computer's before it began,
			// and a refetch that failed must not release it.
			var giveErr error
			if !restart {
				giveErr = c.giveBack(parent, id, taken.Driving.Fence)
			}
			return Continued{}, errors.Join(waitErr, giveErr)
		}
		bgMu.Lock()
		out.Taken.Kept = finalKept
		out.TaskCopies = finalTaskCopies
		out.Resume = finalResume
		out.KeptTurns = keptTurns
		bgMu.Unlock()
		phaseMu.Lock()
		out.Files = filesElapsed
		phaseMu.Unlock()
	} else {
		bgMuWatch.Lock()
		bgDetached = true
		bgMuWatch.Unlock()
	}
	return out, nil
}

// giveBack hands back the lease a take acquired before it failed, even when
// the take's context is what ended it (handoff.ReleaseContext).
func (c *Continuer) giveBack(ctx context.Context, id string, fence uint64) error {
	ctx, cancel := handoff.ReleaseContext(ctx)
	defer cancel()
	return c.sync.Dir.Release(ctx, id, fence)
}

func (c *Continuer) restoreTakenIdentity(_ context.Context, at cell.Cell) error {
	branches, err := cellsync.OpenBranchMap(c.sync.Home)
	if err != nil {
		return err
	}
	return branches.Remove(at.ID)
}

// releaseWithin bounds the wait for a session on this machine to let go of the
// chat. The engine host answers a request within half a second, so a holder
// that has not answered by now is not going to, and the take goes on without it.
const releaseWithin = 5 * time.Second

// retireHolder ends the session this machine's engine still holds for chat id,
// BEFORE the transcript is replaced. That session was open when the chat left
// (the engine outlives its windows, and a closed lid freezes it mid-chat), so
// it holds the conversation as it was before the other machine moved it on.
// Left alive, the window that opens after the take is handed that session again:
// old context, and every tool refused as superseded. Retiring it first also
// keeps its closing write on the old journal, where the take reconciles it,
// rather than on the one that has just arrived. It is the ordinary
// move-it-here request, so the engine closes the session the way it does for a
// second window; with no holder there is nothing to ask and nothing is written.
type holderRetirer struct {
	mu      sync.Mutex
	root    string
	journal string
	oldFile *os.File
	asked   bool
	done    bool
}

func newHolderRetirer(root string) *holderRetirer {
	return &holderRetirer{
		root:    root,
		journal: session.Place{Dir: root}.Transcript(),
	}
}

func (r *holderRetirer) ask() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.asked || r.done {
		return
	}
	if !session.InUse(r.journal) {
		r.done = true
		return
	}
	f, _ := os.Open(r.journal)
	if session.AskTakeover(r.root) != nil {
		if f != nil {
			_ = f.Close()
		}
		r.done = true
		return
	}
	r.oldFile = f
	r.asked = true
}

// isIdleReader says the window holding the chat's journal has told us, in its own
// fresh presence record, that it is open and doing nothing. A window that is
// working, waiting on its person, or says nothing at all is not one that may be
// asked to leave before the take is allowed.
func (r *holderRetirer) isIdleReader() bool {
	presence, ok := session.ReadSessionPresenceAnyAge(r.root)
	return ok && presence.PID > 0 && !presence.UpdatedAt.IsZero() &&
		time.Since(presence.UpdatedAt) <= session.TakeoverStale && presence.State == session.PresenceIdle
}

func (r *holderRetirer) isWorkingWriter() bool {
	holder, ok := session.ReadHolder(r.root, time.Now())
	return ok && holder.State == session.PresenceWorking
}

func (r *holderRetirer) isDone() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done || !r.inUseLocked()
}

func (r *holderRetirer) rebindJournal(newPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.oldFile != nil {
		_ = r.oldFile.Close()
		r.oldFile = nil
	}
	r.journal = newPath
}

func (r *holderRetirer) inUseLocked() bool {
	if r.oldFile != nil {
		if err := filelock.Lock(r.oldFile, true, true); err != nil {
			return filelock.IsBusy(err)
		}
		_ = filelock.Unlock(r.oldFile)
		return false
	}
	return session.InUse(r.journal)
}

func (r *holderRetirer) wait(ctx context.Context, within time.Duration) {
	r.mu.Lock()
	if !r.asked || r.done {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	deadline := time.NewTimer(within)
	defer deadline.Stop()
	for {
		r.mu.Lock()
		if r.done || !r.inUseLocked() {
			if r.oldFile != nil {
				_ = r.oldFile.Close()
				r.oldFile = nil
			}
			session.CancelTakeover(r.root)
			r.done = true
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			r.cancel()
			return
		case <-deadline.C:
			r.cancel()
			return
		case <-time.After(releaseBeat):
		}
	}
}

func (r *holderRetirer) cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.oldFile != nil {
		_ = r.oldFile.Close()
		r.oldFile = nil
	}
	if r.asked && !r.done {
		session.CancelTakeover(r.root)
	}
	r.done = true
}

func (c *Continuer) retireHolder(ctx context.Context, id string) {
	r := newHolderRetirer(c.opt.RootFor(id))
	r.ask()
	r.wait(ctx, releaseWithin)
}

// releaseBeat is how often the wait above looks at the journal's lock.
const releaseBeat = 10 * time.Millisecond

// arrive is the look at this machine that a takeover ends with, taken before
// anything here seals over the record the chat arrived with. The record is not a
// gate: a look that could not be kept still offers what it found.
func (c *Continuer) arrive(at cell.Cell, from string) preflight.Resume {
	resume, _ := preflight.Arrive(at.Root, workspaceOf(at.Root), from)
	if meta, err := session.LoadMeta(at.Root); err == nil && meta.ID != "" {
		meta.FromDevice, meta.FromWorkspace = from, resume.Was
		_ = session.SaveMeta(at.Root, meta)
	}
	return resume
}

// fastResume reads the incoming inventory and updates the local metadata
// without spawning git status, so the chat can open immediately (C1, C7).
func (c *Continuer) fastResume(at cell.Cell, from string) (preflight.Resume, bool) {
	work := workspaceOf(at.Root)
	var r preflight.Resume
	if store, err := inventory.Open(at.Root); err == nil {
		inv := store.Snapshot()
		r = preflight.Compare(from, inv, preflight.LocalHere{Root: at.Root, Workspace: work}, preflight.Report{})
	}
	r.Now, r.ChatNow = work, at.Root
	if meta, err := session.LoadMeta(at.Root); err == nil && meta.ID != "" {
		meta.FromDevice, meta.FromWorkspace = from, r.Was
		_ = session.SaveMeta(at.Root, meta)
	}
	stashed := false
	stashPath := filepath.Join(at.Root, "resume.pending")
	if r.Empty() && (r.ChatWas == "" || r.ChatWas == r.ChatNow) {
		_ = os.Remove(stashPath)
	} else if raw, err := json.Marshal(r); err == nil {
		if os.WriteFile(stashPath, raw, 0o600) == nil {
			stashed = true
		}
	}
	return r, stashed
}

// keepFailureWords is what a person reads when the change their older copy
// here holds could not be kept, so the files from the other computer have not
// been put over it. It names the reason in the same plain words a failed
// fetch uses, and it never says the edits were kept.
func keepFailureWords(reason string) string {
	return "your unsaved edits here could not be kept yet (" + reason + ")"
}

func plainFetchFailure(err error, relay string) string {
	switch {
	case errors.Is(err, ErrMoveStalled):
		return ErrMoveStalled.Error()
	case errors.Is(err, blobstore.ErrNotFound):
		return "part of this chat's folder is missing from sync"
	case errors.Is(err, blobstore.ErrAnswered):
		return "sync answered with an error"
	case errors.Is(err, blobstore.ErrUnreachable), errors.Is(err, directory.ErrUnreachable):
		return "sync could not be reached"
	default:
		return syncwords.Explain(err, relay).Error()
	}
}

// restoreCopies puts the task working copies the chat carried back to work in
// its project and answers which came back. A copy that cannot be cut again
// still has its files in place, so the takeover carries on and says so on the
// same desk as the vault's sentence.
func (c *Continuer) restoreCopies(at cell.Cell) []string {
	parents, planErr := session.TaskCopyRestoreParents(session.Place{Dir: at.Root})
	if planErr != nil {
		c.say(at, "task working copies were preserved but could not be rebound here: "+planErr.Error())
		return nil
	}
	restored, err := taskcopy.Carry{Cutter: session.TaskCopyCutter{Place: session.Place{Dir: at.Root}}}.RestoreWithParents(at, workspaceOf(at.Root), parents)
	if err != nil {
		c.say(at, "a task's working copy could not be set up again here: "+err.Error())
	}
	if adoptionErr := session.AdoptTaskWorkspaceIdentities(session.Place{Dir: at.Root}, workspaceOf(at.Root), restored); adoptionErr != nil {
		c.say(at, "a restored task's workspace identity could not be rebound here: "+adoptionErr.Error())
	}
	return restored
}

// holderName is the name of the device that held the chat last, for the log
// entry that stands for the head this machine received.
func (c *Continuer) holderName(ctx context.Context, id string) string {
	v, err := c.sync.Dir.Cell(ctx, id)
	if err != nil {
		return ""
	}
	return c.sync.deviceName(v.Cell.Lease.Device)
}

// orphanTurns is how many turns the directory says a branch holds.
func (c *Continuer) orphanTurns(ctx context.Context, branch string) uint32 {
	v, err := c.sync.Dir.Cell(ctx, branch)
	if err != nil {
		return 0
	}
	return v.Cell.OrphanTurns
}

// branch is the Taker's Branch: the Brancher, told what to say about the cell
// the kept edits become.
func (c *Continuer) branch(b cellsync.Brancher) func(context.Context, *cellsync.Driving, string, uint32) (string, error) {
	return c.branchWithDir(b, c.sync.Dir)
}

func (c *Continuer) branchWithDir(b cellsync.Brancher, dir directory.Client) func(context.Context, *cellsync.Driving, string, uint32) (string, error) {
	return func(ctx context.Context, from *cellsync.Driving, head string, turns uint32) (string, error) {
		if branches, err := cellsync.OpenBranchMap(c.sync.Home); err == nil {
			if existing := branches.Resolve(from.Cell.ID); existing != from.Cell.ID {
				if v, err := dir.Cell(ctx, existing); err == nil && v.Cell.ParentCell == from.Cell.ID && v.Cell.Head == head {
					from.Remote, from.Fence, from.Head = existing, 1, head
					return existing, nil
				}
			}
		}
		opened, err := cell.OpenAt(from.Cell.Root, from.Cell.ID)
		if err != nil {
			return "", err
		}
		// A final save can finish the same exchange as the earlier tool saves.
		// Count the complete kept conversation once after that save.
		if _, turns, err = cellstore.Unpublished(opened, from.Head); err != nil {
			return "", err
		}
		return b.Branch(ctx, from, head, turns, c.sync.publishInfo(opened, titleOf(opened.Root)))
	}
}

type countingDir struct {
	directory.Client
	calls atomic.Int64
}

func (d *countingDir) Count() int { return int(d.calls.Load()) }

func (d *countingDir) HeadTakes() bool {
	if ht, ok := d.Client.(interface{ HeadTakes() bool }); ok {
		return ht.HeadTakes()
	}
	return true
}

func (d *countingDir) List(ctx context.Context) (directory.Listing, error) {
	d.calls.Add(1)
	return d.Client.List(ctx)
}

func (d *countingDir) Cell(ctx context.Context, id string) (directory.CellView, error) {
	d.calls.Add(1)
	return d.Client.Cell(ctx, id)
}

func (d *countingDir) PutDevice(ctx context.Context, id string, dev directory.Device) error {
	d.calls.Add(1)
	return d.Client.PutDevice(ctx, id, dev)
}

func (d *countingDir) Revoke(ctx context.Context, id string) error {
	d.calls.Add(1)
	return d.Client.Revoke(ctx, id)
}

func (d *countingDir) SetVault(ctx context.Context, old, new string) error {
	d.calls.Add(1)
	return d.Client.SetVault(ctx, old, new)
}

func (d *countingDir) Create(ctx context.Context, id string, in directory.CellInit) (directory.CellView, error) {
	d.calls.Add(1)
	return d.Client.Create(ctx, id, in)
}

func (d *countingDir) Acquire(ctx context.Context, id string, opts directory.AcquireOpts) (directory.CellView, error) {
	d.calls.Add(1)
	return d.Client.Acquire(ctx, id, opts)
}

func (d *countingDir) Heartbeat(ctx context.Context, id string, b directory.Beat) (directory.CellView, error) {
	d.calls.Add(1)
	return d.Client.Heartbeat(ctx, id, b)
}

func (d *countingDir) Publish(ctx context.Context, id string, p directory.Publish) (directory.CellView, error) {
	d.calls.Add(1)
	return d.Client.Publish(ctx, id, p)
}

func (d *countingDir) Release(ctx context.Context, id string, fence uint64) error {
	d.calls.Add(1)
	return d.Client.Release(ctx, id, fence)
}

func (d *countingDir) Archive(ctx context.Context, id string) error {
	d.calls.Add(1)
	return d.Client.Archive(ctx, id)
}

func (d *countingDir) Presence(ctx context.Context) (directory.PresenceView, error) {
	d.calls.Add(1)
	return d.Client.Presence(ctx)
}

func (d *countingDir) ApproveRequest(ctx context.Context, code string, a directory.Approval) error {
	d.calls.Add(1)
	return d.Client.ApproveRequest(ctx, code, a)
}

func (d *countingDir) DenyRequest(ctx context.Context, code string) error {
	d.calls.Add(1)
	return d.Client.DenyRequest(ctx, code)
}

func (d *countingDir) Rotate(ctx context.Context, req directory.RotationReq) (directory.RotationView, error) {
	d.calls.Add(1)
	return d.Client.Rotate(ctx, req)
}

func (d *countingDir) Rotation(ctx context.Context) (directory.RotationView, error) {
	d.calls.Add(1)
	return d.Client.Rotation(ctx)
}

// landing is the engine for a chat that has not landed on this machine yet: the
// engine works from inside the folder it restores into, so the folders are
// made before it is asked what it is missing.
type landing struct {
	cellsync.Engine
	workspace func(cell.Cell) string
	rootFor   func(string) string
	warm      map[string]bool
}

// Holds consults the existing chat rather than its fresh staging folder, so a
// return still fetches only its delta before installing the replacement.
func (l landing) Holds(c cell.Cell) bool {
	if l.warm != nil {
		if held, ok := l.warm[c.ID]; ok {
			return held
		}
	}
	if l.rootFor != nil {
		c.Root = l.rootFor(c.ID)
	}
	return l.Engine.Holds(c)
}

func (l landing) OpenKeys() ([]byte, []byte) {
	if ok, has := l.Engine.(interface{ OpenKeys() ([]byte, []byte) }); has {
		return ok.OpenKeys()
	}
	return nil, nil
}

func (l landing) OpenCache(c cell.Cell) string {
	if ok, has := l.Engine.(interface{ OpenCache(cell.Cell) string }); has {
		return ok.OpenCache(c)
	}
	return ""
}

// installOpenInstructions puts the workspace's instruction files, which the open
// wave staged beside the conversation records, where the chat's system prompt
// will read them, so a chat that opens before its workspace has arrived still
// has its project's rules in the first turns. A file already in the workspace is
// never touched, because it is either the person's own edit or an older copy,
// and the restore that follows decides between them as it always has; a chat
// working inside one of the person's own folders is left to that restore
// entirely.
//
// THE RECORD UNDER root IS THE HEAD'S, NOT THIS COMPUTER'S. A chat that works in
// one of this computer's own folders but was last sealed by a computer that held
// it as a copy says "owned" here, so the answer this function reads from it is
// wrong for exactly the move back; the caller therefore never calls it for a chat
// it already knows works in place (see worksInPlace in take), and the check below
// is for a head that names a folder of its own that happens to exist here.
func installOpenInstructions(stagedRoot, root string) {
	if _, inPlace := projectOf(root); inPlace {
		return
	}
	workspace := workspaceOf(root)
	for _, name := range cellsync.OpenInstructionFiles {
		src := filepath.Join(stagedRoot, cellsync.OpenInstructionDir, name)
		info, err := os.Lstat(src)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		raw, err := os.ReadFile(src)
		if err != nil || os.MkdirAll(workspace, 0o700) != nil {
			continue
		}
		f, err := os.OpenFile(filepath.Join(workspace, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			continue
		}
		_, werr := f.Write(raw)
		if cerr := f.Close(); werr != nil || cerr != nil {
			_ = os.Remove(filepath.Join(workspace, name))
		}
	}
}

// preserveExistingSubdirs carries the folders of the old .cell/ that the open
// state does not cover (tasks, artifacts, and the like) into the staged one, so
// the chat opens with something in them while the rest arrives. It answers the
// stat of every file now under a carried folder, keyed by its name relative to
// .cell/.
//
// A file the head already placed in the staged folder ALWAYS wins over the old
// copy of the same name, at every depth: carrying an older version over the
// incoming one would let the next seal publish the older. A file only the old
// copy has is kept, and is the older version of whatever the head holds under
// that name until the restore replaces it (see cellstore.Engine.NoteCarried).
func preserveExistingSubdirs(oldCellDir, newCellDir string) map[string]cellstore.CarriedFile {
	entries, err := os.ReadDir(oldCellDir)
	if err != nil {
		return nil
	}
	_ = os.MkdirAll(newCellDir, 0o700)
	var carried []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "env" || name == "workspace-carrier" {
			continue
		}
		src := filepath.Join(oldCellDir, name)
		dst := filepath.Join(newCellDir, name)
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			if os.Rename(src, dst) == nil {
				carried = append(carried, name)
			}
			continue
		}
		if _, err := os.ReadDir(dst); err == nil {
			_ = filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				rel, relErr := filepath.Rel(dst, p)
				if relErr != nil {
					return nil
				}
				to := filepath.Join(src, rel)
				if os.MkdirAll(filepath.Dir(to), 0o700) == nil {
					_ = os.Rename(p, to)
				}
				return nil
			})
			_ = os.RemoveAll(dst)
			if os.Rename(src, dst) == nil {
				carried = append(carried, name)
			}
		}
	}
	files := map[string]cellstore.CarriedFile{}
	for _, name := range carried {
		_ = filepath.WalkDir(filepath.Join(newCellDir, name), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			info, iErr := d.Info()
			rel, relErr := filepath.Rel(newCellDir, p)
			if iErr == nil && relErr == nil && info.Mode().IsRegular() {
				files[filepath.ToSlash(rel)] = cellstore.FingerprintOf(info)
			}
			return nil
		})
	}
	return files
}

func (l landing) Want(ctx context.Context, c cell.Cell, head string) ([]string, error) {
	workspace := workspaceOf(c.Root)
	if l.workspace != nil {
		workspace = l.workspace(c)
	}
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return nil, err
	}
	return l.Engine.Want(ctx, c, head)
}

func (l landing) MaterializeWorkspace(ctx context.Context, c cell.Cell, head string) error {
	if mw, ok := l.Engine.(interface {
		MaterializeWorkspace(context.Context, cell.Cell, string) error
	}); ok {
		return mw.MaterializeWorkspace(ctx, c, head)
	}
	return l.Engine.Materialize(ctx, c, head)
}

type localWorkCache struct {
	eng     cellstore.Engine
	mu      sync.Mutex
	started bool
	done    chan struct{}
	work    bool
	err     error
}

func (c *localWorkCache) start(ctx context.Context, at cell.Cell) {
	if c == nil || c.eng.Run != nil {
		return
	}
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return
	}
	c.started = true
	ch := make(chan struct{})
	c.done = ch
	c.mu.Unlock()
	go func() {
		defer close(ch)
		w, err := c.eng.LocalWork(ctx, at)
		c.mu.Lock()
		c.work, c.err = w, err
		c.mu.Unlock()
	}()
}

func (c *localWorkCache) get(ctx context.Context, eng cellstore.Engine, at cell.Cell) (bool, error) {
	if c == nil || eng.Run != nil {
		return eng.LocalWork(ctx, at)
	}
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		c.start(ctx, at)
		c.mu.Lock()
	}
	ch := c.done
	c.mu.Unlock()
	select {
	case <-ch:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.work, c.err
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (c *localWorkCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.started = false
	c.done = nil
	c.mu.Unlock()
}

// preTakeTemp makes the folder the older copy's state is set aside in. It is a
// variable only so a test can make the disk refuse it.
var preTakeTemp = os.MkdirTemp

// savePreTakeState sets this computer's older copy of the chat aside so its
// unsaved edits can still be checked, sealed and kept after the incoming state
// has been put in place. It FAILS CLOSED: a copy with nothing to set aside
// answers "" and no error, but any failure to set it aside is an error, so the
// take stops before anything is replaced instead of going on with nothing left
// to check the edits against. A journal it had already moved is moved back.
func savePreTakeState(targetRoot, preWorkspace string, moveJournal bool, retirer *holderRetirer) (string, error) {
	srcState := filepath.Join(targetRoot, cell.StateDir)
	if _, err := os.Stat(srcState); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	preRoot, err := preTakeTemp(filepath.Dir(targetRoot), ".precell-")
	if err != nil {
		return "", err
	}
	srcJournal := filepath.Join(targetRoot, filepath.FromSlash(cell.TranscriptPath))
	preJournal := filepath.Join(preRoot, filepath.FromSlash(cell.TranscriptPath))
	movedJournal := false
	fail := func(err error) (string, error) {
		if movedJournal && os.Rename(preJournal, srcJournal) == nil && retirer != nil {
			retirer.rebindJournal(srcJournal)
		}
		_ = os.RemoveAll(preRoot)
		return "", err
	}
	if err := os.CopyFS(filepath.Join(preRoot, cell.StateDir), os.DirFS(srcState)); err != nil {
		return fail(err)
	}
	if moveJournal {
		_ = os.Remove(preJournal)
		if err := os.Rename(srcJournal, preJournal); err == nil {
			movedJournal = true
			if retirer != nil {
				retirer.rebindJournal(preJournal)
			}
		}
	}
	if !cellstore.ForeignFolder(targetRoot, preWorkspace) {
		_ = os.Remove(filepath.Join(preRoot, filepath.FromSlash(cellstore.OwnFolderPath)))
	}
	m, _ := session.LoadMeta(targetRoot)
	if m.ID == "" {
		m.ID = filepath.Base(targetRoot)
	}
	m.Workspace = preWorkspace
	m.Owned = false
	if err := session.SaveMeta(preRoot, m); err != nil {
		return fail(err)
	}
	return preRoot, nil
}

// engineLocal is the Taker's view of a copy of the chat this machine has: what
// the engine says differs from the newest turn, and a seal of it.
type engineLocal struct {
	eng             cellstore.Engine
	ctx             context.Context
	alreadyBranched string
	cache           *localWorkCache
	fastOnly        bool
	unknownClean    *bool
	retireWait      func(context.Context)
	deferWhileInUse func() bool
}

func (l engineLocal) localWork(ctx context.Context, c cell.Cell) (bool, error) {
	if l.fastOnly {
		dirty, known, err := l.eng.LocalWorkFast(ctx, c)
		if err == nil && !dirty && !known && l.unknownClean != nil {
			*l.unknownClean = true
		}
		return dirty, err
	}
	if l.cache != nil {
		return l.cache.get(ctx, l.eng, c)
	}
	return l.eng.LocalWork(ctx, c)
}

func (l engineLocal) Unpublished(c cell.Cell, remoteHead string) (string, uint32, error) {
	head, turns, err := cellstore.Unpublished(c, remoteHead)
	if err != nil || head == "" {
		return head, turns, err
	}
	if l.alreadyBranched != "" && head == l.alreadyBranched && l.fastOnly {
		if l.unknownClean != nil {
			*l.unknownClean = true
		}
		return "", 0, nil
	}
	// A retired session can seal unchanged bookkeeping after losing its
	// hold. Only work beyond the shared snapshot belongs in a kept branch.
	work, err := l.localWork(l.ctx, c)
	if err != nil || !work {
		return "", 0, err
	}
	if l.deferWhileInUse != nil && l.deferWhileInUse() {
		if l.unknownClean != nil {
			*l.unknownClean = true
		}
		return "", 0, nil
	}
	if l.retireWait != nil {
		l.retireWait(l.ctx)
	}
	return head, turns, nil
}

func (l engineLocal) Dirty(ctx context.Context, c cell.Cell) (bool, error) {
	if l.alreadyBranched != "" && !l.fastOnly {
		if head, err := cellstore.Head(c); err == nil && head != nil && head.Turn.ID == l.alreadyBranched && cellstore.Unsaved(c) == 0 {
			if proof, err := l.eng.SavedState(ctx, c); err == nil && proof != nil && len(proof.Changes) == 0 {
				return false, nil
			}
		}
	}
	// Both appended accounting and edits inside an older message must be checked
	// against the shared snapshot, even when the current journal has the same size.
	dirty, err := l.localWork(ctx, c)
	if err == nil && dirty && l.alreadyBranched != "" && l.fastOnly {
		if head, hErr := cellstore.Head(c); hErr == nil && head != nil && head.Turn.ID == l.alreadyBranched && cellstore.Unsaved(c) == 0 {
			if l.unknownClean != nil {
				*l.unknownClean = true
			}
			return false, nil
		}
	}
	if err == nil && dirty && l.deferWhileInUse != nil && l.deferWhileInUse() {
		if l.unknownClean != nil {
			*l.unknownClean = true
		}
		return false, nil
	}
	return dirty, err
}

func (l engineLocal) Follow(ctx context.Context, from, to cell.Cell) error {
	return l.eng.Follow(ctx, from, to)
}

// Seal seals the tree as one turn of the copy. The Taker names a cell by id and
// folder only, and a seal needs the cell's own record, so it is opened here.
func (l engineLocal) Seal(ctx context.Context, c cell.Cell) (string, uint32, error) {
	if l.retireWait != nil {
		l.retireWait(ctx)
	}
	l.cache.invalidate()
	opened, err := cell.OpenAt(c.Root, c.ID)
	if err != nil {
		return "", 0, err
	}
	eng := l.eng
	if eng.FolderGone(opened) {
		// UNSAVED WORDS OUTLIVE THE FOLDER THEY WERE SPOKEN IN. A seal of a folder
		// that is not there refuses (cellstore.ErrWorkspaceGone), which would
		// stop the move for a chat whose words still have to be kept. They are
		// sealed against this copy's own empty work/ folder instead, so the
		// branch holds the conversation and the deleted path is never made again.
		own := session.Place{Dir: c.Root, Owned: true}.Work()
		if err := os.MkdirAll(own, 0o700); err != nil {
			return "", 0, err
		}
		eng.WorkspaceOf = func(cell.Cell) string { return own }
	}
	sealed, err := eng.SealRecovered(ctx, opened)
	return sealed.Turn.ID, 1, err
}

// workspaceOf is the folder the tools of the chat kept at root work in: the
// project folder its session names while that folder is here, and its own
// work/ folder otherwise, which is where a copy taken from another machine
// lands.
func workspaceOf(root string) string {
	if project, ok := projectOf(root); ok {
		return project
	}
	return session.Place{Dir: root, Owned: true}.Work()
}

// projectOf is the project folder the chat at root works in, when it works in
// one of the person's folders and that folder is here. A chat that carries
// only its own folder has no project in a folder that is somebody's elsewhere
// (cellstore.ForeignFolder): it is never taken in place there.
func projectOf(root string) (string, bool) {
	m, err := session.LoadMeta(root)
	return m.Workspace, err == nil && !m.Owned && dirExists(m.Workspace) && !cellstore.ForeignFolder(root, m.Workspace)
}

// keptFolderOf is the folder this computer's older copy of the chat at root
// kept its files in, which is what that copy is checked for unsaved edits
// against. It is the project folder the record names even when that folder was
// deleted: [workspaceOf] answers with the chat's own work/ folder for a project
// that is not here, a folder the take itself creates before the check runs, so
// asking it would check the new empty folder and never see that the old files
// are gone. A folder that is not there holds no edits, and the check says so.
//
// ONLY WHEN NO FOLDER OF THIS CHAT IS HERE AT ALL. A work/ folder that already
// exists may hold files, and the older answer, which checks it, stays.
func keptFolderOf(root string) string {
	m, err := session.LoadMeta(root)
	if err == nil && !m.Owned && strings.TrimSpace(m.Workspace) != "" && !dirExists(m.Workspace) &&
		!cellstore.ForeignFolder(root, m.Workspace) && !dirExists(session.Place{Dir: root, Owned: true}.Work()) {
		return m.Workspace
	}
	return workspaceOf(root)
}

func borrowsProject(root string) bool {
	_, ok := projectOf(root)
	return ok
}

func dirExists(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	return path != "" && err == nil && info.IsDir()
}

// recordOrigin keeps, beside the chat, the folder it was left in on the machine
// it came from when that folder is not here: the seal's inventory names it, and
// this machine's own seals will overwrite it. A chat whose project is here
// needs no note.
func recordOrigin(_ context.Context, c cell.Cell) error {
	if !session.OwnsFolder(workspaceOf(c.Root)) {
		return nil
	}
	store, err := inventory.Open(c.Root)
	if err != nil {
		return err
	}
	origin := store.Snapshot().Workspace
	m, err := session.LoadMeta(c.Root)
	if err != nil || m.ID == "" {
		return err
	}
	if origin != "" && origin != workspaceOf(c.Root) {
		m.Origin = origin
	}
	m.Owned = true
	m.Workspace = session.Place{Dir: c.Root, Owned: true}.Work()
	return session.SaveMeta(c.Root, m)
}

func titleOf(root string) string {
	m, _ := session.LoadMeta(root)
	return m.Title
}

// restoreChatFiles puts the chat's job logs and saved tool output back beside
// it, so the paths its messages name open on this machine. Task journals are
// not among them: they travel inside .cell/tasks and arrive where they are read.
// A carry made before that (task journals under .cell/files) still restores
// them beside the chat, and the open that follows files them into .cell/tasks.
func restoreChatFiles(ctx context.Context, c cell.Cell) error {
	if _, err := (chatfiles.Carry{}).Restore(c); err != nil {
		return err
	}
	return session.RestoreCarriedProcessHistory(ctx, c.Root)
}

// seedMeta writes the session folder's meta.json from the newly installed
// transcript and sealed truth before the chat opens, while the rest of the
// derived indexes wait for the background after-work.
func seedMeta(_ context.Context, c cell.Cell) error {
	_ = os.Remove(filepath.Join(c.Root, "meta.json"))
	d, err := session.ReadDigest(session.Place{Dir: c.Root}.Transcript())
	if err != nil || d.ID == "" {
		return err
	}
	origin := d.Workspace
	if w := workspaceOf(c.Root); w != "" {
		d.Workspace = w
	}
	held, _ := session.LoadMeta(c.Root)
	if held.Origin == "" && origin != "" && origin != d.Workspace {
		held.Origin = origin
	}
	return session.SaveMeta(c.Root, d.Derive(held))
}

// rebuildIndexes makes the session row and the other derived files of a chat
// that just arrived, from its transcript, so this machine lists it. A chat this
// machine already held comes with older indexes: they are brought up to date too.
func rebuildIndexes(_ context.Context, c cell.Cell) error {
	_, err := cellindex.Refresh(c, workspaceOf(c.Root))
	return err
}

// pullVaultVia brings this identity's secrets here, and with them the files the
// chat's seals withheld, which are written back into the folder the tools run in.
//
// While blind pass-through carries the chat there is no vault to pull from (it
// waits for storage), so the files come from the computer that has the chat, over
// the tunnel its fetch already opened ([Continuer.takeSecretFiles]); the vault's
// own pull is left exactly as it was.
func (c *Continuer) pullVaultVia(sc *Scope) func(context.Context, cell.Cell) error {
	return func(ctx context.Context, at cell.Cell) error {
		opened, err := cell.OpenAt(at.Root, at.ID)
		if err != nil {
			return err
		}
		files, err := c.sync.files(c.eng, opened)
		if err != nil {
			return err
		}
		err = c.sync.withVaultWithin(ctx, vaultBusyWait, func(v vaultsync.Syncer) error { return v.Pull(ctx) }, files)
		return errors.Join(err, c.takeSecretFiles(ctx, sc, files))
	}
}

// files is the withheld files of c as a carrier: the workspace the engine seals
// and the paths its guard keeps out of the seals. Its slots are named by the id
// the directory knows the chat by, which a branch does not share with the chat
// it grew from.
func (s *Sync) files(eng cellstore.Engine, c cell.Cell) (vaultsync.Files, error) {
	branches, err := cellsync.OpenBranchMap(s.Home)
	if err != nil {
		return vaultsync.Files{}, err
	}
	// Only the folder is wanted here; the paths are asked again at each use, since
	// a seal in between changes them.
	tree, _, _ := eng.Withheld(c)
	return vaultsync.Files{
		Root: tree, Chat: branches.Resolve(c.ID),
		Withheld: func() ([]string, error) { _, paths, err := eng.Withheld(c); return paths, err },
	}, nil
}

// withVault runs use with the syncer of this machine's vault, which also carries
// extra beside the profile state. The vault file is made if it is not there
// yet, with this machine's identity as its key. The syncer's store is counted
// under the vault scope, so vaultsync stays unaware of counting, and what the
// run moved is written when it ends.
func (s *Sync) withVault(use func(vaultsync.Syncer) error, extra ...vaultsync.Carrier) error {
	return s.withVaultWithin(context.Background(), 0, use, extra...)
}

// A CONTINUE WAITS FOR THE VAULT; A BACKGROUND EXCHANGE DOES NOT. Every running
// chat's drive pushes the vault under state-sync.lock, and over a slow relay it
// holds the lock for seconds. A background exchange that finds it busy may skip,
// because the holder is already carrying the same state; a continue's pull may
// not, because it is the last step of a move the person asked for, and failing
// it handed the chat back with "could not be put in this folder" (D4 in the
// two-network test). So the continue waits up to vaultBusyWait, then fails as
// before.
const (
	vaultBusyWait  = 30 * time.Second
	vaultBusyRetry = 100 * time.Millisecond
)

// withVaultWithin is withVault for a caller that waits up to within for another
// codeaf on this computer to finish its own exchange; zero never waits.
func (s *Sync) withVaultWithin(ctx context.Context, within time.Duration, use func(vaultsync.Syncer) error, extra ...vaultsync.Carrier) error {
	return s.withVaultRound(ctx, within, false, use, extra...)
}

// Only idle certification may skip a competing in-process exchange. Ordinary
// callers serialize as before, with cancellation belonging to their context.
func (s *Sync) withVaultRound(ctx context.Context, within time.Duration, idle bool, use func(vaultsync.Syncer) error, extra ...vaultsync.Carrier) error {
	if idle {
		if !stateMu.TryLock() {
			return errStateBusy
		}
	} else if err := lockDrive(ctx, &stateMu); err != nil {
		return err
	}
	defer stateMu.Unlock()
	// Refuse before opening the lock file as well as before creating keys, so
	// a replacement directory receives none of the previous home's state.
	if home.Removed(s.Home) {
		return identity.ErrHomeRemoved
	}
	lock, err := os.OpenFile(filepath.Join(s.Home, "state-sync.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockState(ctx, lock, within); err != nil {
		return err
	}

	v, err := keys.Open(s.Home)
	if err != nil {
		return err
	}
	sc := s.scope(cellstats.VaultScope)
	defer sc.Settle()
	return use(vaultsync.Syncer{StorageOn: s.StorageAllowed, Store: sc.Store, Dir: s.Dir, Vault: v, CellKeyID: s.Identity.CellKeyID(),
		Carry: append(s.carried(), extra...)})
}

var errStateBusy = errors.New("sync: another home-state exchange is in progress")

// lockState takes the vault's lock without blocking, and while another process
// holds it retries every vaultBusyRetry until within has passed or ctx ends. The
// answer after the bound is the busy refusal itself, so a caller that gives up
// says what it always said.
func lockState(ctx context.Context, lock *os.File, within time.Duration) error {
	err := filelock.Lock(lock, true, true)
	if within <= 0 || !filelock.IsBusy(err) {
		return err
	}
	deadline := time.Now().Add(within)
	tick := time.NewTicker(vaultBusyRetry)
	defer tick.Stop()
	for filelock.IsBusy(err) && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-tick.C:
		}
		err = filelock.Lock(lock, true, true)
	}
	return err
}

// profile is the directory the product reads config.json and credentials.json
// from, the same one its own stores resolve.
func (s *Sync) profile() string {
	if s.ProfileDir != "" {
		return s.ProfileDir
	}
	return s.Home
}

// teamsSharing leaves every carrier quiet when this computer has not paired.
func (s *Sync) teamsSharing() bool {
	return config.TeamsSharingAt(s.profile(), func() bool { return MayTalk(s.Home) })
}

// IdleHandoffAllowed reports whether the idle-handoff setting is on and
// encrypted relay storage is allowed for this home.
func (s *Sync) IdleHandoffAllowed() bool {
	return s != nil && s.StorageAllowed() && config.IdleHandoffAt(s.profile())
}

// carried is the profile state that travels in the vault beside the secrets.
func (s *Sync) carried() []vaultsync.Carrier {
	dir := s.profile()
	carried := []vaultsync.Carrier{vaultsync.Credentials(filepath.Join(dir, vaultsync.CredentialsFile)), providerKeys(dir, s.Home), preferences(dir, s.Home), sharedMemories(s.Home)}
	if !s.teamsSharing() {
		return carried
	}
	if s.teamsCache == nil {
		s.teamsCache = &teamIdentityCache{}
	}
	s.teamsCache.scanned = false
	report := func(err error) { teams.SharingError(dir, err) }
	return append(carried,
		vaultsync.BestEffort(syncedTeams(dir, s.Home, s.Device.ID(), s.teamsCache), s.teamsSharing, report),
		vaultsync.BestEffort(vaultsync.Windows("team-traffic", s.Device.ID(), &teamTraffic{teamKeys: teamKeys{dir, s.Home, s.Device.ID(), s.teamsCache}, peers: s.pairedTrafficDevices}, filepath.Join(s.Home, "teams-traffic-sync.ledger"), teams.TrafficSyncEvery), s.teamsSharing, report))
}

// Discard sets a branch aside: it is archived, not deleted, so it leaves every
// machine's list. Only a live branch can be discarded.
func (s *Sync) Discard(ctx context.Context, branch string) error {
	v, err := s.Dir.Cell(ctx, branch)
	if err != nil {
		return err
	}
	if v.Cell.ParentCell == "" || v.Cell.Archived {
		return errors.New("not a live branch: " + branch)
	}
	return s.Dir.Archive(ctx, branch)
}

func newCellID() (string, error) { return cell.NewID(time.Now()) }

// publishInfo is what the directory is told about c: its class, its title
// sealed under the metadata key, and who can open it. While blind pass-through
// is the road it carries neither the title nor the folder, for EVERY caller:
// the drive's publishes, the kept-edits branch a take makes and the background
// pass all build their record here, so there is no second place to forget.
func (s *Sync) publishInfo(c cell.Cell, title string) cellsync.PublishInfo {
	in := cellsync.PublishInfo{
		Class: string(c.Meta().Class),
		Keys:  map[string]map[string]string{c.Meta().CellKeyID: {s.Identity.ID(): ""}},
	}
	if s.recordsWithholdNames() {
		return in
	}
	if title != "" {
		in.Title, _ = directory.SealTitle(directory.MetadataKey(s.Identity.CellKey()), title)
	}
	if folder := chatFolder(c.Root); folder != "" {
		in.Folder, _ = directory.SealTitle(directory.MetadataKey(s.Identity.CellKey()), folder)
	}
	in.Crew = s.sealedCrew(c)
	return in
}

// chatFolder is the name of the project folder a chat works in, which a move
// copies with it; "" for a chat in its own private folder or with no meta.
func chatFolder(root string) string {
	meta, err := session.LoadMeta(root)
	if err != nil || meta.Owned || meta.Workspace == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(meta.Workspace))
}

// Driving is how this device drives c now: the directory id it drives (the
// branch a death made it, when there was one), the fence it holds and the head.
// A device that does not hold the lease answers fence 0, which nothing accepts.
func (s *Sync) Driving(ctx context.Context, c cell.Cell) (*cellsync.Driving, error) {
	branches, err := cellsync.OpenBranchMap(s.Home)
	if err != nil {
		return nil, err
	}
	d := &cellsync.Driving{Cell: c}
	if id := branches.Resolve(c.ID); id != c.ID {
		d.Remote = id
	}
	v, err := s.Dir.Cell(ctx, d.ID())
	if err != nil {
		return nil, err
	}
	if v.Cell.Lease.Expires > v.Now && v.Cell.Lease.Device == s.Device.ID() {
		d.Fence, d.Head = v.Cell.Lease.Fence, v.Cell.Head
	}
	return d, nil
}

// ReleaseUnused gives up only the unused acquisition returned to this window.
// The directory checks the fence, so a later writer's ownership is never released.
func (c *Continuer) ReleaseUnused(ctx context.Context, id string, fence uint64) error {
	// Match the close path's bounded retry: a transient refusal must not strand
	// an otherwise successful automatic acquisition without an open conversation.
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := c.sync.Dir.Release(ctx, id, fence)
		if errors.Is(err, directory.ErrFenceStale) {
			return nil
		}
		if !errors.Is(err, directory.ErrUnreachable) && !errors.Is(err, wireauth.ErrUnavailable) {
			return err
		}
		if _, bounded := ctx.Deadline(); !bounded {
			return err
		}
		timer := time.NewTimer(cellsync.ReleaseRetryEvery)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// takeLocalStorageOff acquires through the directory without uploading or restoring over local turns.
func (c *Continuer) takeLocalStorageOff(ctx context.Context, id, root string, view directory.CellView, seen *directory.LeaseExpectation) (Continued, error) {
	at, err := cell.OpenAt(root, id)
	if err != nil {
		return Continued{}, syncwords.ErrContinueStorageOff
	}
	contains := func(head string) error {
		if _, _, err := cellstore.Unpublished(at, head); err != nil {
			return err
		}
		// An empty record has no remote turns to require. Unpublished conservatively
		// retains divergent work too, so only ancestry can permit an in-place claim.
		if head == "" {
			return nil
		}
		extends, err := cellstore.Extends(at, head)
		if err != nil {
			return err
		}
		if !extends {
			return syncwords.ErrContinueStorageOff
		}
		return nil
	}
	if view.Cell.Lease.Device != seen.Device || view.Cell.Lease.Fence != seen.Fence {
		return Continued{}, directory.ErrLeaseHeld
	}
	if err := contains(view.Cell.Head); err != nil {
		return Continued{}, err
	}
	got, err := c.sync.Dir.Acquire(ctx, id, directory.AcquireOpts{RequireConditional: true, Force: view.Cell.Lease.Expires > view.Now, Expect: seen})
	if err != nil {
		return Continued{}, err
	}
	fail := func(err error) (Continued, error) {
		return Continued{}, errors.Join(err, c.sync.Dir.Release(ctx, id, got.Cell.Lease.Fence))
	}
	// Publishing can race the acquire; validate its actual head before recording ownership.
	if err := contains(got.Cell.Head); err != nil {
		return fail(err)
	}
	if err := c.sync.recordSendLease(id, sendLease{Cell: id, Fence: got.Cell.Lease.Fence}); err != nil {
		return fail(err)
	}
	return Continued{LocalOnly: true, Device: c.opt.DeviceName, Taken: handoff.Taken{Cell: at, Driving: cellsync.Driving{Cell: at, Fence: got.Cell.Lease.Fence, Head: got.Cell.Head, Frames: got.Cell.Frames}}}, nil
}
