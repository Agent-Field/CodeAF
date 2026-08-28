// The cache from the command line: what it holds, and the one destructive verb
// that empties it.
//
// `aforge cache` answers the question and changes nothing. `aforge cache clean`
// deletes ~/.aforge/cache — the shared toolchain caches every task worker fills
// (internal/cachedir says what lives there and what never does) — and because a
// deletion cannot be undone it is guarded the way destructive command lines are
// guarded everywhere a person has already learned: the blast radius is printed
// first, sizes and path included, and then the confirmation is a TYPED WORD
// rather than a y. `rebuild` asks [y/N] because the journal survives it; this
// one actually destroys bytes, so the answer that proceeds is the word "clean"
// written out, and any other line — including an empty one, including EOF on a
// pipe — keeps everything. --yes is the scripted door and skips the question,
// which is rebuild's own arrangement.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/cachedir"
)

func runCache(args []string) error {
	return runCacheWith(args, os.Stdin, os.Stdout)
}

func runCacheWith(args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 {
		return showCache(output)
	}
	switch args[0] {
	case "clean":
		return cleanCache(args[1:], input, output)
	default:
		return fmt.Errorf("usage: aforge cache [clean [--yes]]")
	}
}

// showCache is the reading form, and it always answers: a typed question that
// got silence back reads as a command that broke.
func showCache(output io.Writer) error {
	size := cachedir.Size()
	if size == 0 {
		_, err := fmt.Fprintf(output, "the cache is empty · %s\n", cachedir.Root())
		return err
	}
	_, err := fmt.Fprintf(output,
		"the cache holds %s · %s\nshared toolchain caches — go modules, builds, npm, pip, cargo. `aforge cache clean` frees it.\n",
		cachedir.Human(size), cachedir.Root())
	return err
}

func cleanCache(args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("cache clean", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	yes := flags.Bool("yes", false, "skip the typed confirmation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge cache clean [--yes]")
	}
	size := cachedir.Size()
	if size == 0 {
		_, err := fmt.Fprintln(output, "the cache is already empty — nothing to delete.")
		return err
	}
	// The question, asked with everything a person needs to answer it: how
	// much, where, what deleting costs, and what is out of reach. The last
	// clause is there because "cache" is a word people reasonably stretch over
	// their conversations, and the moment to correct that is before the
	// deletion rather than after.
	if !*yes {
		fmt.Fprintf(output, "This deletes the shared build cache — %s at %s.\n", cachedir.Human(size), cachedir.Root())
		fmt.Fprintln(output, "Toolchains and modules are re-downloaded cold on the next task. Conversations,")
		fmt.Fprintln(output, "settings and credentials live elsewhere and are not touched.")
		fmt.Fprint(output, `Type "clean" to delete it; anything else keeps it: `)
		reader := bufio.NewReader(input)
		answer, readErr := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "clean" {
			// EOF, an empty line and a wrong word all land here on purpose: a
			// destructive prompt fails closed, and readErr is not consulted
			// because no reading failure is a yes.
			_ = readErr
			_, err := fmt.Fprintln(output, "kept — nothing was deleted.")
			return err
		}
	}
	freed, err := cachedir.Clean()
	if err != nil {
		return fmt.Errorf("cache clean: %w", err)
	}
	_, err = fmt.Fprintf(output, "cleaned · %s freed\n", cachedir.Human(freed))
	return err
}
