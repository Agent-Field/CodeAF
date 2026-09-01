package furrow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ── restore points ───────────────────────────────────────────────────────────

// snapshotsDefault and snapshotsMax bound one look at the timeline. They are
// this package's numbers rather than furrow's — furrow's own default is
// whatever it is, and this package always passes --limit explicitly so that the
// number promised in [SnapshotsSchemaJSON] is the number the person gets. THE
// SCHEMA AND THE CALL READ THE SAME TWO CONSTANTS; a schema advertising a
// figure the code does not apply is the drift this repository has been bitten
// by before.
const (
	snapshotsDefault = 20
	snapshotsMax     = 100
)

// Snapshot is one sealed restore point on the workspace timeline: a moment the
// whole folder can be put back to, byte for byte.
type Snapshot struct {
	// ID is furrow's own snapshot id, and the only thing a restore will accept.
	ID string

	// SealedAt is when furrow sealed it. This is THE ALIGNMENT KEY between the
	// two kinds of rewind aforge has: the conversation's cut points live in
	// internal/session and know nothing about the workspace, so pairing them is
	// done on the clock and on nothing else (see [Workspace.PointNear]).
	SealedAt time.Time

	// Label is what the seal was called, when it was called anything. A seal
	// furrow made on its own has none; a seal made through [Workspace.Mark]
	// carries the turn it belonged to.
	Label string

	// Trigger is furrow's word for why the seal happened, and Grade is furrow's
	// declaration of how exactly this snapshot can be materialized again —
	// fidelity is declared and never implied, which is furrow's own rule and
	// worth carrying rather than flattening.
	Trigger string
	Grade   string

	// Pinned reports that the person has held this snapshot exact against
	// timeline thinning, so it will still be there later.
	Pinned bool
}

// Short is the snapshot id the way furrow's own timeline prints it — the first
// twelve characters. It is for a person to read and never for a call to pass;
// furrow resolves prefixes, but this package hands back the whole id so that
// nothing it returns can resolve to the wrong snapshot later.
func (s Snapshot) Short() string {
	if len(s.ID) <= 12 {
		return s.ID
	}
	return s.ID[:12]
}

