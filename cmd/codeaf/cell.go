package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

const cellUsage = "usage: codeaf cell log [<cell>] | codeaf cell rewind <turn> [<cell>] | codeaf cell resolve [<cell>] | codeaf cell gc [--dry-run]"

// cellVerb is one word after `cell`: how many arguments of its own it takes
// before the optional cell name, and what it does to the cell. A verb that is
// about every cell on the device (wide) takes no cell name and gets all its
// arguments.
type cellVerb struct {
	args int
	wide bool
	run  func(c cell.Cell, args []string, out io.Writer) error
}

var cellVerbs = map[string]cellVerb{
	"log":     {args: 0, run: cellLog},
	"rewind":  {args: 1, run: cellRewind},
	"resolve": {args: 0, run: cellResolve},
	"gc":      {wide: true, run: cellGC},
}

// runCell is the stage 0 door onto a cell's turn chain. Like `engine` it is
// machinery, not on the help page, until cells are the default.
func runCell(args []string) error {
	cwd, _ := os.Getwd()
	return runCellIn(args, os.Stdout, cwd)
}

// runCellIn is runCell with the writer and the working directory injectable.
func runCellIn(args []string, out io.Writer, cwd string) error {
	if len(args) == 0 {
		return errors.New(cellUsage)
	}
	verb, ok := cellVerbs[args[0]]
	rest := args[1:]
	if ok && verb.wide {
		return verb.run(cell.Cell{}, rest, out)
	}
	if !ok || len(rest) < verb.args || len(rest) > verb.args+1 {
		return errors.New(cellUsage)
	}
	c, err := openCellNamed(strings.Join(rest[verb.args:], ""), cwd)
	if err != nil {
		return err
	}
	return verb.run(c, rest[:verb.args], out)
}

// openCellNamed opens the cell an id or a folder path names, or, with no name,
// the cell the working directory is inside.
func openCellNamed(name, cwd string) (cell.Cell, error) {
	switch {
	case name == "":
		return openCellAbove(cwd)
	case cell.ValidID(name):
		return openCellByID(name)
	}
	return openCellAbove(name)
}

// openCellByID finds a cell by id where a chat keeps it: in the project bucket
// its workspace names, or in the flat cells directory of cell.Create.
func openCellByID(id string) (cell.Cell, error) {
	buckets, _ := filepath.Glob(filepath.Join(home.Join("v3", "projects"), "*", id))
	for _, dir := range append(buckets, filepath.Join(cell.Dir(home.Dir()), id)) {
		if c, err := cell.OpenAt(dir, id); err == nil {
			return c, nil
		}
	}
	return cell.Open(home.Dir(), id)
}

func openCellAbove(dir string) (cell.Cell, error) {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, filepath.FromSlash(cell.MetaPath))); err == nil {
			return cell.OpenAt(d, filepath.Base(d))
		}
		if filepath.Dir(d) == d {
			return cell.Cell{}, fmt.Errorf("no cell at or above %s; name one by id or folder", dir)
		}
	}
}

// cellLog prints one line per turn, newest first: id, parent, time, trigger,
// tools (an external call is marked), receipt.
func cellLog(c cell.Cell, _ []string, out io.Writer) error {
	entries, err := cellstore.Log(c)
	if err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Fprintf(out, "%s  %s  %s  %s  %s  %s\n", short(e.Turn.ID), orDash(short(e.Turn.Parent)),
			time.UnixMilli(e.Turn.SealedAtMs).UTC().Format("2006-01-02T15:04:05Z"),
			e.Turn.Trigger, toolSummary(e.Receipt), short(e.Turn.Receipt))
	}
	return writeUnfinished(c, out)
}

// writeUnfinished says which calls began and never finished: a crash left them
// in the call log, and the harness never runs them again on its own.
func writeUnfinished(c cell.Cell, out io.Writer) error {
	_, rec, err := cellstore.OpenWAL(cellEngine(c).WALPath(c))
	if err != nil {
		return err
	}
	for _, in := range rec.Incomplete {
		fmt.Fprintf(out, "unfinished  %s  %s  started %s  not run again; `codeaf cell resolve` closes it\n", in.Tool,
			in.SideEffect, time.UnixMilli(in.Started).UTC().Format("2006-01-02T15:04:05Z"))
	}
	return nil
}

// cellResolve closes every unfinished call of a cell no session holds, so the
// cell can be rewound again.
func cellResolve(c cell.Cell, _ []string, out io.Writer) error {
	if cellBusy(c) {
		return errors.New("a session holds this cell; close it first")
	}
	wal, rec, err := cellstore.OpenWAL(cellEngine(c).WALPath(c))
	if err != nil {
		return err
	}
	for _, in := range rec.Incomplete {
		if err := wal.Resolve(in); err != nil {
			return err
		}
		fmt.Fprintf(out, "resolved  %s  started %s\n", in.Tool, time.UnixMilli(in.Started).UTC().Format("2006-01-02T15:04:05Z"))
	}
	return nil
}

// cellRewind restores the cell to a turn and prints the turn that records it.
func cellRewind(c cell.Cell, args []string, out io.Writer) error {
	sealed, err := cellEngine(c).Rewind(context.Background(), c, args[0])
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "rewound to %s as new turn %s\n", args[0], short(sealed.Turn.ID))
	return err
}

// cellEngine is the engine of a cell opened from a verb: built from the
// session place the cell's meta records, exactly as the session's seat builds it.
func cellEngine(c cell.Cell) cellstore.Engine { return cellstore.EngineFor(cellWorkspace(c)) }

// cellWorkspace is the tree a cell's session seals: its own work/ when the
// session owns its workspace, else the project it borrowed.
func cellWorkspace(c cell.Cell) string {
	meta, _ := session.LoadMeta(c.Root)
	return v3PlaceFor(c.Root, meta.Workspace, meta.Owned).Workspace
}

const shortIDLen = 12

func short(id string) string { return id[:min(len(id), shortIDLen)] }

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func toolSummary(r cellstore.Receipt) string {
	tools := make([]string, len(r.Calls))
	for i, call := range r.Calls {
		tools[i] = call.Tool
		if call.SideEffect == string(executor.EffectExternal) {
			tools[i] += "[external]"
		}
	}
	return orDash(strings.Join(tools, ","))
}
