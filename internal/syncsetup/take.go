package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellindex"
	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

// TakeOptions is what the surface tells the take side about this machine.
type TakeOptions struct {
	// DeviceName is this machine's name: the kept-edits sentence names it.
	DeviceName string
	// RootFor is where this machine keeps the chat with an id: the folder it
	// already has, or the one a new copy gets.
	RootFor func(id string) string
	// Notify hears the one sentence the vault hook says about what it left alone.
	Notify func(string)
}

// Continued is a takeover that worked, with what the surface says about it.
type Continued struct {
	Taken handoff.Taken
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
}

// Continuer is the take side of one machine: `continue here`.
type Continuer struct {
	sync *Sync
	eng  cellstore.Engine
	opt  TakeOptions
}

// Continuer builds the take side over eng, the engine every chat this machine
// continues is sealed and restored by. It is the real Fetcher over the real
// SyncEngine, and the After hooks bring the chat's derived files and its
// secrets to the folder the tree landed in.
func (s *Sync) Continuer(eng cellstore.Engine, opt TakeOptions) *Continuer {
	eng.WorkspaceOf = func(c cell.Cell) string { return workspaceOf(c.Root) }
	return &Continuer{sync: s, eng: eng, opt: opt}
}

// taker is the Taker for chat id. It is built per takeover because the store
// it fetches through is counted under the chat it serves, and so is the branch
// that keeps this machine's unsent edits: both are the cost of taking this chat.
func (c *Continuer) taker(sc *Scope) handoff.Taker {
	s := c.sync
	sync := c.eng.Sync(cellstore.SyncKeys{CellKey: s.Identity.CellKey(), Dedup: s.Identity.DedupSecret()}, s.Ledger)
	engine := landing{Engine: sync}
	pub := &cellsync.Publisher{Engine: engine, Store: sc.Store, Dir: s.Dir}
	// THE TAKEOVER'S BRANCHER RUNS WITH Map NIL. The chat being taken keeps its
	// id on this machine; only the edits set aside live in the branch, and a
	// mapping from the chat to that branch would make the chat open as the
	// branch. (The drive side's brancher records the mapping, because there the
	// local chat does become the branch.)
	brancher := cellsync.Brancher{Dir: s.Dir, Publisher: pub, NewID: newCellID}
	return handoff.Taker{
		Dir:     s.Dir,
		Fetch:   &cellsync.Fetcher{Engine: engine, Store: sc.Store, Dir: s.Dir, Inbox: sync.Inbox},
		Local:   engineLocal{c.eng},
		Branch:  c.branch(brancher),
		RootFor: c.opt.RootFor,
		InPlace: func(c cell.Cell) bool { return borrowsProject(c.Root) },
		After:   []func(context.Context, cell.Cell) error{rebuildIndexes, recordOrigin, c.pullVault},
	}
}

// Take continues chat id here. Everything the surface says about it comes back
// with it.
func (c *Continuer) Take(ctx context.Context, id string) (Continued, error) {
	from := c.holderName(ctx, id)
	sc := c.sync.scope(id)
	defer sc.Settle()
	taken, err := c.taker(sc).Take(ctx, id)
	if err != nil {
		return Continued{}, err
	}
	if err := cellstore.AdoptedFrom(taken.Cell, from); err != nil {
		return Continued{}, err
	}
	out := Continued{Taken: taken, Device: c.opt.DeviceName, TaskCopies: c.restoreCopies(taken.Cell), Resume: c.arrive(taken.Cell, from)}
	if taken.Kept != "" {
		out.KeptTurns = c.orphanTurns(ctx, taken.Kept)
	}
	return out, nil
}

// arrive is the look at this machine that a takeover ends with, taken before
// anything here seals over the record the chat arrived with. The record is not a
// gate: a look that could not be kept still offers what it found.
func (c *Continuer) arrive(at cell.Cell, from string) preflight.Resume {
	resume, _ := preflight.Arrive(at.Root, workspaceOf(at.Root), from)
	return resume
}

