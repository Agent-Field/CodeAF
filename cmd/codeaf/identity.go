package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

const identityUsage = "usage: codeaf identity show | codeaf identity export [<file>] | codeaf identity import [--replace] <file>"

// identityDoor is what a verb may touch: the codeaf home, where it writes, and
// how a passphrase is asked for (with a second entry when it is being chosen).
type identityDoor struct {
	home       string
	out        io.Writer
	passphrase func(confirm bool) (string, error)
}

// identityVerbs maps the word after `identity` to what it does.
var identityVerbs = map[string]func(d identityDoor, args []string) error{
	"show":   identityShow,
	"export": identityExport,
	"import": identityImport,
}

// runIdentity is the door onto the person's identity. Like `cell` it is
// machinery, off the help page and refused, until cells are the default.
func runIdentity(args []string) error {
	if !cell.Enabled() {
		return errors.New("codeaf identity needs CODEAF_CELLS=1")
	}
	return runIdentityAt(args, identityDoor{home: home.Dir(), out: os.Stdout, passphrase: askPassphrase})
}

func runIdentityAt(args []string, d identityDoor) error {
	if len(args) == 0 {
		return errors.New(identityUsage)
	}
	verb, ok := identityVerbs[args[0]]
	if !ok {
		return errors.New(identityUsage)
	}
	return verb(d, args[1:])
}

func identityShow(d identityDoor, args []string) error {
	if len(args) != 0 {
		return errors.New(identityUsage)
	}
	id, err := identity.Ensure(d.home)
	if err != nil {
		return err
	}
	dev, err := identity.Device(d.home)
	if err != nil {
		return err
	}
	fmt.Fprintf(d.out, "id           %s\nfingerprint  %s\ncell key id  %s\ndevice       %s\n",
		id.ID(), id.Fingerprint(), id.CellKeyID(), dev.ID())
	return nil
}

func identityExport(d identityDoor, args []string) error {
	if len(args) > 1 {
		return errors.New(identityUsage)
	}
	id, err := identity.Ensure(d.home)
	if err != nil {
		return err
	}
	phrase, err := d.passphrase(true)
	if err != nil {
		return err
	}
	blob, err := identity.Export(id, phrase)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		_, err = fmt.Fprintf(d.out, "%s\n", blob)
		return err
	}
	return os.WriteFile(args[0], append(blob, '\n'), 0o600)
}

func identityImport(d identityDoor, args []string) error {
	replace := slices.Contains(args, "--replace")
	files := slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--replace" })
	if len(files) != 1 {
		return errors.New(identityUsage)
	}
	blob, err := os.ReadFile(files[0])
	if err != nil {
		return err
	}
	phrase, err := d.passphrase(false)
	if err != nil {
		return err
	}
	id, err := identity.Import(blob, phrase)
	if err != nil {
		return err
	}
	replaced, err := identity.Adopt(d.home, id, replace)
	if err != nil {
		return err
	}
	fmt.Fprintf(d.out, "imported %s (%s)\n", id.ID(), id.Fingerprint())
	if replaced && keys.Exists(d.home) {
		fmt.Fprintln(d.out, "secrets sealed under the previous identity cannot be read now")
	}
	return nil
}

// askPassphrase reads a passphrase without echo on a terminal, else one line
// from stdin. Choosing one (confirm) asks twice on a terminal.
func askPassphrase(confirm bool) (string, error) {
	if !connectInputIsTTY(connectInput) {
		return readPassphraseLine(connectInput)
	}
	first, err := promptSecret("passphrase: ")
	if err != nil || !confirm {
		return first, err
	}
	again, err := promptSecret("again: ")
	if err == nil && again != first {
		err = errors.New("the two passphrases differ")
	}
	return first, err
}

func promptSecret(prompt string) (string, error) {
	fmt.Fprint(usageErr, prompt)
	raw, err := connectReadPassword(connectInput.Fd())
	fmt.Fprintln(usageErr)
	return nonEmpty(string(raw), err)
}

func readPassphraseLine(in io.Reader) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(in, 1<<16))
	line, _, _ := strings.Cut(string(raw), "\n")
	return nonEmpty(strings.TrimRight(line, "\r"), err)
}

func nonEmpty(phrase string, err error) (string, error) {
	if err == nil && phrase == "" {
		err = errors.New("the passphrase is empty")
	}
	return phrase, err
}
