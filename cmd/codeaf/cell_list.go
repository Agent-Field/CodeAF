package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/tui2/reltime"
)

// cellListAsk bounds one listing so a silent relay ends the command rather
// than hanging an ssh session.
const cellListAsk = 10 * time.Second

// cellListSource is where `cell list --all` reads chats from. It is a seam so
// a test can stand in any source; the real one is the sync setup (syncwire.go).
var cellListSource = syncListSource

// cellList prints every chat this person has, on any machine, one per line:
// id, title, device, what §8.1 says about where it runs, and the age of its
// last durable turn. It is headless on purpose, for ssh-driven demos.
func cellList(args []string, out io.Writer) error {
	if len(args) != 1 || args[0] != "--all" {
		return errors.New(cellUsage)
	}
	src, err := cellListSource()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cellListAsk)
	defer cancel()
	rows, err := src.Rows(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", chatlist.Unreachable, err)
	}
	return writeChatRows(rows, out)
}

// writeChatRows draws the rows as an aligned table. A column with nothing to
// say (a chat that is here or idle has no status line) shows a dash so the
// columns still line up.
func writeChatRows(rows []chatlist.Row, out io.Writer) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Cell, r.Title, orDash(r.Device),
			orDash(statusOf(r)), reltime.Elapsed(r.DurableAgo))
	}
	return w.Flush()
}

// statusOf is the status column: a branch says only what this build can do with
// it, which is discard (there is no merge, cell_branch.go).
func statusOf(r chatlist.Row) string {
	if r.Status == chatlist.Branch {
		return chatlist.BranchLine(r, false)
	}
	return chatlist.StatusLine(r)
}
