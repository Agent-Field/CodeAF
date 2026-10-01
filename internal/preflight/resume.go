package preflight

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
)

// Resume is what a chat that moved here left behind. The transcript says what
// was done; it cannot say what is not running now, what the copy left out, or
// what this machine lacks, and those three are the whole of a Resume
// (docs/STAGE-1-CONTRACTS.md section 19.5). It is facts, never a summary.
//
// A Resume is built with no model call, from the record the other machine
// sealed and from a look at this one, and it carries no command to run: the
// agent performs, the record only says.
type Resume struct {
	// From is the name of the device that drove the chat, "" when unknown.
	From string `json:"from,omitempty"`
	// Was is the folder the chat worked in there and Now is the one it works in
	// here; a command that names Was is shown with the change said once.
	Was string `json:"was,omitempty"`
	Now string `json:"now,omitempty"`
	// Missing is the folders the seal left out that are not on this machine.
	Missing []inventory.Withheld `json:"missing,omitempty"`
	// Stopped is the commands that were running there and are not running here.
	Stopped []inventory.Running `json:"stopped,omitempty"`
	// Detached is what was started there outside the chat's own processes.
	Detached []inventory.Detached `json:"detached,omitempty"`
	// Lacks is what this machine needs that it does not have.
	Lacks []Item `json:"lacks,omitempty"`
	// Omitted is how many entries of each list the record itself cut.
	Omitted map[string]int `json:"omitted,omitempty"`
	// Uncommitted is the files in the folder here that differ from its last
	// commit. It is shown, never offered: it raises no card by itself.
	Uncommitted []string `json:"uncommitted,omitempty"`
	// Tests is the last test run the record holds, nil when the chat ran none.
	// Like Uncommitted it is shown, never offered.
	Tests *inventory.TestRun `json:"tests,omitempty"`
}

// Empty reports whether there is nothing to say. An empty Resume raises no card,
// sends no message and writes nothing anywhere (the emptiness law).
func (r Resume) Empty() bool { return !r.hasFacts() && len(r.Lacks) == 0 }

// hasFacts is whether the chat left anything behind, apart from tools this
// machine lacks, which the setup brief already names.
func (r Resume) hasFacts() bool {
	return len(r.Missing)+len(r.Stopped)+len(r.Detached) > 0
}

// Here is what this machine can say about the places and ports a record names.
// It is an interface so the comparison can be tested without a network.
type Here interface {
	// Has reports whether the folder at a record path is on this machine.
	Has(recordPath string) bool
	// Answers reports whether something accepts connections on the port here.
	Answers(port int) bool
}

// LocalHere is this machine: the workspace the tools run in and the cell's own
// folder, where a task copy's folders live.
type LocalHere struct{ Root, Workspace string }

// Has implements Here. A task copy's folder is named from the cell's own folder
// and every other from the workspace, as the record spells them.
func (h LocalHere) Has(recordPath string) bool {
	base := h.Workspace
	if strings.HasPrefix(recordPath, cell.TreesDir+"/") {
		base = h.Root
	}
	_, err := os.Stat(filepath.Join(base, filepath.FromSlash(recordPath)))
	return err == nil
}

// dialTimeout bounds one connection attempt to a port on this machine.
const dialTimeout = 200 * time.Millisecond

// Answers implements Here with one connection attempt to this machine's own
// loopback: the card reads no file and opens no socket beyond that.
func (LocalHere) Answers(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), dialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Compare is the resume of a chat that just arrived: the record the other
// machine sealed, set against this machine. A folder that is here already
// (taking a chat back onto the machine that kept its own) is not missing, and a
// recorded port that answers is not stopped.
func Compare(from string, inv inventory.Inventory, here Here, report Report) Resume {
	r := Resume{From: from, Was: inv.Workspace, Detached: inv.Detached, Omitted: inv.Omitted, Tests: inv.Tests}
	r.Missing = missingFrom(inv.Withheld, here)
	r.Stopped = stoppedFrom(inv.Running, here)
	r.Lacks = report.needs()
	return r
}

// Absent is the part of a resume that stays true after the takeover: what is
// not on this machine now, and what it lacks, read from the record this machine
// holds. The commands that were running elsewhere are known only at the moment
// of the takeover, before this machine seals a record of its own.
func Absent(inv inventory.Inventory, here Here, report Report) Resume {
	return Resume{Missing: missingFrom(inv.Withheld, here), Lacks: report.needs()}
}

func missingFrom(list []inventory.Withheld, here Here) []inventory.Withheld {
	var out []inventory.Withheld
	for _, w := range list {
		if !here.Has(w.Path) {
			out = append(out, w)
		}
	}
	return out
}

func stoppedFrom(list []inventory.Running, here Here) []inventory.Running {
	var out []inventory.Running
	for _, run := range list {
		if !answering(run.Ports, here) {
			out = append(out, run)
		}
	}
	return out
}

// answering reports whether any recorded port answers here. A command recorded
// with no port cannot be told from a stopped one, so it is listed.
func answering(ports []int, here Here) bool {
	for _, port := range ports {
		if here.Answers(port) {
			return true
		}
	}
	return false
}

// needs is what this machine lacks: every tool or service setup could act on,
// and every version that differs from the one the chat used. A different
// platform is the chat list's line and not a thing to set up.
func (r Report) needs() []Item {
	var out []Item
	for _, it := range r.Items {
		if it.Name != "platform" && (it.Bucket == Installable || it.Severity != None) {
			out = append(out, it)
		}
	}
	return out
}
