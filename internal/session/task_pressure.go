package session

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The admission governor: the machine's own answer to "may one more node
// start".
//
// ── WHY THE COUNT WAS NEVER THE RESOURCE ──
//
// The frontier used to hold a fixed two nodes at once, and two was a guess
// standing in for two real ceilings it could not see. A node is a whole agent
// — its own model calls, its own build, its own checkout — and what actually
// runs out when several of them work at once is CORES and MEMORY, not a
// number somebody picked. On a sixteen-core workstation two was leaving the
// machine idle; on a laptop already carrying somebody's compile, two was one
// too many. So the count became a person's own setting (task.parallel,
// default no limit) and the ceilings became these: load average per core, and
// available memory.
//
// The same lesson is written down in internal/exec's governor.go, from the
// incident that produced it — aforge pinning a laptop's fan — and the
// per-core figure here is the one it settled on. A task node is exactly the
// class that governor calls LOCAL WORK: it spawns real compilers and real
// test runs on this host, so the host's own reading is the right question to
// ask about it.
//
// ── IT GATES ADMISSION AND NOTHING ELSE ──
//
// Nothing running is ever touched. A node that has a worktree and a child
// agent keeps them however loaded the machine gets, because killing work to
// relieve pressure is how a run loses an hour to a coincidence — and because
// pressure DRAINS on its own: the running nodes finish, the reading falls,
// and the next pass admits. A governor that could also stop things would
// need a policy for which; one that can only hold the next start needs none.
//
// ── SILENCE IS NEVER A HOLD ──
//
// The readings come from /proc, which is Linux's. A platform without it
// answers "cannot say", and a governor that cannot say NEVER HOLDS: a person
// on a machine this package cannot measure gets exactly the scheduler they had
// before the governor existed, which is the only honest thing to do with a
// number nobody took.

const (
	// taskPressureTTL bounds how often the host is asked. Load average is a
	// one-minute decayed figure and MemAvailable moves in page-cache time;
	// neither can say anything new inside a second, while the frontier turns
	// on every landing and could ask far faster than that.
	taskPressureTTL = time.Second

	// taskPressurePoll is how often a HELD frontier re-asks, and it exists
	// because nothing else would ever wake it. Every other frontier pass is
	// caused — an admission, a landing, a resolution — and a machine getting
	// quieter causes nothing at all: somebody else's build finishing is not an
	// event this process can hear. Five seconds is slow enough that the poll
	// is two small file reads a minute per held queue, and fast enough that a
	// person watching a card marked "machine busy" sees it move rather than
	// wondering whether it is stuck.
	taskPressurePoll = 5 * time.Second
)

// machineReading is what the host said about itself at one moment.
type machineReading struct {
	// loadPerCore is the one-minute load average divided by the number of
	// cores. Per core rather than raw, because "four runnable threads" is a
	// crisis on a two-core laptop and an idle afternoon on a thirty-two-core
	// workstation, and the setting a person writes has to mean the same thing
	// on both.
	loadPerCore float64
	// availableMB is MemAvailable — what the kernel says a new process could
	// actually get — and NOT MemFree. Free memory on a working machine is
	// close to zero by design, because the page cache has the rest; gating on
	// it would hold every node on every machine that had read a file.
	availableMB int
}

// admissionGovernor is the gate, and it is shared by one session's frontier.
//
// It never blocks. A hold is a decision about one pass, re-asked by the next
// one, so there is no wait to bound and no way for the gate to wedge a queue
// it has stopped being right about.
type admissionGovernor struct {
	mu sync.Mutex
	// maxLoad is the per-core load average at or above which admission holds,
	// and 0 turns the load half off (config.KeyTaskMaxLoad).
	maxLoad float64
	// minFreeMB is the MemAvailable floor below which admission holds, and 0
	// turns the memory half off (config.KeyTaskMinFreeMB).
	minFreeMB int
	// read is the host, seamed so the tests state a machine instead of
	// borrowing whatever the machine running them happens to be doing. The
	// bool is the host's own honesty: false is "this platform cannot say".
	read func() (machineReading, bool)
	// now is the clock the TTL is measured against, seamed with read.
	now func() time.Time

	sample machineReading
	known  bool
	at     time.Time
}

// newAdmissionGovernor builds the gate over the real host, and returns nil
// when both halves are off — a governor with nothing to check is not a
// governor that always says yes, it is one that should not be consulted, and
// nil is how this package spells that (see [admissionGovernor.holds]).
func newAdmissionGovernor(maxLoad float64, minFreeMB int) *admissionGovernor {
	if maxLoad <= 0 && minFreeMB <= 0 {
		return nil
	}
	return &admissionGovernor{
		maxLoad:   maxLoad,
		minFreeMB: minFreeMB,
		read:      hostReading,
		now:       time.Now,
	}
}

// holds reports whether the next queued node waits on the machine rather than
// starting.
//
// A NIL GOVERNOR NEVER HOLDS, and that is the whole of what "the governor is
// off" means anywhere in this package: a scripted graph in a test, a session
// whose person zeroed both rows, and a build asked to schedule before this
// file existed all take the same path.
func (g *admissionGovernor) holds() bool {
	if g == nil {
		return false
	}
	reading, known := g.reading()
	if !known {
		// The host would not say. Silence is not pressure.
		return false
	}
	if g.maxLoad > 0 && reading.loadPerCore >= g.maxLoad {
		return true
	}
	if g.minFreeMB > 0 && reading.availableMB > 0 && reading.availableMB < g.minFreeMB {
		return true
	}
	return false
}

// reading is the host's answer, cached for taskPressureTTL.
func (g *admissionGovernor) reading() (machineReading, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now
	if g.now != nil {
		now = g.now
	}
	at := now()
	if g.known && at.Sub(g.at) < taskPressureTTL {
		return g.sample, true
	}
	if g.read == nil {
		return machineReading{}, false
	}
	sample, known := g.read()
	if !known {
		// A host that cannot answer is not cached as an answer: a machine that
		// grows a /proc between two passes should be believed on the second.
		g.known = false
		return machineReading{}, false
	}
	g.sample, g.known, g.at = sample, true, at
	return sample, true
}

// hostReading asks this machine what it is carrying. Both halves are read
// independently, and a half that cannot be read is reported as zero rather
// than as a failure of the whole: a kernel with a loadavg and no meminfo still
// has one true thing to say.
func hostReading() (machineReading, bool) {
	load, haveLoad := hostLoadPerCore()
	available, haveMemory := hostAvailableMB()
	if !haveLoad && !haveMemory {
		return machineReading{}, false
	}
	return machineReading{loadPerCore: load, availableMB: available}, true
}

// hostLoadPerCore reads the one-minute load average out of /proc/loadavg and
// divides it by the cores that are meant to carry it.
func hostLoadPerCore() (float64, bool) {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, false
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || load < 0 {
		return 0, false
	}
	cores := runtime.NumCPU()
	if cores < 1 {
		cores = 1
	}
	return load / float64(cores), true
}

// hostAvailableMB reads MemAvailable out of /proc/meminfo, in mebibytes.
//
// The file's own unit is kB and it is scanned line by line rather than parsed
// whole: MemAvailable is the third line on every kernel that has it, and the
// rest of the file is fifty rows nobody here has a question about.
func hostAvailableMB() (int, bool) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false
		}
		kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kilobytes < 0 {
			return 0, false
		}
		return int(kilobytes / 1024), true
	}
	return 0, false
}
