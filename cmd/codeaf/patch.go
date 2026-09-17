// The patch command: the edit hand's exact-match replacement, from a shell.
//
// It exists for the same reason `manual` does, one tool along: the belt has an
// edit hand the model uses constantly, and a person scripting a fix had no way
// to reach the same one. `codeaf patch` is that hand, verbatim — one old text
// in, the file with its single occurrence replaced out, a refusal naming the
// match count when the text is not there exactly once.
//
// THE MATCHER IS THE EDIT HAND'S LAW, and it lives in internal/patch rather
// than here so the belt could share it; what this door adds is the shell
// grammar: flags, the two --*-file escapes for text that is awkward to quote,
// and the exit code.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/patch"
)

func runPatch(args []string) error {
	flags := commandFlags("patch")
	oldText := flags.String("old", "", "the text to replace — must match exactly one region of the file")
	newText := flags.String("new", "", "what replaces it; may be empty")
	oldFile := flags.String("old-file", "", "read --old's text from this file instead, for text that is awkward to quote")
	newFile := flags.String("new-file", "", "read --new's text from this file instead")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}

	// THE FILE IS NAMED FIRST. `codeaf patch` with nothing after it used to be
	// answered with a sentence about --old, which is one of three things the
	// line was short of and not the one a person typing the command alone was
	// asking about; the file is what the grammar opens with.
	positionals := flags.Args()
	if len(positionals) != 1 || strings.TrimSpace(positionals[0]) == "" {
		return wrongCall("name one file to patch")
	}
	name := positionals[0]

	seen := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	if seen["old"] == seen["old-file"] {
		return errors.New("name the text to replace with exactly one of --old TEXT or --old-file PATH")
	}
	if !seen["new"] && !seen["new-file"] {
		return errors.New("name the replacement with --new TEXT or --new-file PATH")
	}

	asked, err := patchArgument(*oldText, *oldFile)
	if err != nil {
		return fmt.Errorf("--old: %w", err)
	}
	replacement, err := patchArgument(*newText, *newFile)
	if err != nil {
		return fmt.Errorf("--new: %w", err)
	}

	original, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	patched, err := patch.Apply(string(original), asked, replacement)
	if err != nil {
		// A refusal is the whole answer: the count is in the message, nothing
		// was written, and exit 1 tells the caller's script.
		return err
	}
	info, err := os.Stat(name)
	if err != nil {
		return err
	}
	if err := os.WriteFile(name, []byte(patched), info.Mode().Perm()); err != nil {
		return err
	}
	fmt.Println("patched", name)
	return nil
}

// patchArgument resolves one of the two texts from its flag or its file, and
// refuses when both or neither were given. A file is read whole, trailing
// newline included — the same bytes a quoted argument would have carried,
// endings and all.
func patchArgument(text, file string) (string, error) {
	switch {
	case text != "" && file != "":
		return "", errors.New("give one of the flag and the file, not both")
	case file != "":
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return text, nil
}
