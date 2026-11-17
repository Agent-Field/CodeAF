package syncsetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
}

// Continuer is the take side of one machine: `continue here`.
type Continuer struct {
	sync     *Sync
	eng      cellstore.Engine
	opt      TakeOptions
	freeOnly bool
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
		After:    []func(context.Context, cell.Cell) error{rebuildIndexes, recordOrigin, restoreChatFiles, c.pullVault},
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

func (c *Continuer) take(ctx context.Context, id string, seen *directory.LeaseExpectation, branchOnly bool) (out Continued, err error) {
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
	sc := c.sync.scope(id)
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
	if keepRoot && (seen == nil || seen.Device != c.sync.Device.ID()) {
		if _, fence, lErr := c.sync.SendLease(id); lErr != nil || fence == 0 {
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
	if err := cell.WaitWorkspace(ctx, root); err != nil {
		localCapture()
		return Continued{}, err
	}
	if err := cellstore.Await(ctx, cell.Cell{Root: root}); err != nil {
		localCapture()
		return Continued{}, err
	}
	localCapture()
	recordStep("capture", "")
	localCache := &localWorkCache{eng: c.eng}
	var view directory.CellView
	if supplied, ok := c.sync.takeCellView(id); ok {
		dir.calls.Add(1)
		view = supplied
	} else {
		if keepRoot {
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
	if heldHere {
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
		return Continued{AlreadyHere: true, LocalOnly: !c.sync.StorageAllowed(), Device: c.opt.DeviceName, Taken: handoff.Taken{
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
	storageOff := !c.sync.StorageAllowed()
	// An own free chat can return without replacing its open local conversation.
	keepOpen := storageOff && lease.Device == c.sync.Device.ID() && lease.Expires <= view.Now
	filesFirst := MoveFilesFirst() || branchOnly || view.Cell.ParentCell != ""
	wantAsync := !filesFirst && (c.opt.OnShown != nil || c.opt.AsyncFiles)
	alreadyBranched := ""
	if preKept != "" && keepRoot {
		if unshared, _, uErr := cellstore.Unpublished(cell.Cell{ID: id, Root: root}, view.Cell.Head); uErr == nil && unshared != "" {
			if kv, err := dir.Cell(ctx, preKept); err == nil && kv.Cell.ParentCell == id {
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
		}
	}
	if !keepOpen {
		retire()
	}
	localWork := engineLocal{
		eng:             c.eng,
		ctx:             ctx,
		alreadyBranched: alreadyBranched,
		cache:           localCache,
		retireWait: func(wCtx context.Context) {
			defer handoff.Trace(c.opt.OnPhase, "retirement")()
			retirer.wait(wCtx, releaseWithin)
		},
	}
	if storageOff {
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
	if project, here := projectOf(root); here {
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
		if project, here := projectOf(c.opt.RootFor(id)); here {
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
			preserveExistingSubdirs(filepath.Join(target.Root, cell.StateDir), filepath.Join(staged.Root, cell.StateDir))
			if err := handoff.Swap(filepath.Join(staged.Root, cell.StateDir), filepath.Join(target.Root, cell.StateDir)); err != nil {
				return err
			}
			if err := cellstore.NoteAdopted(target, head); err != nil {
				return err
			}
			return cellstore.NotePublished(target, head)
		}
		after := []func(context.Context, cell.Cell) error{c.restoreTakenIdentity}
		if view.Cell.ParentCell != "" {
			after = append(after, branchIdentity)
		}
		after = append(after, seedMeta)
		if project, here := projectOf(c.opt.RootFor(id)); here {
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
	taken, err := taker.TakeSeen(ctx, id, seen)
	if err != nil {
		return Continued{}, err
	}
	place := session.Place{Dir: taken.Cell.Root, Workspace: intendedPrimary}
	workspacePlan, err := session.TaskWorkspaceTransferPlan(place)
	if err != nil {
		return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
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
		return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
	}
	if filesFirst {
		defer handoff.Trace(c.opt.OnPhase, "filing")()
		if err := cellstore.AdoptedFrom(taken.Cell, resolveFrom()); err != nil {
			return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
		}
		if err := cellstore.NotePublished(taken.Cell, taken.Driving.Head); err != nil {
			return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
		}
		var filed func()
		defer func() {
			if filed != nil {
				filed()
			}
		}()
		if taken.Cell, filed, err = c.fileCopy(ctx, taken.Cell, keepRoot); err != nil {
			return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
		}
		taken.Driving.Cell = taken.Cell
		var taskCopies []string
		if workspacePlan != nil {
			taskCopies, err = session.RestoreTaskWorkspaceSets(ctx, session.Place{Dir: taken.Cell.Root, Workspace: workspaceOf(taken.Cell.Root)}, c.opt.WorkspaceRoots)
			if err != nil {
				return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
			}
		} else {
			if err := session.AdoptChatWorkspaceIdentity(session.Place{Dir: taken.Cell.Root}); err != nil {
				return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
			}
			taskCopies = c.restoreCopies(taken.Cell)
		}
		if err := c.sync.recordSendLease(taken.Cell.ID, sendLease{Cell: id, Fence: taken.Driving.Fence}); err != nil {
			return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
		}
		if c.sync.teamsSharing() {
			if err := (teamKeys{c.sync.profile(), c.sync.Home, c.sync.Device.ID(), nil}).rebindChat(id, taken.Cell); err != nil {
				if c.opt.Notify != nil {
					c.opt.Notify("This chat continued here, but its teams could not be updated yet: " + err.Error())
				}
			}
		}
		recordStep("restore", "")
		phaseMu.Lock()
		shownElapsed = stepCursor.Sub(started)
		filesElapsed = shownElapsed
		openGets = sc.Gets()
		openRounds = dir.Count()
		if fetcher != nil {
			openRounds += fetcher.OpenRounds()
		}
		phaseMu.Unlock()
		out = Continued{LocalOnly: storageOff, Taken: taken, Device: c.opt.DeviceName, TaskCopies: taskCopies, Resume: c.arrive(taken.Cell, resolveFrom()), Shown: shownElapsed, Files: filesElapsed, Wait: func(context.Context) error { return nil }, Retry: func() bool { return false }}
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
		return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
	}
	if err := cellstore.NotePublished(taken.Cell, taken.Driving.Head); err != nil {
		return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
	}
	var filed func()
	if taken.Cell, filed, err = c.fileCopy(ctx, taken.Cell, keepRoot); err != nil {
		if filed != nil {
			filed()
		}
		return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
	}
	taken.Driving.Cell = taken.Cell
	if err := os.MkdirAll(workspaceOf(taken.Cell.Root), 0o700); err != nil {
		if filed != nil {
			filed()
		}
		return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
	}
	if workspacePlan == nil {
		if err := session.AdoptChatWorkspaceIdentity(session.Place{Dir: taken.Cell.Root}); err != nil {
			if filed != nil {
				filed()
			}
			return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
		}
	}
	if err := c.sync.recordSendLease(taken.Cell.ID, sendLease{Cell: id, Fence: taken.Driving.Fence}); err != nil {
		if filed != nil {
			filed()
		}
		return Continued{}, errors.Join(err, c.giveBack(ctx, id, taken.Driving.Fence))
	}
	var keptTurns uint32
	if taken.Kept != "" && taken.Kept != preKept {
		keptTurns = branchedTurns
	}
	initialResume, initialStashed := c.fastResume(taken.Cell, fastFrom)
	recordStep("restore", "")
	phaseMu.Lock()
	shownElapsed = stepCursor.Sub(started)
	openGets = sc.Gets()
	openRounds = dir.Count()
	if fetcher != nil {
		openRounds += fetcher.OpenRounds()
	}
	phaseMu.Unlock()
	stallBound := func() time.Duration {
		if c.opt.IdleWithin > 0 {
			return c.opt.IdleWithin
		}
		return MoveIdleWithin
	}
	arrival := cell.BeginWorkspaceArrival(taken.Cell.Root, workspaceOf(taken.Cell.Root), stallBound, nil)
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

	syncEng := c.eng.Sync(cellstore.SyncKeys{CellKey: c.sync.Identity.CellKey(), Dedup: c.sync.Identity.DedupSecret()}, c.sync.Ledger)
	var bgMu sync.Mutex
	var bgRunning bool
	var bgDone chan struct{}
	var bgErr error
	var finalTaskCopies []string
	var finalResume preflight.Resume

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
			if initial {
				runCtx, runWatch = ctx, watch
				runStop = func() {
					stopWatch()
					cancelMove(nil)
				}
			} else {
				runCtx, runWatch, runStop = watchMove(context.Background(), c.opt.OnMove, c.opt.IdleWithin)
				if runWatch != nil {
					runWatch.setArrival(arrival)
					runWatch.setArriving()
				}
			}
			fetchDone := handoff.Trace(c.opt.OnPhase, "fetch")
			fetchErr := taker.Fetch.Complete(runCtx, taken.Cell, taken.Driving.Head)
			fetchDone()
			if fetchErr != nil {
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

			for _, hook := range []func(context.Context, cell.Cell) error{restoreChatFiles, rebuildIndexes, c.pullVault} {
				hookDone := handoff.Trace(c.opt.OnPhase, "hooks")
				hErr := hook(runCtx, taken.Cell)
				hookDone()
				if hErr != nil && c.opt.Notify != nil {
					c.opt.Notify("This chat continued here, but part of its state could not be updated yet: " + hErr.Error())
				}
			}

			filingDone := handoff.Trace(c.opt.OnPhase, "filing")
			var copies []string
			if workspacePlan != nil {
				var rErr error
				copies, rErr = session.RestoreTaskWorkspaceSets(runCtx, session.Place{Dir: taken.Cell.Root, Workspace: workspaceOf(taken.Cell.Root)}, c.opt.WorkspaceRoots)
				if rErr != nil && c.opt.Notify != nil {
					c.opt.Notify("a task's working copy could not be set up again here: " + rErr.Error())
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
					if c.opt.Notify != nil {
						c.opt.Notify("This chat continued here, but its teams could not be updated yet: " + tErr.Error())
					}
				}
			}
			stashPath := filepath.Join(taken.Cell.Root, "resume.pending")
			_, stashStillThere := os.Stat(stashPath)
			res := c.arrive(taken.Cell, actualFrom)
			if initialStashed && os.IsNotExist(stashStillThere) {
				_ = os.Remove(stashPath)
			}
			c.eng.RecordMaterializedStat(taken.Cell, taken.Driving.Head)
			if taken.Kept != "" && taken.Kept != preKept && keptTurns == 0 {
				keptTurns = c.orphanTurns(runCtx, taken.Kept)
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
	}
	if c.opt.OnShown != nil {
		c.opt.OnShown(out)
	}
	startBg(true)

	runAsync := (c.opt.OnShown != nil || c.opt.AsyncFiles) && c.eng.HasComposedCellDir(taken.Cell)
	if !runAsync {
		if waitErr := waitFn(parent); waitErr != nil {
			if filed != nil {
				filed()
			}
			if arriveRootFence != nil {
				arriveRootFence()
			}
			return Continued{}, errors.Join(waitErr, c.giveBack(parent, id, taken.Driving.Fence))
		}
		bgMu.Lock()
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

func (r *holderRetirer) isWorkingWriter() bool {
	holder, ok := session.ReadHolder(r.root, time.Now())
	return ok && holder.State == session.PresenceWorking
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
		if c.opt.Notify != nil {
			c.opt.Notify("task working copies were preserved but could not be rebound here: " + planErr.Error())
		}
		return nil
	}
	restored, err := taskcopy.Carry{Cutter: session.TaskCopyCutter{Place: session.Place{Dir: at.Root}}}.RestoreWithParents(at, workspaceOf(at.Root), parents)
	if err != nil && c.opt.Notify != nil {
		c.opt.Notify("a task's working copy could not be set up again here: " + err.Error())
	}
	if adoptionErr := session.AdoptTaskWorkspaceIdentities(session.Place{Dir: at.Root}, workspaceOf(at.Root), restored); adoptionErr != nil && c.opt.Notify != nil {
		c.opt.Notify("a restored task's workspace identity could not be rebound here: " + adoptionErr.Error())
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

func preserveExistingSubdirs(oldCellDir, newCellDir string) {
	entries, err := os.ReadDir(oldCellDir)
	if err != nil {
		return
	}
	_ = os.MkdirAll(newCellDir, 0o700)
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
			_ = os.Rename(src, dst)
			continue
		}
		if subEntries, err := os.ReadDir(dst); err == nil {
			for _, se := range subEntries {
				_ = os.Rename(filepath.Join(dst, se.Name()), filepath.Join(src, se.Name()))
			}
			_ = os.RemoveAll(dst)
			_ = os.Rename(src, dst)
		}
	}
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

// engineLocal is the Taker's view of a copy of the chat this machine has: what
// the engine says differs from the newest turn, and a seal of it.
type engineLocal struct {
	eng             cellstore.Engine
	ctx             context.Context
	alreadyBranched string
	cache           *localWorkCache
	retireWait      func(context.Context)
}

func (l engineLocal) localWork(ctx context.Context, c cell.Cell) (bool, error) {
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
	// A retired session can seal unchanged bookkeeping after losing its
	// hold. Only work beyond the shared snapshot belongs in a kept branch.
	work, err := l.localWork(l.ctx, c)
	if err != nil || !work {
		return "", 0, err
	}
	if l.retireWait != nil {
		l.retireWait(l.ctx)
	}
	return head, turns, nil
}

func (l engineLocal) Dirty(ctx context.Context, c cell.Cell) (bool, error) {
	if l.alreadyBranched != "" {
		if head, err := cellstore.Head(c); err == nil && head != nil && head.Turn.ID == l.alreadyBranched && cellstore.Unsaved(c) == 0 {
			if proof, err := l.eng.SavedState(ctx, c); err == nil && proof != nil && len(proof.Changes) == 0 {
				return false, nil
			}
		}
	}
	// Both appended accounting and edits inside an older message must be checked
	// against the shared snapshot, even when the current journal has the same size.
	return l.localWork(ctx, c)
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
	sealed, err := l.eng.SealRecovered(ctx, opened)
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
func restoreChatFiles(_ context.Context, c cell.Cell) error {
	_, err := chatfiles.Carry{}.Restore(c)
	return err
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

// pullVault brings this identity's secrets here, and with them the files the
// chat's seals withheld, which are written back into the folder the tools run in.
func (c *Continuer) pullVault(ctx context.Context, at cell.Cell) error {
	opened, err := cell.OpenAt(at.Root, at.ID)
	if err != nil {
		return err
	}
	files, err := c.sync.files(c.eng, opened)
	if err != nil {
		return err
	}
	return c.sync.withVaultWithin(ctx, vaultBusyWait, func(v vaultsync.Syncer) error { return v.Pull(ctx) }, files)
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
// sealed under the metadata key, and who can open it.
func (s *Sync) publishInfo(c cell.Cell, title string) cellsync.PublishInfo {
	in := cellsync.PublishInfo{
		Class: string(c.Meta().Class),
		Keys:  map[string]map[string]string{c.Meta().CellKeyID: {s.Identity.ID(): ""}},
	}
	if title != "" {
		in.Title, _ = directory.SealTitle(directory.MetadataKey(s.Identity.CellKey()), title)
	}
	if folder := chatFolder(c.Root); folder != "" {
		in.Folder, _ = directory.SealTitle(directory.MetadataKey(s.Identity.CellKey()), folder)
	}
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
