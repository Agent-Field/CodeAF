package enginehost

// status.go answers the question a person used to answer with `ps` and a kill
// by hand: WHICH ENGINE IS HOLDING THIS FOLDER, and what is it.
//
// On 2026-09-23 a machine had a new `codeaf engine --daemon` exit without a
// word because a two-day-old engine from another binary held the slot, and the
// only way to see that was the process table. The host has always known what it
// is; this is the door that asks it, and the kernel's answer for the one kind of
// host that cannot be asked — a build from before the version exchange.

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// ErrNothingHolding is a workspace nobody is holding: no socket, or a socket
// nobody is listening on. It is the ordinary state of every folder, and a
// caller says "none" rather than reporting a failure.
var ErrNothingHolding = errors.New("engine host: nothing is holding this workspace")

// Holder is what is holding one workspace, as far as it could be learned.
type Holder struct {
	// Self is the host's own account of itself, with the pid and the binary
	// filled from the kernel when the host is too old to say them.
	Self remote.HostSelf
	// Answered is whether the host answered the question at all. False is a
	// build older than the exchange: it is known only by the process on the
	// other end of its socket.
	Answered bool
}

// Inspect asks the host holding this workspace what it is, and asks nothing
// else of it: the question carries no stand-down.
//
// THE KERNEL IS ASKED FIRST, for [Stop]'s reason: the answer to the question may
// be that the process cannot answer questions, and a pid is still worth having
// for a person deciding what to do about it.
func Inspect(workspace string) (Holder, error) {
	conn, err := Dial(workspace)
	if err != nil {
		if errors.Is(err, ErrSocketPathTooLong) {
			return Holder{}, err
		}
		return Holder{}, ErrNothingHolding
	}
	pid, _ := peerPID(conn)
	_ = conn.SetDeadline(time.Now().Add(askTimeout))
	self, askErr := remote.AskHost(conn, remote.WhoIs{})
	_ = conn.Close()
	held := Holder{Answered: askErr == nil}
	if askErr == nil {
		held.Self = self
	}
	if held.Self.PID <= 0 {
		held.Self.PID = pid
	}
	if strings.TrimSpace(held.Self.Binary) == "" && held.Self.PID > 0 {
		held.Self.Binary = processBinary(held.Self.PID)
	}
	if strings.TrimSpace(held.Self.Workspace) == "" {
		held.Self.Workspace = workspace
	}
	return held, nil
}

// processBinary is the file a process is running, read off the process itself,
// and "" when the machine will not say. It is only ever asked about a host too
// old to name its own binary, so it is a courtesy to the person reading the
// status line and never an input to a decision.
func processBinary(pid int) string {
	if pid <= 0 {
		return ""
	}
	if runtime.GOOS == "linux" {
		if path, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe"); err == nil {
			return strings.TrimSuffix(path, " (deleted)")
		}
		return ""
	}
	if runtime.GOOS == "windows" {
		return ""
	}
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
