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
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

const cellUsage = "usage: codeaf cell log [<cell>] | codeaf cell rewind <turn> [<cell>] | codeaf cell gc [--dry-run]"

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
	"log":    {args: 0, run: cellLog},
	"rewind": {args: 1, run: cellRewind},
	"gc":     {wide: true, run: cellGC},
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
		return cell.Open(home.Dir(), name)
	}
	return openCellAbove(name)
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

// cellLog prints one line per turn, newest first: id, parent, time, tools,
// receipt.
func cellLog(c cell.Cell, _ []string, out io.Writer) error {
	entries, err := cellstore.Log(c)
	if err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Fprintf(out, "%s  %s  %s  %s  %s\n", short(e.Turn.ID), orDash(short(e.Turn.Parent)),
			time.UnixMilli(e.Turn.SealedAtMs).UTC().Format("2006-01-02T15:04:05Z"),
			toolSummary(e.Receipt), short(e.Turn.Receipt))
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
	}
	return orDash(strings.Join(tools, ","))
}
