package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/lease"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The whole architecture rests on one claim: every table in this database is a
// projection of the journal, and can be thrown away and rebuilt from it. Store.
// Rebuild is the routine that makes the claim good — and it had no caller
// outside the test suite, which meant the claim was only ever exercised where
// it was already believed. A guarantee nobody can invoke is an aspiration.
//
// So there is a command. It is the operator's recovery path from a corrupted
// view, and it is also the only way a future event kind whose view write was
// forgotten will ever be noticed: replay writes what the live path wrote, and
// the difference shows up here or nowhere.
func runRebuild(args []string) error {
	return runRebuildWith(args, os.Stdin, os.Stdout)
}

func runRebuildWith(args []string, input io.Reader, output io.Writer) error {
	flags := commandFlags("rebuild")
	database := flags.String("db", defaultChatDB(), storeFlagHelp)
	yes := flags.Bool("yes", false, "skip the confirmation prompt")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge rebuild [--db path] [--yes]")
	}
	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("open store: %s is not a regular database file", path)
	}
	// A live resident is mid-write in every table this is about to replace. The
	// rebuild is one transaction and would not corrupt anything, but the
	// reconciler's in-memory carries would be reasoning about a graph that
	// moved underneath them, so the honest answer is to ask the operator to
	// close it first.
	holder, err := lease.ProbeResident(path)
	if err != nil {
		return err
	}
	if holder != nil {
		return fmt.Errorf("a resident is running (pid %d) — close it before rebuilding", holder.PID)
	}
	if !*yes {
		// THE QUESTION IS AN ASIDE AND THE ANSWER IS NOT. This went to the
		// command's `output` — os.Stdout in the shipped binary — so `aforge
		// rebuild | tee log` handed the person a blank terminal waiting for a
		// word they could not see, and put the question in the data file
		// (streams.go). The result line below is the answer and stays where it
		// is; everything a person reads ABOUT the command goes here.
		//
		// AND IT IS SAID IN THE PRODUCT'S OWN WORDS. `materialized view` is how
		// the storage engine thinks about itself, and nobody typing this
		// command has to know the term to decide whether they want it: what is
		// thrown away is everything aforge worked out from the journal, and the
		// journal is what is kept.
		fmt.Fprintf(aside, "Rebuild everything aforge worked out from the journal in %s?\n", path)
		fmt.Fprint(aside, "The journal itself is untouched; everything worked out from it is discarded and replayed. [y/N] ")
		reader := bufio.NewReader(input)
		answer, readErr := reader.ReadString('\n')
		if readErr != nil && strings.TrimSpace(answer) == "" {
			// NOBODY WAS THERE TO ANSWER, which is a rung of the ladder rather
			// than a plain error: the command asked, and there was no keyboard
			// on the other end. It used to come back as `error: rebuild
			// cancelled` on exit 1 — telling a script that a DESTRUCTIVE
			// command had failed to start, when in truth it had refused to
			// guess. The remedy is named, because a person who hit this from a
			// pipe wanted the rebuild and needs to know how to ask for it.
			fmt.Fprintln(aside, "nothing was changed — there was nobody to answer the question.")
			fmt.Fprintln(aside, "pass --yes to rebuild without being asked.")
			return exitUnanswered
		}
		if reply := strings.ToLower(strings.TrimSpace(answer)); reply != "y" && reply != "yes" {
			// SAYING NO IS NOT AN ERROR. Nothing was rebuilt, so there is no
			// answer to write and nothing went wrong: this is the aside saying
			// what happened to the question it just asked, and the command
			// leaves on the rung that means it is done.
			_, err = fmt.Fprintln(aside, "nothing was changed.")
			return err
		}
	}
	graph, err := store.Open(path)
	if err != nil {
		return err
	}
	defer graph.Close()
	if err := graph.Rebuild(); err != nil {
		return fmt.Errorf("rebuild: %w", err)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		return err
	}
	events, err := graph.LatestEventSeq()
	if err != nil {
		return err
	}
	// WHAT WAS REBUILT IS COUNTED IN STEPS. `nodes` is the store's own word for
	// them and it is machinery in front of a person — the same pieces of work
	// are `steps` in the `--json` envelope, on the task page and everywhere
	// else a person is shown a count of them, and one thing may not have two
	// names depending on which command printed it.
	_, err = fmt.Fprintf(output, "rebuilt %d steps from %d journaled events\n", len(nodes), events)
	return err
}
