package main

// ── naming this computer: `codeaf devices rename <name>` ────────────────────
//
// THE NAME IS KEPT HERE FIRST AND TOLD TO THE FLEET SECOND. A rename that
// needed the network would fail on a plane, and a name that was typed and then
// lost would be a person typing it twice; so the name is written to this
// computer's home, and the same upsert every chat start makes ([syncsetup.Sync.Rename])
// carries it to the other devices when it can. If it cannot, the next chat
// start does, and the sentence says so.
//
// A device only ever writes its own record, so this verb names the computer it
// is typed on and no other.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// renameWithin bounds the one request a rename makes, so a silent relay cannot
// hang the command.
const renameWithin = 20 * time.Second

// renameThisDevice keeps typed as the name of the computer whose home is dir,
// tells the fleet when it may, and returns the sentence a person reads.
func renameThisDevice(ctx context.Context, dir, typed string) (string, error) {
	name, err := devname.Set(dir, typed)
	if err != nil {
		return "", err
	}
	if !syncsetup.MayTalk(dir) {
		return pair.RenamedAloneLine(name), nil
	}
	if tellFleet(ctx, dir, name) != nil {
		return pair.RenamedHereLine(name), nil
	}
	return pair.RenamedLine(name), nil
}

// tellFleet puts the new name in the directory, and says why not when it cannot.
func tellFleet(ctx context.Context, dir, name string) error {
	s, ok, err := syncsetup.Open(dir)
	if err != nil || !ok {
		return errors.Join(err, errors.New("sync is off"))
	}
	ctx, cancel := context.WithTimeout(ctx, renameWithin)
	defer cancel()
	return s.Rename(ctx, name)
}

// runDevicesRename is `codeaf devices rename <name>`. The name may be several
// words, typed with or without quotes.
func runDevicesRename(args []string) error {
	// Its own usage entry names the command; it takes no flags of its own.
	flags := commandFlags("devices rename")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	typed := strings.Join(flags.Args(), " ")
	if strings.TrimSpace(typed) == "" {
		return errors.New("usage: codeaf devices rename <name> \u2014 " + devname.ErrEmpty.Error())
	}
	said, err := renameThisDevice(context.Background(), home.Dir(), typed)
	if err != nil {
		return err
	}
	fmt.Println(said)
	return nil
}
