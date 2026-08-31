// aforge-changes is the changelog's gate, its scaffold and its typesetter.
//
// THE CHANGELOG IN THIS REPOSITORY IS WRITTEN FOR A MODEL AND NOT FOR A
// RELEASE PAGE. Its job is not to say what shipped — `git log` says that, and
// says it better. Its job is to say WHAT SOMEBODY NOW BELIEVES WRONGLY: which
// branch stopped existing, which default moved, which refusal became a
// capability. That is the one thing no tool can derive from a diff and the one
// thing a session opening this repository with a fortnight-old memory of it
// most needs, so it is the field the format is built around.
//
// It is not shipped. Like cmd/aforge-demo-home this is a developer's binary and
// never a verb on aforge, so it costs the product nothing on the SIZE-BUDGET.
//
// Usage:
//
//	aforge-changes check [dir]                validate every entry
//	aforge-changes render <version> [date]    print one version's section
//	aforge-changes roll   <version> [date]    render into CHANGELOG.md, then
//	                                          remove the entries it consumed
//	aforge-changes new <kind> <pr> <slug>     scaffold an entry to fill in
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDir    = "docs/changes/unreleased"
	changelogFile = "CHANGELOG.md"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "check":
		err = check(argOr(2, defaultDir))
	case "render":
		err = render(os.Args[2:])
	case "roll":
		err = roll(os.Args[2:])
	case "new":
		err = scaffold(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n%v\n\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `aforge-changes — the changelog entries in docs/changes/unreleased

  check [dir]                 validate every entry and say everything wrong at once
  render <version> [date]     print one version's section to stdout
  roll   <version> [date]     write that section into CHANGELOG.md and remove the
                              entries it consumed
  new <kind> <pr> <slug>      scaffold an entry to fill in

  kinds: added changed renamed fixed removed internal

docs/rules/changelog.md is the rule and says what belongs in an entry.
`)
}

func argOr(i int, fallback string) string {
	if len(os.Args) > i {
		return os.Args[i]
	}
	return fallback
}

func check(dir string) error {
	entries, errs := LoadDir(dir)
	if len(errs) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%d problem(s) in the change entries:\n", len(errs))
		for _, e := range errs {
			fmt.Fprintf(&b, "  · %v\n", e)
		}
		fmt.Fprintf(&b, "\ndocs/rules/changelog.md says what an entry has to carry.")
		return fmt.Errorf("%s", b.String())
	}
	fmt.Printf("%s: %d entr%s, all well formed.\n", dir, len(entries), plural(len(entries)))
	return nil
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func versionAndDate(args []string) (string, string, error) {
	if len(args) < 1 {
		return "", "", fmt.Errorf("name the version, for example v0.2.0")
	}
	date := time.Now().UTC().Format("2006-01-02")
	if len(args) > 1 {
		date = args[1]
	}
	return args[0], date, nil
}

func render(args []string) error {
	version, date, err := versionAndDate(args)
	if err != nil {
		return err
	}
	entries, errs := LoadDir(defaultDir)
	if len(errs) > 0 {
		return check(defaultDir)
	}
	if len(entries) == 0 {
		return fmt.Errorf("there is nothing in %s to roll up", defaultDir)
	}
	fmt.Print(Render(version, date, entries))
	return nil
}

// roll is the whole release-note step in one verified move: read, render,
// insert, and only then delete. THE DELETE IS LAST AND ONLY ON SUCCESS, because
// a half-done roll that has eaten the entries leaves nothing to try again with.
func roll(args []string) error {
	version, date, err := versionAndDate(args)
	if err != nil {
		return err
	}
	entries, errs := LoadDir(defaultDir)
	if len(errs) > 0 {
		return check(defaultDir)
	}
	if len(entries) == 0 {
		return fmt.Errorf("there is nothing in %s to roll up", defaultDir)
	}

	current, err := os.ReadFile(changelogFile)
	if err != nil {
		return err
	}
	next, err := Insert(string(current), Render(version, date, entries))
	if err != nil {
		return err
	}
	if err := os.WriteFile(changelogFile, []byte(next), 0o644); err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.Remove(e.Path); err != nil {
			return err
		}
	}
	fmt.Printf("%s now carries %s (%d entr%s). The loose entries are gone; commit both.\n",
		changelogFile, version, len(entries), plural(len(entries)))
	return nil
}

func scaffold(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: aforge-changes new <kind> <pr> <slug>\n  kinds: %s", strings.Join(kindKeys(), " "))
	}
	kind, prText, slug := args[0], args[1], args[2]

	ok := false
	for _, k := range kinds {
		if k.key == kind {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("kind %q is not one of %s", kind, strings.Join(kindKeys(), " "))
	}
	pr, err := strconv.Atoi(prText)
	if err != nil || pr <= 0 {
		return fmt.Errorf("the pull request number has to be a positive integer, not %q", prText)
	}

	path := filepath.Join(defaultDir, fmt.Sprintf("%d-%s.md", pr, slug))
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	body := fmt.Sprintf(`---
kind: %s
title: ONE LINE, NO FULL STOP, THE WAY YOU WOULD SAY IT OUT LOUD
pr: %d
# Optional. Which part of the repository this touches, so a reader can skip it.
# One or more of: %s
surface: []
# Optional, and the reason this file exists. Every statement that WAS true and is
# not any more, written whole: what it was, and what it is now. A model reading
# this has to be able to check its own memory against it, so "branch names" is
# useless and "the trunk was chat-v3-task and no longer exists; work goes to dev"
# is the whole point. Leave the list empty if nothing anybody believed changed.
invalidates: []
---

Optional. A paragraph on WHY, if the title does not already carry it. Delete this
line if it does.
`, kind, pr, strings.Join(surfaceKeys(), ", "))

	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s — fill it in, then `make changelog-check`.\n", path)
	return nil
}
