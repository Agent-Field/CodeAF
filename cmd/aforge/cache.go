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
// one actually destroys bytes, so the answer that proceeds is a word written
// out, and any other line — including an empty one, including EOF on a pipe —
// keeps everything. --yes is the scripted door and skips the question, which is
// rebuild's own arrangement.
//
// AND THE WORD IS THE SAME WORD THE CHAT WANTS. In the chat the deletion is
// `/cache clean now`, because the chat cannot pass a flag; here it used to be
// the word "clean", so the one prompt in the product that deletes gigabytes
// asked for a different word depending on which surface a person had learned
// first. One word, [cacheCleanWord], in both. `--yes` stays as the script's
// spelling and is not a word anybody types at a prompt.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/cachedir"
)

// cacheCleanWord is what a person types to go through with the deletion, and it
// is `now` because that is what the chat has always wanted after `/cache
// clean`. Two surfaces, one destructive gesture, one word.
const cacheCleanWord = "now"

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
		// `aforge cache --help` reaches here rather than a flag set, because
		// the reading form parses nothing at all (usage.go).
		if askedForHelp(args) {
			return commandHelp("cache")
		}
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
	flags := commandFlags("cache clean")
	yes := flags.Bool("yes", false, "skip the typed confirmation")
	if err := parseCommandFlags(flags, args); err != nil {
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
		// THE QUESTION GOES TO THE ASIDE, NOT TO STDOUT, and of everything this
		// wave moved off stdout this is the one that mattered most: a prompt on
		// stdout means `aforge cache clean | tee clean.log` puts the question
		// into the file and leaves the person looking at a blank terminal,
		// waiting for a word they cannot see. See streams.go.
		fmt.Fprintf(aside, "This deletes the shared build cache — %s at %s.\n", cachedir.Human(size), cachedir.Root())
		fmt.Fprintln(aside, "Toolchains and modules are re-downloaded cold on the next task. Conversations,")
		fmt.Fprintln(aside, "settings and credentials live elsewhere and are not touched.")
		fmt.Fprint(aside, `Type "`+cacheCleanWord+`" to delete it; anything else keeps it: `)
		reader := bufio.NewReader(input)
		answer, readErr := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != cacheCleanWord {
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
