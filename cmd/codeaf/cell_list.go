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

// cellListSource is where `cell list --all` reads chats from. It is a seam
// because the directory is reached through the sync setup, which is wired in a
// later change; until then a machine has no source and says so in the frozen
// words for it.
var cellListSource = func() (chatlist.Source, error) {
	return nil, errors.New(chatlist.NoIdentity)
}

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
			orDash(chatlist.StatusLine(r)), reltime.Elapsed(r.DurableAgo))
	}
	return w.Flush()
}
