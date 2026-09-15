package session

// ── THE MEMORY OF A FORK THAT COULD NOT BE MADE ──
//
// The ground ladder's top rung forks the whole workspace with furrow, and every
// way that can go wrong falls to the rung below — a bargain written for a pause
// paid once, and paid on every task instead (groundladder.go's header has the
// laptop's census).
//
// SO A FALL IS WRITTEN DOWN, PER GROUND, and the next node standing on that
// ground does not climb the rung again. What it does instead is say so, in its
// own log, with the reason the fork failed the last time it was tried
// ([universeFall.stood]).
//
// ── UNTIL SOMETHING THAT COULD CHANGE THE ANSWER HAS CHANGED ──
//
// A remembered fall holds only while the two things that decide it are the ones
// that produced it: THIS codeaf ([buildinfo.Identity]) — whose rung is the code
// that failed — and THE FURROW IT RUNS ([furrow.Program]) — whose fork is the
// other half. Either one changing is a new question and the rung is tried again,
// so a fix to either end is never held back by a memory of the bug it fixed. A
// fork that succeeds forgets the fall outright.
//
// WHAT THIS DOES NOT DO is try again on a timer. A fall that was a coincidence —
// a machine too loaded to fork inside its bound — stands the rung down for that
// ground until the next build, and the tasks in between get the snapshot rung's
// world, which is git's world and lacks the ignored files. That is the trade, and
// it is stated here rather than hidden in a retry: a rung that is tried on every
// task after it has failed is the fifteen seconds this file was written to end,
// and every line it prints names what was skipped and why.
//
// A STOP IS NOT A FALL ([universeRung.carve]): a task the person cancelled while
// its fork was being made says nothing about whether this ground can be forked.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/home"
)

// universeFallsName is the file the falls are kept in, under the state root's
// v3/ directory beside everything else this surface keeps per machine. It is per
// machine rather than per session because what it records is: whether THIS
// machine's furrow can fork THIS folder, which every conversation on the machine
// standing on that folder is asking.
const universeFallsName = "universe-falls.json"

// universeFall is one ground's remembered fall.
type universeFall struct {
	// Build and Furrow are what the fall holds for ([universeFallAt]).
	Build  string `json:"build"`
	Furrow string `json:"furrow"`
	// Why is the fork's own reason, in furrow's or git's words.
	Why string    `json:"why"`
	At  time.Time `json:"at"`
}

// stood is the line a node's log prints when this memory stands the rung down.
// It says what was not done, when it last failed, and why — the three things a
// person reading why a task's world lacks its `.env` needs, and nothing that
// would send them hunting for a file.
func (f universeFall) stood() string {
	line := "a fork of the whole folder was not tried: it could not be made here at " + f.At.Local().Format("Jan 2 15:04")
	if why := strings.TrimSpace(f.Why); why != "" {
		line += " · " + why
	}
	return line
}

// universeFallsMu serializes this process's reads and writes of the file. Two
// processes writing at once lose one of the two entries, which costs one more
// fork attempt and nothing else, so the file carries no lock of its own.
var universeFallsMu sync.Mutex

func universeFallsPath() string { return home.Join("v3", universeFallsName) }

// readUniverseFalls reads the whole memory. EVERYTHING IS TOLERATED: a missing
// file is the ordinary case, and one that does not parse is one some other
// version or an interrupted write left behind — starting empty costs one fork
// attempt per ground, which is survivable, where refusing to start is not.
func readUniverseFalls() map[string]universeFall {
	falls := map[string]universeFall{}
	raw, err := os.ReadFile(universeFallsPath())
	if err != nil {
		return falls
	}
	_ = json.Unmarshal(raw, &falls)
	return falls
}

// writeUniverseFalls replaces the file whole, through a rename, so that a reader
// in another process sees the old memory or the new one and never half of one.
func writeUniverseFalls(falls map[string]universeFall) {
	path := universeFallsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	raw, err := json.MarshalIndent(falls, "", "  ")
	if err != nil {
		return
	}
	staged, err := os.CreateTemp(filepath.Dir(path), universeFallsName+".*")
	if err != nil {
		return
	}
	name := staged.Name()
	_, werr := staged.Write(append(raw, '\n'))
	cerr := staged.Close()
	if werr != nil || cerr != nil || os.Rename(name, path) != nil {
		_ = os.Remove(name)
	}
}

// universeFallAt answers the fall remembered for this ground, when it still
// holds: the same codeaf, and the same furrow, as the one that failed.
func universeFallAt(ground string) (universeFall, bool) {
	ground = canonicalPath(ground)
	if ground == "" {
		return universeFall{}, false
	}
	universeFallsMu.Lock()
	defer universeFallsMu.Unlock()
	fall, ok := readUniverseFalls()[ground]
	if !ok || fall.Build != buildinfo.Identity() || fall.Furrow != furrow.Program() {
		return universeFall{}, false
	}
	return fall, true
}

// rememberUniverseFall writes one ground's fall down, over whatever was there.
func rememberUniverseFall(ground, why string) {
	ground = canonicalPath(ground)
	if ground == "" {
		return
	}
	universeFallsMu.Lock()
	defer universeFallsMu.Unlock()
	falls := readUniverseFalls()
	falls[ground] = universeFall{
		Build:  buildinfo.Identity(),
		Furrow: furrow.Program(),
		Why:    strings.TrimSpace(why),
		At:     time.Now(),
	}
	writeUniverseFalls(falls)
}

// forgetUniverseFall drops a ground's fall after a fork of it succeeded. It
// writes nothing when there is nothing to drop, which is every success on a
// machine where the rung works — so the ordinary road costs one small read.
func forgetUniverseFall(ground string) {
	ground = canonicalPath(ground)
	if ground == "" {
		return
	}
	universeFallsMu.Lock()
	defer universeFallsMu.Unlock()
	falls := readUniverseFalls()
	if _, ok := falls[ground]; !ok {
		return
	}
	delete(falls, ground)
	writeUniverseFalls(falls)
}
