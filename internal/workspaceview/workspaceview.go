// Package workspaceview reads a collection's typed references back through the
// owners that already keep those records, and says what each one currently is.
//
// internal/workspace deliberately knows nothing about conversations, work,
// standing orders or files: a membership is an address and never a copy of the
// thing addressed. That is the right boundary and this package does not move
// it. What it adds is the OTHER HALF a surface needs — a list of addresses is
// not a list a person can read, because the one question they ask of a folder
// is what is in it and how it is doing.
//
// Four laws shape what it answers with.
//
//   - IT IS A READ AND NOTHING ELSE. Nothing here opens an execution owner,
//     builds an agent, replays a journal, takes a lock it keeps or writes a
//     byte. The whole layer is one reading of the world (session/world.go,
//     itself four system calls per conversation), one query for collection
//     names, one document per standing order asked for, and one stat per
//     artifact. A person can ask it on a keystroke.
//
//   - IT IS NOT A SECOND OWNER OF ANY STATE. Every word this package puts in
//     [Record.State] comes out of a projection the runtime already keeps —
//     [session.ProjectTask] for work, [session.SessionRow.Doing] for a
//     conversation, [standing.Item.Status] for a standing order. A second
//     spelling of "what is this doing" is the one defect a view like this can
//     introduce, and it would be invisible until two screens disagreed.
//
//   - MISSING, UNAVAILABLE AND BROKEN ARE THREE ANSWERS. A reference whose
//     record is not here comes back as a Record with its reference intact and
//     [Record.Unavailable] saying why, because a folder that hides a chat
//     somebody deleted is a folder that lost their membership too. A reference
//     this reader was not WIRED to resolve is an error, not a shrug: a
//     capability with nothing behind it must be absent rather than quietly
//     wrong. An invalid reference is an error before any file is opened.
//
//   - AN ABSENCE IN AN INDEX IS NOT A DELETION. The project's task record is
//     append-only, keeps only its newest rows, skips a line it cannot parse and
//     takes no row at all for work that has not landed. So "I cannot find it"
//     is the only claim this package will make about a task it cannot find, and
//     [taskMissing] says exactly that in the sentence a person reads.
//
// It imports the runtime and is never imported by it. internal/session must not
// depend on this package — the direction is what keeps organization optional —
// so the world arrives as a callback the caller supplies ([Resolver.World]).
package workspaceview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// The three wiring faults. They are errors and not [Record.Unavailable]
// sentences because they are not facts about the person's machine: a caller
// that hands over no way to read conversations and then asks about one has a
// hole in its assembly, and a Record saying "not here" would hide it behind a
// sentence about their work.
var (
	ErrNoConversations = errors.New("workspaceview: no reading of this machine's conversations was supplied")
	ErrNoStandingStore = errors.New("workspaceview: no standing store was supplied")
	ErrNoCollections   = errors.New("workspaceview: no collection store was supplied")
)

// Record is one reference and what its owner says about it right now.
//
// Every field except Ref may be empty, and empty always means NOBODY COULD SAY
// rather than a zero: a conversation nothing ever named has no title, a landed
// task is in no phase, a row written by an older build carries no ground. A
// surface draws nothing for them (the emptiness law) and never a placeholder.
type Record = workspace.ResolvedRef

// Resolver is the reading. Both of its fields are supplied by whoever assembles
// it, and either may be nil: a caller that resolves only artifacts needs
// neither, and asking for something a nil field owns is the wiring fault above
// rather than a silent empty answer.
type Resolver struct {
	// World reads every conversation and every project's task record on this
	// machine. IT IS A CALLBACK AND NOT A CALL so that internal/session never
	// has to know this package exists; the runtime hands over its own reader
	// (session.ReadHome, or a reading it already took this tick).
	World func() session.World
	// Standing is the store the standing orders are kept in, already open. This
	// package only ever asks it [standing.Store.Get], once per standing
	// reference it was asked about — never List, which would read every document
	// a person owns to answer a question about one.
	Standing *standing.Store
}

