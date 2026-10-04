package pair

// `codeaf devices`: what this machine has let in, and how to stop one.
//
// THE LIST IS THE ENGINE MACHINE'S OWN, and so is the stopping. A device cannot
// list itself out of somebody's machine and cannot stop another device; the
// command answers about the machine it is typed on. That is the same law the
// rest of codeaf keeps about remote surfaces — the machine that runs the tools
// is the machine that decides who may run them.
//
// THE EMPTINESS LAW APPLIES: a machine with no devices of either kind prints
// one sentence saying so ([NoDevices]), and a kind with nothing in it prints no
// heading at all. The door in cmd/codeaf decides that, so the functions here
// only ever draw a table that has rows.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
)

// decodeStoredKey reads a key out of a book.
func decodeStoredKey(stored string) ([]byte, error) {
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(stored))
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("a key is 32 bytes")
	}
	return key, nil
}

// DevicesList is what `codeaf devices` prints about the devices that can use
// this machine. Its caller has already checked that paired is not empty.
func DevicesList(name string, paired []Paired, keeper Keeper, now time.Time) string {
	var out strings.Builder
	fmt.Fprintf(&out, "this machine is reachable as %s\n", name)
	fmt.Fprintf(&out, "its key is kept in %s\n\n", keeper.Where())

	widest := 0
	for _, one := range paired {
		widest = max(widest, width(one.Label))
	}
	out.WriteString("devices that can use this machine\n\n")
	for _, one := range paired {
		// The emptiness law, one row at a time: a device that has never
		// connected shows nothing where its last connection would be, rather
		// than a zero time or the word "never".
		line := fmt.Sprintf("  %s  paired %s", padded(one.Label, widest), since(one.Since, now))
		if seen := since(one.Seen, now); seen != "" {
			line += "  ·  last here " + seen
		}
		out.WriteString(line + "\n")
	}
	out.WriteString("\nremove one with `codeaf devices revoke <name>` — it will need a new code to come back.\n")
	return out.String()
}

// MachinesList is what `codeaf devices` prints about the machines THIS device
// can reach. It is the other half of the same command, because a person asking
// "what am I paired with" means both directions and should not have to know
// there are two books.
func MachinesList(known []Known, now time.Time) string {
	if len(known) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\nmachines this device can reach\n\n")
	widest := 0
	for _, one := range known {
		widest = max(widest, width(one.Name))
	}
	for _, one := range known {
		fmt.Fprintf(&out, "  %s  paired %s\n", padded(one.Name, widest), since(one.Since, now))
	}
	out.WriteString("\nopen one with `codeaf chat --at <name>`.\n")
	return out.String()
}

// width is how many columns a name takes, so a name with wide or accented
// letters still lines up with the rest of its column.
func width(s string) int { return ansi.StringWidth(s) }

// padded is s followed by the spaces that make it w columns wide.
func padded(s string, w int) string { return s + strings.Repeat(" ", max(0, w-width(s))) }

// RevokedLine is what a person reads when a device has been removed.
func RevokedLine(label string) string {
	return label + " was removed — it can no longer open a chat here, and it needs a new pairing code to come back."
}

// NoDevices is the whole answer of `codeaf devices` when nothing of either kind
// is paired: one sentence, and the two ways to change that.
const NoDevices = "nothing is paired yet — run `codeaf pair` to add another device, or `codeaf serve` to let a device use this machine."

// ChatsDevice is one device that holds the person's chats, as a row.
type ChatsDevice struct {
	Name    string
	This    bool // the computer the command is typed on
	Stopped bool // removed: the relay no longer answers it
}

// ChatsDevicesList is the table of computers that hold the person's chats. Its
// caller has already checked that rows is not empty.
func ChatsDevicesList(rows []ChatsDevice) string {
	var out strings.Builder
	out.WriteString("your devices\n\n")
	widest := 0
	for _, one := range rows {
		widest = max(widest, width(one.Name))
	}
	for _, one := range rows {
		fmt.Fprintf(&out, "  %s%s\n", padded(one.Name, widest), chatsMarks(one))
	}
	out.WriteString("\nremove one with `codeaf devices revoke <name>` — it stops syncing your chats, but keeps what it already holds.\n")
	return out.String()
}

// chatsMarks is what stands after a name: nothing for an ordinary computer,
// so the common row is only its name.
func chatsMarks(one ChatsDevice) string {
	var marks []string
	if one.This {
		marks = append(marks, "this device")
	}
	if one.Stopped {
		marks = append(marks, "removed")
	}
	if len(marks) == 0 {
		return ""
	}
	return "  " + strings.Join(marks, "  ·  ")
}

// ChatsRevokedLine is what a person reads when a computer that holds their
// chats has been removed. It says what removing cannot do, because the person
// who has just removed a stolen computer is the one who most needs to know.
func ChatsRevokedLine(name string) string {
	return name + " was removed — it can no longer sync your chats.\n" +
		"It cannot undo what that device already holds: your chats and keys. If it was lost or stolen, treat your chats as exposed and run `codeaf identity rotate` to lock it out for good."
}
