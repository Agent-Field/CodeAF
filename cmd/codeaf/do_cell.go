package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	runengine "github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A RUN IN A CELL IS A CHAT'S SESSION WITHOUT THE CHAT. With CODEAF_CELLS=1 the
// run's workers ride the seat a chat rides — minted and built by the same
// functions ([v3MintSession], [v3Seated]) — so every tool call seals and `codeaf
// cell log` shows the run's turns. The run's record folder points at the cell
// (cellPointer), and `do --continue` follows the pointer back to it.

// cellPointer is the file in a record folder that names the cell the run worked
// in. A record without one is a pre-cell run and continues from the record alone.
const cellPointer = "cell"

// runCellClass is the class a run's cell declares. A run's workers drive the
// run's own record (`plandb done`, the trajectory), which lives outside the
// directory they edit, so their calls cannot be confined to it: the cell is
// host-bound and every call it seals is external, never re-run after a crash.
const runCellClass = cell.HostBound

// errandCell is the cell one run works in. The zero value is a run with no
// cell: no seat, no pointer, the host as ever.
type errandCell struct {
	root string
	seat executor.Seat
}

// openErrandCell puts a run in a cell: the one named by resume when a
// continuation follows a record that has one, a new one otherwise. Flag off, it
// answers the zero value and touches nothing.
func openErrandCell(workspace, resume string) (errandCell, error) {
	if !cell.Enabled() {
		return errandCell{}, nil
	}
	place, err := errandPlace(workspace, resume)
	if err != nil {
		return errandCell{}, err
	}
	seat, _ := v3SeatOf(place, errandSeals)
	return errandCell{root: place.Dir, seat: seat}, nil
}

// errandPlace is the session place of the run's cell: the folder, with the
// directory the run edits as the tree its calls seal (a borrowed workspace).
func errandPlace(workspace, resume string) (session.Place, error) {
	if resume != "" {
		return v3PlaceFor(resume, workspace, false), nil
	}
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		return session.Place{}, err
	}
	return v3MintSessionAs(runCellClass, bucket, workspace, v3StampLaunchDir(v3LaunchDir(), workspace), false)
}

// workerOptions seats every worker of the run on the cell's seat.
func (c errandCell) workerOptions() []runengine.WorkerOption {
	if c.seat == nil {
		return nil
	}
	return []runengine.WorkerOption{runengine.WithSeat(c.seat)}
}

// pointFrom writes the pointer into the record folder. A run with no cell
// writes nothing.
func (c errandCell) pointFrom(record string) error {
	if c.root == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(record, cellPointer), []byte(c.root+"\n"), 0o600)
}

// cellOf is the cell root a record points at, and empty for a pre-cell record
// or one whose cell is gone: both continue from the record alone.
func cellOf(record string) string {
	raw, err := os.ReadFile(filepath.Join(record, cellPointer))
	if err != nil {
		return ""
	}
	root := strings.TrimSpace(string(raw))
	if _, err := cell.OpenAt(root, filepath.Base(root)); err != nil {
		return ""
	}
	return root
}

// sealedState is what a cell's chain holds of an earlier run: how many turns
// were sealed, the last one, and the calls that began and never finished.
type sealedState struct {
	turns      int
	head       string
	unfinished []string
}

// sealedStateOf reads the chain of the cell at root. A cell that cannot be read
// has sealed nothing that can be named.
func sealedStateOf(root string) sealedState {
	c, err := cell.OpenAt(root, filepath.Base(root))
	if err != nil {
		return sealedState{}
	}
	turns, _ := cellstore.Turns(c)
	state := sealedState{turns: len(turns), unfinished: unfinishedCalls(c)}
	if len(turns) > 0 {
		state.head = short(turns[len(turns)-1].ID)
	}
	return state
}

// unfinishedCalls names each call the call log holds an intent for and no
// completion. They are surfaced and never run again.
func unfinishedCalls(c cell.Cell) []string {
	_, rec, err := cellstore.OpenWAL(cellEngine(c).WALPath(c))
	if err != nil {
		return nil
	}
	lines := make([]string, len(rec.Incomplete))
	for i, in := range rec.Incomplete {
		lines[i] = fmt.Sprintf("- %s (%s), started %s", in.Tool, in.SideEffect,
			time.UnixMilli(in.Started).UTC().Format("2006-01-02T15:04:05Z"))
	}
	return lines
}

// empty says the chain holds nothing worth a block in a brief.
func (s sealedState) empty() bool { return s.turns == 0 && len(s.unfinished) == 0 }

// words is the sealed state as the continuation brief carries it.
func (s sealedState) words() string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s\nThe run's cell sealed %d turns, the last %s; its files stand as that turn left them.", continueSealedHeader, s.turns, orDash(s.head))
	if len(s.unfinished) > 0 {
		out.WriteString("\nThese calls began and never finished. They are not run again; look at what they left in the tree before relying on it:\n")
		out.WriteString(strings.Join(s.unfinished, "\n"))
	}
	return out.String()
}