// restoreCopies puts the task working copies the chat carried back to work in
// its project and answers which came back. A copy that cannot be cut again
// still has its files in place, so the takeover carries on and says so on the
// same desk as the vault's sentence.
func (c *Continuer) restoreCopies(at cell.Cell) []string {
	restored, err := taskcopy.Carry{Cutter: session.TaskCopyCutter{Place: session.Place{Dir: at.Root}}}.Restore(at, workspaceOf(at.Root))
	if err != nil && c.opt.Notify != nil {
		c.opt.Notify("a task's working copy could not be set up again here: " + err.Error())
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
	return func(ctx context.Context, from *cellsync.Driving, head string, turns uint32) (string, error) {
		opened, err := cell.OpenAt(from.Cell.Root, from.Cell.ID)
		if err != nil {
			return "", err
		}
		return b.Branch(ctx, from, head, turns, c.sync.publishInfo(opened, titleOf(opened.Root)))
	}
}

// landing is the engine for a chat that has not landed on this machine yet: the
// engine works from inside the folder it restores into, so the folders are
// made before it is asked what it is missing.
type landing struct{ cellsync.Engine }

func (l landing) Want(ctx context.Context, c cell.Cell, head string) ([]string, error) {
	if err := os.MkdirAll(workspaceOf(c.Root), 0o700); err != nil {
		return nil, err
	}
	return l.Engine.Want(ctx, c, head)
}

// engineLocal is the Taker's view of a copy of the chat this machine has: what
// the engine says differs from the newest turn, and a seal of it.
type engineLocal struct{ eng cellstore.Engine }

func (l engineLocal) Dirty(ctx context.Context, c cell.Cell) (bool, error) {
	return l.eng.Dirty(ctx, c)
}

func (l engineLocal) Follow(ctx context.Context, from, to cell.Cell) error {
	return l.eng.Follow(ctx, from, to)
}

// Seal seals the tree as one turn of the copy. The Taker names a cell by id and
// folder only, and a seal needs the cell's own record, so it is opened here.
func (l engineLocal) Seal(ctx context.Context, c cell.Cell) (string, uint32, error) {
	opened, err := cell.OpenAt(c.Root, c.ID)
	if err != nil {
		return "", 0, err
	}
	sealed, err := l.eng.Seal(ctx, opened, cellstore.TurnInfo{})
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
// one of the person's folders and that folder is here.
func projectOf(root string) (string, bool) {
	m, err := session.LoadMeta(root)
	return m.Workspace, err == nil && !m.Owned && dirExists(m.Workspace)
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
	if origin == "" || origin == workspaceOf(c.Root) {
		return nil
	}
	m, err := session.LoadMeta(c.Root)
	if err != nil || m.ID == "" {
		return err
	}
	m.Origin = origin
	return session.SaveMeta(c.Root, m)
}

func titleOf(root string) string {
	m, _ := session.LoadMeta(root)
	return m.Title
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
	return c.sync.withVault(func(v vaultsync.Syncer) error { return v.Pull(ctx) }, files)
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
	v, err := keys.Open(s.Home)
	if err != nil {
		return err
	}
	sc := s.scope(cellstats.VaultScope)
	defer sc.Settle()
	return use(vaultsync.Syncer{Store: sc.Store, Dir: s.Dir, Vault: v, CellKeyID: s.Identity.CellKeyID(),
		Carry: append(s.carried(), extra...)})
}

// profile is the directory the product reads config.json and credentials.json
// from, the same one its own stores resolve.
func (s *Sync) profile() string {
	if s.ProfileDir != "" {
		return s.ProfileDir
	}
	return s.Home
}

// carried is the profile state that travels in the vault beside the secrets.
func (s *Sync) carried() []vaultsync.Carrier {
	dir := s.profile()
	return []vaultsync.Carrier{vaultsync.Credentials(filepath.Join(dir, vaultsync.CredentialsFile)), providerKeys(dir, s.Home)}
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
	return in
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
	if v.Cell.Lease.Device == s.Device.ID() {
		d.Fence, d.Head = v.Cell.Lease.Fence, v.Cell.Head
	}
	return d, nil
}