// Snapshots is the workspace timeline, newest first, as furrow ordered it.
func (w *Workspace) Snapshots(ctx context.Context, limit int) ([]Snapshot, error) {
	if limit <= 0 {
		limit = snapshotsDefault
	}
	if limit > snapshotsMax {
		limit = snapshotsMax
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	stdout, stderr, err := w.run(ctx, "--json", "timeline", "--limit", strconv.Itoa(limit))
	if err != nil && len(documents(stdout)) == 0 {
		return nil, failure(stderr, err)
	}
	var rows []struct {
		ID       string  `json:"id"`
		SealedAt int64   `json:"sealed_at"`
		Label    *string `json:"label"`
		Trigger  string  `json:"trigger"`
		Pinned   bool    `json:"pinned"`
		// Materialization is furrow's fidelity declaration for this snapshot.
		// Only its grade is read: the missing-path detail underneath it is
		// furrow's to present in `furrow status --fidelity`, and copying it
		// into a tool result would be aforge paraphrasing a contract it does
		// not own.
		Materialization struct {
			Grade string `json:"grade"`
		} `json:"materialization"`
	}
	if err := decodeLast(stdout, &rows); err != nil {
		return nil, err
	}
	snapshots := make([]Snapshot, 0, len(rows))
	for _, row := range rows {
		snapshot := Snapshot{
			ID:       row.ID,
			SealedAt: time.Unix(row.SealedAt, 0),
			Trigger:  row.Trigger,
			Grade:    row.Materialization.Grade,
			Pinned:   row.Pinned,
		}
		if row.Label != nil {
			snapshot.Label = *row.Label
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

// PointNear is the aligned-rewind seam: the newest restore point sealed at or
// before a moment, which is the workspace state a conversation cut at that
// moment was looking at.
//
// It takes a time and not a rewind point because THE TWO REWINDS DO NOT SHARE A
// VOCABULARY AND SHOULD NOT. internal/session's RewindPoint is an index into a
// transcript; furrow's snapshot is a content hash of a folder; the only thing
// both ends genuinely agree on is the clock. Keeping the join here, on one
// argument, means the conversation's rewind stays exactly what its own package
// says it is — an edit of the conversation, never of the workspace — and this
// package is the only place that ever offers to move the second one too.
//
// Reported false when the timeline reaches no further back than the moment
// asked about, which is the ordinary answer for a folder attached to furrow
// only recently: there is no restore point from before furrow was watching, and
// saying so is better than offering the oldest one there is.
func (w *Workspace) PointNear(ctx context.Context, when time.Time) (Snapshot, bool, error) {
	snapshots, err := w.Snapshots(ctx, snapshotsMax)
	if err != nil {
		return Snapshot{}, false, err
	}
	var best Snapshot
	found := false
	for _, snapshot := range snapshots {
		if snapshot.SealedAt.After(when) {
			continue
		}
		if !found || snapshot.SealedAt.After(best.SealedAt) {
			best, found = snapshot, true
		}
	}
	return best, found, nil
}

// Mark seals the workspace now and attributes the seal to a turn, so that a
// conversation rewound later has a workspace state to be offered beside it.
//
// It is `furrow hook turn-end`, which is the same door `furrow hook install`
// wires a generic harness into — called directly instead, because aforge knows
// its own turn boundaries and does not need a shell adapter to tell it about
// them. The label furrow writes is its own: `hook turn-end agent=<agent>
// turn=<turn>`.
//
// It is only ever called on a Workspace, and that matters: the command furrow
// runs underneath ATTACHES a folder it was not already watching, and attaching
// somebody's folder to a program because a turn ended is not a thing aforge may
// decide. [Open] having already said yes is what makes this safe.
func (w *Workspace) Mark(ctx context.Context, agent, turn string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	args := []string{"--json", "hook", "turn-end"}
	if agent = strings.TrimSpace(agent); agent != "" {
		args = append(args, "--agent", agent)
	}
	if turn = strings.TrimSpace(turn); turn != "" {
		args = append(args, "--turn", turn)
	}
	stdout, stderr, err := w.run(ctx, args...)
	if err != nil && len(documents(stdout)) == 0 {
		return "", failure(stderr, err)
	}
	var sealed struct {
		Snapshot string `json:"snapshot"`
	}
	if err := decodeLast(stdout, &sealed); err != nil {
		return "", err
	}
	return sealed.Snapshot, nil
}

// ── restoring ────────────────────────────────────────────────────────────────

// RestoreChange is one path a restore would touch, or did.
type RestoreChange struct {
	Path   string
	Action string
}

// Restore is what a restore preview or a restore says about itself.
type Restore struct {
	// Snapshot is the restore point aimed at, and Changes is every path the
	// restore covers — empty when the workspace already matches it.
	Snapshot string
	Changes  []RestoreChange

	// Applied distinguishes a preview from a restore that really happened.
	Applied bool

	// Undo is the snapshot furrow sealed of the CURRENT state immediately
	// before applying, set only when Applied. It is the reason a restore is not
	// a one-way door — furrow seals before it restores, so a restore is itself
	// rewindable — and a caller that does not offer it back to the person is
	// hiding the safest thing about the operation.
	Undo string
}

// PreviewRestore reports what restoring to a snapshot would change, and touches
// nothing. paths, when given, narrow the restore to those repository-relative
// paths — the `.env` back and newer work untouched.
func (w *Workspace) PreviewRestore(ctx context.Context, snapshot string, paths []string) (Restore, error) {
	return w.restore(ctx, snapshot, paths, false)
}

// ApplyRestore puts the workspace back, whole or by path.
//
// It passes furrow's `--yes`, which is not this package waving a gate through:
// furrow's gate is an explicit ID plus a confirmation, and the confirmation
// aforge is passing on is a person's, collected before this is ever called. The
// tool in tools.go is where that is enforced, and it is enforced by requiring a
// separate argument rather than by trusting a model to have meant it.
func (w *Workspace) ApplyRestore(ctx context.Context, snapshot string, paths []string) (Restore, error) {
	return w.restore(ctx, snapshot, paths, true)
}

func (w *Workspace) restore(ctx context.Context, snapshot string, paths []string, apply bool) (Restore, error) {
	snapshot = strings.TrimSpace(snapshot)
	if snapshot == "" {
		return Restore{}, fmt.Errorf("furrow: no restore point named")
	}
	for _, path := range paths {
		if filepath.IsAbs(path) {
			return Restore{}, fmt.Errorf("furrow: %q must be relative to the workspace", path)
		}
	}

	// Only the preview is bounded here. Putting a large workspace back is
	// furrow's work to finish, and cutting it off part-way is the one outcome
	// nobody wants; the caller's own context still governs.
	if !apply {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, readTimeout)
		defer cancel()
	}

	args := []string{"--json", "rewind", snapshot}
	if apply {
		args = append(args, "--yes")
	} else {
		args = append(args, "--dry-run")
	}
	for _, path := range paths {
		args = append(args, "--paths", path)
	}

	stdout, stderr, err := w.run(ctx, args...)
	found := documents(stdout)
	if err != nil && len(found) == 0 {
		return Restore{}, failure(stderr, err)
	}

	// An applied restore prints TWO documents: the plan it is about to carry
	// out, and then what it did. A restore with nothing to change prints only
	// the plan and stops — so the plan is read from the first document and the
	// outcome is looked for by name in the last, rather than by counting.
	var plan struct {
		Target  string `json:"target"`
		Changes []struct {
			Path   string `json:"path"`
			Action string `json:"action"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(found[0], &plan); err != nil {
		return Restore{}, fmt.Errorf("%w: %s", ErrUnreadable, err)
	}
	result := Restore{Snapshot: plan.Target}
	if result.Snapshot == "" {
		result.Snapshot = snapshot
	}
	for _, change := range plan.Changes {
		result.Changes = append(result.Changes, RestoreChange{Path: change.Path, Action: change.Action})
	}

	var applied struct {
		Restored string `json:"restored"`
		Undo     string `json:"pre_rewind_snapshot"`
	}
	if err := json.Unmarshal(found[len(found)-1], &applied); err == nil && applied.Undo != "" {
		result.Applied = true
		result.Undo = applied.Undo
		if applied.Restored != "" {
			result.Snapshot = applied.Restored
		}
	}
	if apply && !result.Applied && len(result.Changes) > 0 {
		// furrow was asked to apply, had changes to make, and printed no
		// account of making them. Reporting success here would be reporting a
		// restore that may not have happened, which is the one lie this package
		// must never tell.
		return result, fmt.Errorf("%w: furrow did not say whether the restore was applied", ErrUnreadable)
	}
	return result, nil
}

// ── universes ────────────────────────────────────────────────────────────────

// Fork is what running a command in its own universe came back with.
type Fork struct {
	// Name is the fork's stable name, and the only handle [Workspace.Merge]
	// takes. Path is where it was materialized.
	Name string
	Path string

	// Base and Head are the snapshot the universe started from and the one
	// furrow sealed of it when the command finished.
	Base string
	Head string

	// ExitCode is the command's own status. A non-zero one is not a furrow
	// failure and is never reported as one: the fork was made, the command ran,
	// and it said no.
	ExitCode int

	// Output is what the command printed, captured and capped. Under --json
	// furrow sends a universe's own stdout to stderr so the JSON stays clean,
	// which is why both streams end up here as one thing to read.
	Output string
}

// RunInFork materializes a copy-on-write universe of the whole workspace —
// files, dependencies, `.env`, the dev database, git's own mutable state — and
// runs one command inside it, leaving the real workspace untouched.
//
// The command goes to `/bin/sh -c`, which is the same shell furrow itself runs
// a merge check through, so a command that works in one works in the other.
//
// No timeout is imposed. The command's own runtime is the person's business and
// the caller's context is what carries their decision to stop; a ceiling
// invented here would be a second, quieter limit beside whichever one the
// caller already applies.
func (w *Workspace) RunInFork(ctx context.Context, name, command string) (Fork, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return Fork{}, fmt.Errorf("furrow: no command to run")
	}
	args := []string{"--json", "exec"}
	if name = strings.TrimSpace(name); name != "" {
		args = append(args, "--fork", name)
	}
	args = append(args, "--", "/bin/sh", "-c", command)

	stdout, stderr, err := w.run(ctx, args...)
	if err != nil && len(documents(stdout)) == 0 {
		return Fork{}, failure(stderr, err)
	}
	var run struct {
		Universes []struct {
			Fork     string `json:"fork"`
			Path     string `json:"path"`
			Base     string `json:"base_snapshot"`
			Head     string `json:"head_snapshot"`
			ExitCode int    `json:"exit_code"`
		} `json:"universes"`
	}
	if err := decodeLast(stdout, &run); err != nil {
		return Fork{}, err
	}
	if len(run.Universes) == 0 {
		return Fork{}, fmt.Errorf("%w: furrow reported no universe", ErrUnreadable)
	}
	// One command was asked for, so the first universe is the one. Reading
	// index zero rather than asserting a length of one is deliberate: a furrow
	// that grows a reason to report more must not turn this into an error.
	first := run.Universes[0]
	return Fork{
		Name:     first.Fork,
		Path:     first.Path,
		Base:     first.Base,
		Head:     first.Head,
		ExitCode: first.ExitCode,
		Output:   capped(stderr),
	}, nil
}

// Forks lists the universes that exist, with what each has changed.
func (w *Workspace) Forks(ctx context.Context) ([]Fork, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	stdout, stderr, err := w.run(ctx, "--json", "forks")
	if err != nil && len(documents(stdout)) == 0 {
		return nil, failure(stderr, err)
	}
	var rows []struct {
		Name string `json:"name"`
		Path string `json:"destination"`
		Base string `json:"base_snapshot"`
		Head string `json:"head_snapshot"`
	}
	if err := decodeLast(stdout, &rows); err != nil {
		return nil, err
	}
	forks := make([]Fork, 0, len(rows))
	for _, row := range rows {
		forks = append(forks, Fork{Name: row.Name, Path: row.Path, Base: row.Base, Head: row.Head})
	}
	return forks, nil
}

// MergeConflict is one path two universes disagree about.
type MergeConflict struct {
	Path string
	Kind string
}

// Merge is what landing a universe came back with.
type Merge struct {
	Fork string

	// Landed reports that the workspace really changed. It is false for a
	// preview, false when the check failed, and false when there were
	// conflicts — three different reasons for the same fact, which is why the
	// reason travels beside it rather than being inferred from it.
	Landed bool

	// Result is the snapshot the merge sealed, set only when Landed.
	Result string

	// Changes is how many paths the merge covers, and Conflicts is every path
	// it stopped on. A merge with conflicts changes nothing.
	Changes   int
	Conflicts []MergeConflict

	// CheckOutput is what the verification command printed, whether it passed
	// or failed. It is capped like every other captured stream.
	CheckOutput string

	// CheckFailed reports that a check was given and said no. NOTHING WAS
	// MERGED when it is true, which is the entire point of giving a check.
	CheckFailed bool
}

// MergeFork lands a universe's changes back into the workspace, verifying them
// first when a check is given: furrow materializes the merged result in a
// scratch workspace, runs the check there through `/bin/sh -c`, and lands
// nothing unless it passes.
//
// preview plans the merge and reports its changes and conflicts without
// materializing or checking anything.
func (w *Workspace) MergeFork(ctx context.Context, fork, check string, preview bool) (Merge, error) {
	fork = strings.TrimSpace(fork)
	if fork == "" {
		return Merge{}, fmt.Errorf("furrow: no universe named")
	}
	args := []string{"--json", "merge", fork}
	if check = strings.TrimSpace(check); check != "" {
		args = append(args, "--check", check)
	}
	if preview {
		args = append(args, "--dry-run")
	}

	stdout, stderr, err := w.run(ctx, args...)
	found := documents(stdout)
	if err != nil && len(found) == 0 {
		// A check that failed is this path, and it is the ordinary case rather
		// than a fault: furrow aborts before printing anything, with the check's
		// own status and output on stderr. It is reported as a Merge that did
		// not land and never as an error, because "your tests failed" is an
		// answer and not a breakage.
		if failed, ok := checkFailure(stderr); ok {
			return Merge{Fork: fork, CheckFailed: true, CheckOutput: capped(failed)}, nil
		}
		return Merge{}, failure(stderr, err)
	}

	var outcome struct {
		Fork        string     `json:"fork"`
		Result      *string    `json:"result_snapshot"`
		Changes     int        `json:"changes"`
		CheckOutput *string    `json:"check_output"`
		Conflicts   []conflict `json:"conflicts"`
	}
	if err := decodeLast(stdout, &outcome); err != nil {
		return Merge{}, err
	}
	merge := Merge{Fork: outcome.Fork, Changes: outcome.Changes}
	if merge.Fork == "" {
		merge.Fork = fork
	}
	if outcome.Result != nil && *outcome.Result != "" && !preview {
		merge.Landed = true
		merge.Result = *outcome.Result
	}
	if outcome.CheckOutput != nil {
		merge.CheckOutput = capped(*outcome.CheckOutput)
	}
	for _, one := range outcome.Conflicts {
		merge.Conflicts = append(merge.Conflicts, MergeConflict{Path: one.Path.String(), Kind: one.Kind})
	}
	return merge, nil
}

func checkFailure(stderr string) (string, bool) {
	const marker = "merge check failed"
	index := strings.Index(stderr, marker)
	if index < 0 {
		return "", false
	}
	return strings.TrimPrefix(strings.TrimSpace(stderr[index:]), "Error: "), true
}

// conflict decodes one entry of furrow's conflict list.
type conflict struct {
	Path pathBytes `json:"path"`
	Kind string    `json:"kind"`
}

// pathBytes is a path furrow serializes as a sequence of BYTES rather than as a
// string, because a repository path on Unix is bytes and need not be valid
// UTF-8. It accepts a JSON string as well, so that a furrow which one day
// decides to spell these the easy way costs this package nothing.
type pathBytes []byte

func (p *pathBytes) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		*p = []byte(text)
		return nil
	}
	var values []int
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	out := make([]byte, 0, len(values))
	for _, value := range values {
		out = append(out, byte(value))
	}
	*p = out
	return nil
}

func (p pathBytes) String() string { return string(p) }

// ── folder sync ──────────────────────────────────────────────────────────────

// SyncOffer is the pairing aforge OFFERS rather than performs: the two commands
// that put one folder on two machines, for the person to run themselves.
//
// AFORGE NEVER RUNS `furrow remote add` ITSELF, and the reason is one line of
// furrow's output. Pairing prints the workspace's recovery key — the only thing
// that can read this workspace's ciphertext anywhere — and a secret that passes
// through a tool result has been written into a transcript, a journal, and
// possibly a screen somebody else is looking at. So the offer is commands, the
// person runs them, and the key never enters aforge at all.
type SyncOffer struct {
	// Workspace is the folder being offered, and Name is the shared workspace
	// name suggested for both ends — furrow's own default, the folder's name.
	Workspace string
	Name      string

	// Here is what to run on the machine holding the folder, and There is what
	// to run on the machine that should receive it. There carries a
	// placeholder for the recovery key, because this package does not have one
	// and will not go looking.
	Here  []string
	There []string

	// Note is the honest edge, in furrow's own terms and aforge's law's terms
	// at once. It is not decoration: somebody who reads only this field should
	// still not be surprised later.
	Note string
}

// syncNote is the sentence a person must read before they pair two machines.
// Divergence is furrow's declared honest edge — cross-machine changes are
// preserved and reported, never merged for you — and it happens to be exactly
// this wave's own law about a connected session: the engine machine is the one
// that writes.
const syncNote = "Changes made on both machines at once are kept and reported, never merged for you — so let one machine be the one that writes."

// OfferSync builds the pairing offer for a remote. The remote is furrow's own
// spelling: an ssh:// host reachable over a LAN or a tailnet, an s3:// bucket
// used as an always-available encrypted mailbox, or a path to a directory.
func (w *Workspace) OfferSync(remote string) SyncOffer {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		remote = "ssh://user@machine"
	}
	name := filepath.Base(w.root)
	return SyncOffer{
		Workspace: w.root,
		Name:      name,
		Here: []string{
			fmt.Sprintf("furrow remote add %s --name %s", remote, name),
			"furrow sync --follow",
		},
		There: []string{
			fmt.Sprintf("FURROW_RECOVERY_KEY=<key> furrow clone %s/%s", strings.TrimRight(remote, "/"), name),
		},
		Note: syncNote,
	}
}

// ── a universe to work in, rather than a command run inside one ──────────────

// Fork materializes a copy-on-write universe of the whole workspace and HANDS
// IT BACK, running nothing inside it.
//
// It is the door [Workspace.RunInFork] is not. RunInFork exists for the model's
// own `workspace_fork` verb, where a universe is the safe place one command
// gets to run; this exists for the harness, where a universe is the GROUND a
// task is given and everything that happens in it happens afterwards, over
// hours, through the task's own belt. Until this door existed the rest of
// aforge had to ground a task with `git worktree add`, which carries HEAD and
// leaves the dirty tree, the untracked files and the `.env` behind — the defect
// the ground law (internal/session/taskground.go) was written from.
//
// destination is where the universe is put. An empty one lets furrow choose
// `<repo>.furrow-forks/<name>` beside the workspace, which is furrow's own
// default and the wrong answer for a task — a task's world belongs under the
// session that asked for it, not beside the person's project — so every caller
// in this codebase names one.
func (w *Workspace) Fork(ctx context.Context, name, destination string) (Fork, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Fork{}, fmt.Errorf("furrow: a universe to work in needs a name")
	}
	args := []string{"--json", "fork", name}
	if destination = strings.TrimSpace(destination); destination != "" {
		args = append(args, "--destination", destination)
	}

	stdout, stderr, err := w.run(ctx, args...)
	if err != nil && len(documents(stdout)) == 0 {
		return Fork{}, failure(stderr, err)
	}
	// `furrow fork` prints one document with the PLAN it costed and the RESULT
	// it got. Only the result is read: the plan is a projection made before the
	// copy happened, and a caller told what a fork was going to be rather than
	// what it is would be told something that may not have happened.
	var forked struct {
		Result struct {
			Name        string `json:"name"`
			Destination string `json:"destination"`
			Base        string `json:"base_snapshot"`
			Head        string `json:"head_snapshot"`
		} `json:"result"`
	}
	if err := decodeLast(stdout, &forked); err != nil {
		return Fork{}, err
	}
	if strings.TrimSpace(forked.Result.Destination) == "" {
		return Fork{}, fmt.Errorf("%w: furrow reported no universe", ErrUnreadable)
	}
	fork := Fork{
		Name: forked.Result.Name,
		Path: forked.Result.Destination,
		Base: forked.Result.Base,
		Head: forked.Result.Head,
	}
	if fork.Name == "" {
		fork.Name = name
	}
	return fork, nil
}

// DropFork tells furrow to forget one universe while LEAVING ITS FILES ALONE.
//
// The two halves are separated on purpose. Whoever asked for the fork owns the
// directory — for a task that is the session, which removes its own trees when
// the work has landed — and a furrow that deleted those files from under it
// would be a second owner of one directory. What furrow is asked to drop is the
// record and the timeline, so that `furrow forks` does not fill up with the
// universes of every task this machine has ever run.
//
// A FAILURE IS REPORTED AND NEVER DECIDED ABOUT HERE, because what one is worth
// depends entirely on who asked. A landing has already put the work in, so a
// record furrow would not drop costs it one line in a listing and it says
// nothing about it. The sweep that reaps a session nobody landed is the last
// thing on the machine that will ever know this fork's name, and the directory
// the record points at is about to go — so it writes the miss down
// (internal/session/sweep.go). One door, two readings, and neither of them is
// this package's to make.
func (w *Workspace) DropFork(ctx context.Context, name string) error {
	if name = strings.TrimSpace(name); name == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	stdout, stderr, err := w.run(ctx, "--json", "fork-rm", name, "--keep-files")
	if err != nil && len(documents(stdout)) == 0 {
		return failure(stderr, err)
	}
	return nil
}
