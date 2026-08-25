package pair

// `aforge devices`: what this machine has let in, and how to stop one.
//
// THE LIST IS THE ENGINE MACHINE'S OWN, and so is the stopping. A device cannot
// list itself out of somebody's machine and cannot stop another device; the
// command answers about the machine it is typed on. That is the same law the
// rest of aforge keeps about remote surfaces — the machine that runs the tools
// is the machine that decides who may run them.
//
// THE EMPTINESS LAW APPLIES: a machine with no devices paired prints one
// sentence saying so, not a heading over an empty table.

import (
	"encoding/base64"
	"errors"
	"fmt"
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

// DevicesList is what `aforge devices` prints on the machine that owns the work.
func DevicesList(name string, paired []Paired, keeper Keeper, now time.Time) string {
	var out strings.Builder
	fmt.Fprintf(&out, "this machine is reachable as %s\n", name)
	fmt.Fprintf(&out, "its key is kept in %s\n\n", keeper.Where())

	if len(paired) == 0 {
		out.WriteString("no devices are paired with this machine.\nrun `aforge serve` here and `aforge chat --at " + name + "` there to pair one.\n")
		return out.String()
	}

	widest := 0
	for _, one := range paired {
		if len(one.Label) > widest {
			widest = len(one.Label)
		}
	}
	out.WriteString("devices paired with this machine\n\n")
	for _, one := range paired {
		// The emptiness law, one row at a time: a device that has never
		// connected shows nothing where its last connection would be, rather
		// than a zero time or the word "never".
		line := fmt.Sprintf("  %-*s  paired %s", widest, one.Label, since(one.Since, now))
		if seen := since(one.Seen, now); seen != "" {
			line += "  ·  last here " + seen
		}
		out.WriteString(line + "\n")
	}
	out.WriteString("\nstop one with `aforge devices revoke <name>` — it will need a new code to come back.\n")
	return out.String()
}

// MachinesList is what `aforge devices` prints about the machines THIS device
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
		if len(one.Name) > widest {
			widest = len(one.Name)
		}
	}
	for _, one := range known {
		fmt.Fprintf(&out, "  %-*s  paired %s\n", widest, one.Name, since(one.Since, now))
	}
	out.WriteString("\nopen one with `aforge chat --at <name>`.\n")
	return out.String()
}

// RevokedLine is what a person reads when a device has been stopped.
func RevokedLine(label string) string {
	return label + " has been stopped — it can no longer open a conversation here, and it will need a new pairing code to come back."
}
