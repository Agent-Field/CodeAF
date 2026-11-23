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

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// renameThisDevice keeps typed as the name of the computer whose home is dir,
// tells the fleet when it may, and returns the sentence a person reads.
func renameThisDevice(ctx context.Context, dir, typed string) (string, error) {
	return syncsetup.RenameDevice(ctx, dir, typed)
}

// runDevicesRename is `codeaf devices rename <name>`. The name may be several
// words, typed with or without quotes.
func runDevicesRename(args []string) error {
	if reason := cell.UnavailableReason(); reason != "" {
		return errors.New(reason)
	}
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
