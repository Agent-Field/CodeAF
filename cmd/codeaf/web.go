// The web command: the belt's two hands outside this machine, from a shell.
//
// `codeaf web search QUERY` is web_search — a numbered list of results — and
// `codeaf web fetch URL` is web_fetch — one page, markup stripped, bounded.
// Both run the same road the tools run ([session.WebSearch] and
// [session.WebFetch]), so a shell and a model get the same answer shaped the
// same way, on whatever provider the person's settings name.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/search"
	"github.com/Agent-Field/codeaf/internal/session"
)

func runWeb(args []string) error {
	flags := commandFlags("web")
	if len(args) > 0 && (args[0] == "-h" || args[0] == "-help" || args[0] == "--help") {
		writeCommandUsage(usageOut, flags)
		return exitHelped
	}
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}

	positionals := flags.Args()
	if len(positionals) == 0 {
		return errors.New("codeaf web search QUERY or codeaf web fetch URL")
	}
	verb, rest := positionals[0], positionals[1:]
	switch verb {
	case "search":
		return runWebSearch(rest)
	case "fetch":
		return runWebFetch(rest)
	default:
		return fmt.Errorf("codeaf web search QUERY or codeaf web fetch URL — %q is neither", verb)
	}
}

func runWebSearch(args []string) error {
	flags := commandFlags("web search")
	count := flags.Int("count", session.SearchDefaultCount, "how many results to return (maximum 8)")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	query := strings.Join(flags.Args(), " ")
	if strings.TrimSpace(query) == "" {
		return errors.New("what to search for — codeaf web search QUERY")
	}

	provider, _, err := webPair()
	if err != nil {
		return err
	}
	// The tool clamps the model's ask instead of arguing about it; a shell
	// caller is clamped by the same function.
	said, failed := session.WebSearch(context.Background(), provider, query, session.WebSearchCount(*count))
	return printWebResult(said, failed)
}

func runWebFetch(args []string) error {
	flags := commandFlags("web fetch")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	positionals := flags.Args()
	if len(positionals) != 1 || strings.TrimSpace(positionals[0]) == "" {
		return errors.New("one absolute URL — codeaf web fetch URL")
	}

	_, fetcher, err := webPair()
	if err != nil {
		return err
	}
	said, failed := session.WebFetch(context.Background(), fetcher, positionals[0])
	return printWebResult(said, failed)
}

// printWebResult writes the road's answer where a shell reads it: the text on
// stdout, a failure on stderr with the refusal exit. The words are the tool
// result's own, whatever door asked.
func printWebResult(said string, failed bool) error {
	if failed {
		fmt.Fprintln(os.Stderr, said)
		return exitCannotRun
	}
	fmt.Println(said)
	return nil
}

// webPair resolves the provider and fetcher the session's web tools run on:
// the four settings rows in, internal/search's live resolver out — the same
// wiring [v3Search] gives a conversation.
func webPair() (search.Provider, search.Fetcher, error) {
	settings, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	provider, fetcher := search.Live(func() search.Options {
		return config.SearchOptionsAt(settings.ProfileDir)
	})
	return provider, fetcher, nil
}