// Resolve answers one Record per reference, IN THE ORDER THEY WERE GIVEN.
//
// The order is the caller's and is never sorted here: a collection's membership
// is an insertion order the person can see and rearrange (internal/workspace
// keeps it), and a view that re-ordered it would be a second arrangement of
// their folder that they never made.
//
// store may be nil when no collection reference is among refs. Every reference
// is validated before anything is opened, so one malformed address costs no
// reads at all.
func (r Resolver) Resolve(ctx context.Context, store *workspace.Store, refs []workspace.Ref) ([]Record, error) {
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	read := &reading{resolver: r, store: store}
	records := make([]Record, 0, len(refs))
	for _, ref := range refs {
		// CHECKED PER RECORD, BECAUSE EACH ONE COSTS A FILE. A cancelled context
		// stops the walk where it stands rather than finishing a hundred stats
		// for an answer nobody is waiting for any more.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := read.one(ctx, ref)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// Members is Resolve over one collection's membership, in the collection's own
// order. A collection that is not in the store answers [workspace.ErrNotFound],
// which is the store's own answer carried straight through — this layer does
// not turn a missing folder into an empty one.
func (r Resolver) Members(ctx context.Context, store *workspace.Store, id string) ([]Record, error) {
	if store == nil {
		return nil, ErrNoCollections
	}
	refs, err := store.Members(ctx, id)
	if err != nil {
		return nil, err
	}
	return r.Resolve(ctx, store, refs)
}

// reading is one call's worth of state. Everything in it is read LAZILY AND AT
// MOST ONCE: a folder of ten artifacts never reads the world, a folder of ten
// conversations reads it once for all of them, and a folder of neither never
// asks the collection store for its names.
type reading struct {
	resolver Resolver
	store    *workspace.Store

	// sessions is the world's conversations by id, built the first time one is
	// asked for. A nil map with taken set is a machine that has held none.
	sessions map[string]session.SessionRow
	taken    bool

	// names is every collection's name by id, and read for the same reason.
	names     map[string]string
	namesRead bool
}

func (d *reading) one(ctx context.Context, ref workspace.Ref) (Record, error) {
	switch ref.Kind {
	case workspace.CollectionKind:
		return d.collection(ctx, ref)
	case workspace.ConversationKind:
		return d.conversation(ref)
	case workspace.TaskKind:
		return d.task(ref)
	case workspace.StandingKind:
		return d.standingOrder(ref)
	case workspace.ArtifactKind:
		return artifact(ref), nil
	}
	// Unreachable: every reference was validated above, and Validate's own
	// switch is the closed list this one mirrors.
	return Record{}, fmt.Errorf("%w: unknown reference kind %q", workspace.ErrInvalid, ref.Kind)
}

// ── the five owners ─────────────────────────────────────────────────────────

func (d *reading) collection(ctx context.Context, ref workspace.Ref) (Record, error) {
	names, err := d.collectionNames(ctx)
	if err != nil {
		return Record{}, err
	}
	name, ok := names[ref.ID]
	if !ok {
		return unavailable(ref, "No collection here has that id."), nil
	}
	// A collection is organization and has no place on disk, no state and no
	// phase. Three empty fields is the honest answer, not a gap to fill.
	return Record{Ref: ref, Title: name, Available: true}, nil
}

func (d *reading) conversation(ref workspace.Ref) (Record, error) {
	row, ok, err := d.session(ref.ID)
	if err != nil {
		return Record{}, err
	}
	if !ok {
		return unavailable(ref, "That conversation is not on this machine."), nil
	}
	return Record{
		Ref:   ref,
		Title: row.Title,
		// The conversation's OWN word for itself, and "" for one nothing is
		// holding — a claim nobody has refreshed is not a claim about now.
		State:     row.Doing(),
		Location:  row.ProjectDir,
		Available: true,
	}, nil
}

// task resolves one node by the pair that identifies it — the conversation that
// ran it and its number inside that conversation. TASK NUMBERS RESTART IN EVERY
// CONVERSATION, which is why the session is half of the address and why work 1
// in two chats is two different pieces of work here.
func (d *reading) task(ref workspace.Ref) (Record, error) {
	row, ok, err := d.session(ref.SessionID)
	if err != nil {
		return Record{}, err
	}
	if !ok {
		return unavailable(ref, "The conversation that ran this work is not on this machine."), nil
	}
	for _, entry := range row.Tasks.Rows {
		if strings.TrimSpace(entry.ID) != ref.ID {
			continue
		}
		return taskRecord(ref, row, entry), nil
	}
	// The record has no row for it. That is not the same fact as the work never
	// having existed — see [taskMissing] — and if the conversation is live and
	// still names the node among the work it has out, we know better than the
	// file does and say so.
	if row.Live && row.Presence.Holds(ref.ID) {
		return livingTaskRecord(ref, row), nil
	}
	return unavailable(ref, taskMissing(ref.ID)), nil
}

// taskRecord projects one row of the project's record.
//
// THE PROJECTION IS THE RUNTIME'S OWN AND NOT A SECOND ONE.
// [session.SessionRow.Runs] is the single place a live-looking row is judged
// against the conversation that would have to be behind it, and
// [session.ProjectTask] is the single place engine facts become the word a
// person reads. This function's whole job is to carry the facts from one to the
// other; if it grew a `switch entry.Status` of its own, this package would have
// become the second owner of task state.
func taskRecord(ref workspace.Ref, row session.SessionRow, entry session.TaskIndexEntry) Record {
	facts := entry.StatusFacts(row.Runs(entry))
	facts.Life = row.Phase(entry)
	status := session.ProjectTask(facts)
	return Record{
		Ref:       ref,
		Title:     entry.Title,
		State:     string(status.Presence),
		Phase:     row.Phase(entry),
		Location:  taskLocation(entry),
		Available: true,
	}
}

// livingTaskRecord answers for a node the conversation is holding right now and
// the project's record has not taken a row for yet.
//
// IT IS THE SAME PROJECTION OVER THE SAME FACTS FROM THE OTHER FILE. Presence
// and the index spell a node's id and state identically on purpose
// (taskpresence.go says so), and [session.ReadSessionPresence] has already
// refused a claim too old to believe — so a caller reaching here is holding a
// fresh statement by the process that owns the work. Answering "not found"
// instead would be this layer preferring a file that has not been written yet
// over a conversation that is talking.
func livingTaskRecord(ref workspace.Ref, row session.SessionRow) Record {
	var task session.PresenceTask
	for _, out := range row.Presence.RunningTasks {
		if strings.TrimSpace(out.ID) == ref.ID {
			task = out
			break
		}
	}
	status := session.ProjectTask(session.TaskFacts{
		State:    session.TaskState(task.State),
		Life:     task.Phase,
		Liveness: session.TaskLivenessHeld,
	})
	return Record{
		Ref:   ref,
		Title: task.Title,
		State: string(status.Presence),
		Phase: task.Phase,
		// Where the work is happening is the record's to say, and it has not
		// said yet. Nothing is drawn rather than the conversation's own folder,
		// which would be a guess.
		Available: true,
	}
}

// taskLocation is where the work WENT, which is the question a person asks of a
// row in their own history — the repository or folder it was about, and the
// task's own directory only when the row is too old to name a ground. Both are
// absent on rows written before those fields existed, and absence is unknown.
func taskLocation(entry session.TaskIndexEntry) string {
	if ground := strings.TrimSpace(entry.Ground); ground != "" {
		return ground
	}
	return strings.TrimSpace(entry.Where)
}

// taskMissing is the sentence for work the project's record does not name, and
// every clause of it is there to stop one wrong conclusion.
//
// THE RECORD IS NOT A CENSUS. It is append-only and takes a row when work
// LANDS, it keeps only its newest rows, and it skips a line two windows
// interleaved. So a task that is not in it may be running somewhere this
// reading cannot see, may have aged out, or may have been lost to a half-written
// line — and a view that said "deleted" would be inventing an event nobody
// recorded.
func taskMissing(id string) string {
	return fmt.Sprintf("This conversation's project record does not name work %s. "+
		"Work that has not landed is not in it yet and only its newest rows are kept, "+
		"so nothing here says it was deleted.", id)
}

func (d *reading) standingOrder(ref workspace.Ref) (Record, error) {
	if d.resolver.Standing == nil {
		return Record{}, ErrNoStandingStore
	}
	item, err := d.resolver.Standing.Get(ref.ID)
	switch {
	case errors.Is(err, standing.ErrNotFound):
		return unavailable(ref, "No standing order with that id is kept here."), nil
	case err != nil:
		// A document that is there and cannot be read is a DIFFERENT fact from
		// one that is gone — a newer build wrote it, or a full disk truncated it
		// — and a person who deletes the membership on the strength of "not
		// found" would be throwing away a live standing order.
		return unavailable(ref, "That standing order could not be read: "+err.Error()), nil
	}
	return Record{
		Ref:   ref,
		Title: item.Title(),
		// The item's own word. Whether a pass has it in its hands this second,
		// and whether its last firing stopped on a question, are facts the store
		// keeps elsewhere and this reading does not ask for.
		State:     string(item.Status),
		Location:  item.Workspace,
		Available: true,
	}, nil
}

// artifact is a stat and nothing more. The reference is an absolute path, which
// is not a claim of identity across a rename, a machine or a version — so what
// this can honestly answer is whether something is at that path today.
func artifact(ref workspace.Ref) Record {
	record := Record{Ref: ref, Title: filepath.Base(ref.ID), Location: filepath.Dir(ref.ID)}
	switch _, err := os.Stat(ref.ID); {
	case err == nil:
		record.Available = true
	case os.IsNotExist(err):
		record.Unavailable = "There is nothing at that path any more."
	default:
		// Unreadable is not absent: a folder whose permissions changed still
		// holds the person's file.
		record.Unavailable = "That path could not be read: " + err.Error()
	}
	return record
}

// ── the two readings, taken once ────────────────────────────────────────────

// session answers one conversation out of the world, reading the world the
// first time it is asked and never again in this call.
//
// THE INDEX IS BUILT NEWEST FIRST AND THE FIRST WINS, which is [World.Sessions]'
// own order. Two folders with one id is not a shape this machine writes, and
// resolving it by recency rather than by whichever directory walk came first is
// the answer that does not move between two readings of the same disk.
func (d *reading) session(id string) (session.SessionRow, bool, error) {
	if !d.taken {
		if d.resolver.World == nil {
			return session.SessionRow{}, false, ErrNoConversations
		}
		world := d.resolver.World()
		d.sessions = make(map[string]session.SessionRow)
		for _, row := range world.Sessions() {
			if _, seen := d.sessions[row.ID]; !seen {
				d.sessions[row.ID] = row
			}
		}
		d.taken = true
	}
	row, ok := d.sessions[id]
	return row, ok, nil
}

// collectionNames is every collection's name, read once per call. The whole
// list is cheaper than a query per reference and it is what the store already
// answers.
func (d *reading) collectionNames(ctx context.Context) (map[string]string, error) {
	if d.namesRead {
		return d.names, nil
	}
	if d.store == nil {
		return nil, ErrNoCollections
	}
	collections, err := d.store.Collections(ctx)
	if err != nil {
		return nil, err
	}
	d.names = make(map[string]string, len(collections))
	for _, collection := range collections {
		d.names[collection.ID] = collection.Name
	}
	d.namesRead = true
	return d.names, nil
}

// unavailable keeps the reference whole. THE MEMBERSHIP SURVIVES ITS RECORD
// GOING MISSING, which is internal/workspace's own rule — external availability
// is not a condition of keeping a reference — and this is that rule said on the
// way back out.
func unavailable(ref workspace.Ref, why string) Record {
	return Record{Ref: ref, Unavailable: why}
}
