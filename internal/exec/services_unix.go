//go:build !windows

package exec

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// ProcessStartTime is the platform identity helper used both when a service
// is recorded and when it is re-adopted. Darwin and the other Unix targets
// supported by codeaf expose lstart through ps; callers degrade safely when a
// platform does not.
func ProcessStartTime(pid int) (time.Time, error) {
	if pid <= 0 {
		return time.Time{}, errors.New("invalid pid")
	}
	//codeaf:plumbing ps read of a process start time
	command := exec.Command("ps", "-o", "lstart=", "-p", fmt.Sprint(pid))
	// ps renders lstart in the caller's locale, which reorders month and day.
	// Asking for C makes the one format we depend on the one we get; the
	// day-first layouts below keep a platform that ignores the request usable.
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "LC_TIME=C")
	output, err := command.Output()
	if err != nil {
		return time.Time{}, err
	}
	raw := strings.Join(strings.Fields(string(output)), " ")
	for _, layout := range []string{
		"Mon Jan 2 15:04:05 2006", "Mon Jan _2 15:04:05 2006",
		"Mon 2 Jan 15:04:05 2006", "Jan 2 15:04:05 2006", "2 Jan 15:04:05 2006",
	} {
		if parsed, parseErr := time.ParseInLocation(layout, raw, time.Local); parseErr == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("parse process start time %q", raw)
}

func serviceTreeAlive(pid int) bool { return serviceLeaderAlive(pid) }

func serviceTerminate(pid int) error { return processgroup.Terminate(pid) }

func unverifiedServiceStop(int) error { return nil }

func finishServiceStop(pid int) error {
	// Preserve the existing best-effort final POSIX sweep.
	_ = processgroup.Kill(pid)
	return nil
}

func waitProcessGroupStop(done <-chan struct{}) error { <-done; return nil }
