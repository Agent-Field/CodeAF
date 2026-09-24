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
// incident that produced it — codeaf pinning a laptop's fan — and the
// per-core figure here is the one it settled on. A task node is exactly the
// class that governor calls LOCAL WORK: it spawns real compilers and real
// test runs on this host, so the host's own reading is the right question to
// ask about it.
//
// ── A READING CANNOT SEE WHAT IT HAS JUST LET IN ──
//
// The governor used to answer one yes or no per frontier pass, from one
// reading, and the frontier marked every ready node running against it. That
// was right for one node and wrong for a fan: twenty parts handed out in one
// breath were twenty agents, twenty worktrees and later twenty builds, every
// one of them admitted against a machine read before any of them existed.
// Neither half of the reading could have caught it. Load average is a
// one-minute decayed figure, and a node's memory arrives with its first
// build, minutes after it started — so the reading that finally shows the
// burst is taken long after the burst was admitted.
//
// So THE GOVERNOR KEEPS A RESERVATION FOR THE WORK IT HAS LET IN, and it is
// asked once PER ADMISSION rather than once per pass ([admissionGovernor.admits]).
// Every running node is expected to need one node's FOOTPRINT of memory
// ([admissionGovernor.footprintLocked]). What the running nodes already hold
// is visible in the reading, as the resident memory of this process and
// everything it started, above what they held when this graph ran nothing. The
// part of the running nodes' footprints NOT yet visible is taken off
// MemAvailable before the floor is compared:
//
//	projected = MemAvailable − max(0, running × footprint − visible)
//
// and one more node starts only while `projected` is at or above the floor.
// The next admission in the same pass therefore sees the machine as it will
// be once the work already admitted is carrying its weight, not as it was
// before any of it started. And nothing is counted twice: as a node's memory
// becomes visible, MemAvailable falls by what `visible` rises by, so the
// projection stands still while the reading catches up and moves only when a
// node settles or the machine frees memory of its own.
//
// With nothing of this graph running there is nothing to reserve, and the
// first node is judged on the reading alone, exactly as every node was before
// the reservation existed. That is also why the fan a quiet machine starts at
// once is 1 + (MemAvailable − floor) ÷ footprint and not one fewer: the floor
// is the room the machine keeps for everything else, and the node being asked
// about is judged the same way the first one always was.
//
// ── WHY THE RESERVATION IS KEPT IN MEMORY AND NOT IN LOAD ──
//
// Memory is the resource that FAILS: a machine short of cores runs every build
// slower and finishes all of them, and a machine short of memory kills one or
// swaps until nothing finishes. And bounding admitted nodes by memory already
// bounds the CPU burst, because a footprint is at least one core's share of
// the machine's memory, so a quiet machine admits at most about one node per
// core — a compiler each is a machine's worth of load, not twenty machines'.
// The load half stays what it always was, the reading of a machine that is
// busy with somebody else's work, and the reservation adds nothing to it.
//
// ── IT GATES ADMISSION AND NOTHING ELSE ──
//
// Nothing running is ever touched. A node that has a worktree and a child
// agent keeps them however loaded the machine gets, because killing work to
// relieve pressure is how a run loses an hour to a coincidence — and because
// pressure DRAINS on its own: the running nodes finish, the reservation falls,
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
//
// ── ONE ACCOUNT FOR THE WHOLE PROCESS ──
//
// The visible half is this whole process and every descendant it started,
// because /proc cannot say which conversation started which compiler. A
// governor handed its OWN graph's count of running nodes therefore divided the
// whole tree's memory by one conversation's nodes: a second conversation's
// build, under way while this graph had two parts out, read here as three
// core-shares a node. And the measured footprint only rises, so that one
// reading narrowed every later fan in this conversation for the rest of the
// session. The same gap ran the other way on the reservation itself — each
// graph read the other's visible memory as covering part of its own — and both
// are issue #907.
//
// So THE COUNT THIS FILE IS HANDED COVERS EVERY LANE THE READING DOES, never
// one graph's. Both
// halves of `share = visible / running` and of `running × footprint − visible`
// are then read over the same population: every lane the process is running,
// and every byte the process holds above rest. Another conversation's build is
// counted as exactly what it is — memory held by lanes — against a divisor
// that already counts the lanes holding it, and no conversation can move
// another's per-node weight. The count comes from [TaskLanes] — one account,
// handed to every conversation the process opens ([Config.TaskLanes]) and
// written by every `TaskGraph.running` mutation through one door
// ([TaskGraph.takeLaneLocked], [TaskGraph.giveLaneLocked]). There is no clamp
// to a fraction of MemTotal, no timer decay and no per-graph correction,
// because the attribution gap is a fact about the machine the reading is of,
// and the account is kept where that machine is known.

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
	// is a handful of small file reads a minute per held queue, and fast
	// enough that a person watching a card marked "machine busy" sees it move
	// rather than wondering whether it is stuck.
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
	// totalMB is MemTotal and cores is how many CPUs this process may use.
	// Between them they say how the machine was built, which is where a node
	// nobody has measured yet takes its footprint from
	// ([admissionGovernor.footprintLocked]).
	totalMB int
	cores   int
	// workMB is the resident memory of this process and of every process it
	// started, which is where a running node's weight shows: its agent is in
	// this process and its builds and tests are this process's descendants. It
	// is the VISIBLE half of the reservation.
	workMB int
}

// admissionGovernor is the gate, and it is shared by one session's frontier.
//
// It never blocks. A hold is a decision about one admission, re-asked by the
// next pass, so there is no wait to bound and no way for the gate to wedge a
// queue it has stopped being right about.
type admissionGovernor struct {
	mu sync.Mutex
	// maxLoad is the per-core load average at or above which admission holds,
	// and 0 turns the load half off (config.KeyTaskMaxLoad).
	maxLoad float64
	// minFreeMB is the MemAvailable floor the projected reading must stay at
	// or above, and 0 turns the memory half, and its reservation, off
	// (config.KeyTaskMinFreeMB).
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

	// restMB is workMB as the last reading that found the process running
	// nothing saw it: the conversation, its tools' servers, whatever this
	// process carries with no node at work. What is above it is the nodes'.
	// rested says there has been such a reading; until there is one, the
	// first reading stands in for it, which counts none of the work already
	// under way as visible and so errs toward holding.
	restMB int
	rested bool
	// peakShareMB is the most visible memory per running LANE OF THE PROCESS
	// any reading of this session has seen, and it only rises. A share that fell whenever the
	// nodes happened to be between builds would hand the room back just before
	// the next build needs it — the same reason the kernel's own ru_maxrss is
	// a high-water mark.
	peakShareMB int
}

// TaskLanes is THE COUNT OF RUNNING LANES ON ONE MACHINE, and the one divisor
// the governor's reading is shared over. Every task graph that belongs to the
// same running codeaf writes its own starts and hand-backs into one of these,
// so the count beside a reading of the whole process tree is drawn from the
// same population the reading is (#907).
//
// WHICH GRAPHS SHARE ONE IS SAID, NOT ASSUMED. The process's own door builds
// one and hands it to every conversation it opens ([Config.TaskLanes]); a graph
// given none is alone in its process and keeps one of its own ([newTaskGraph]).
// A package-level variable would have been the same claim made silently, and it
// is a claim no library can make for its caller — a test binary is one process
// and a hundred unrelated machines.
//
// It is one mutex and one int, and that is the whole design. IT IS THE
// INNERMOST LOCK here: every caller either holds a graph's lock already or
// holds nothing, and nothing inside it calls back out, so it cannot be half of
// a cycle. It is nil-safe throughout, because a graph assembled field by field
// in a test has no account and a count of nothing is the truth about it.
type TaskLanes struct {
	mu sync.Mutex
	n  int
}

// NewTaskLanes builds the account one machine's conversations share. The door
// that opens conversations builds one for the life of the process and puts it
// on every [Config] it hands out.
func NewTaskLanes() *TaskLanes { return &TaskLanes{} }

// RunAdmission is the run engine's three admission verbs. It uses the same
// governor and process lane account as the node frontier without exposing the
// governor's readings across the engine seam.
type RunAdmission interface {
	// MayStart reads host pressure before one worker is launched.
	MayStart() bool
	// Started reserves the lane after a worker has been built.
	Started()
	// Returned releases that lane when its worker comes home.
	Returned()
}

type runAdmission struct {
	governor *admissionGovernor
	lanes    *TaskLanes
}

// NewRunAdmission builds the machine gate for either run door. Zeroing both
// ceilings gives the engine a nil gate, the governor's existing off rule.
func NewRunAdmission(maxLoad float64, minFreeMB int, lanes *TaskLanes) RunAdmission {
	governor := newAdmissionGovernor(maxLoad, minFreeMB)
	if governor == nil {
		return nil
	}
	if lanes == nil {
		lanes = NewTaskLanes()
	}
	return &runAdmission{governor: governor, lanes: lanes}
}

// MayStart samples the host with no graph lock held, because proc reads may
// stall; the shared count projects the footprint of all conversation workers.
func (g *runAdmission) MayStart() bool {
	running := g.lanes.running()
	g.governor.observe(running)
	return g.governor.admits(g.lanes.running())
}

// Started adds one run worker to the account the node frontier also reads.
func (g *runAdmission) Started() { g.lanes.take() }

// Returned removes the worker on the supervisor's return and drain roads.
func (g *runAdmission) Returned() { g.lanes.give() }

// take records one lane taken anywhere in the process.
func (a *TaskLanes) take() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.n++
	a.mu.Unlock()
}

// give records one lane handed back anywhere in the process.
func (a *TaskLanes) give() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.n > 0 {
		a.n--
	}
	a.mu.Unlock()
}

// running is how many lanes the whole process is running, which is the
// divisor the visible half is shared over.
func (a *TaskLanes) running() int {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}

// newAdmissionGovernor builds the gate over the real host, and returns nil
// when both halves are off — a governor with nothing to check is not a
// governor that always says yes, it is one that should not be consulted, and
// nil is how this package spells that (see [admissionGovernor.admits]).
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

// observe asks the host, at most once per taskPressureTTL, and learns from
// the answer.
//
// `running` IS THE WHOLE PROCESS'S COUNT OF RUNNING LANES and never one
// graph's ([TaskGraph.lanesTaken]), because the memory in the reading is the
// whole process tree's and /proc cannot attribute a compiler to the
// conversation that started it.
//
// IT IS CALLED BEFORE THE GRAPH'S LOCK, once a pass, and it is the only
// method here that touches /proc: the graph's lock is held by everything that
// announces a node, and no file read belongs under it, however cheap.
//
// THE FILES ARE READ WITH NO LOCK HELD AT ALL, for the same reason one step
// further out: [admissionGovernor.admits] IS called under the graph's lock, so
// a reading held under this governor's lock would be a graph lock waiting on
// /proc through the back door. Two passes that both find the sample stale both
// read, which is a few file reads done twice and the later answer kept — far
// cheaper than the hold it replaces.
func (g *admissionGovernor) observe(running int) {
	if g == nil {
		return
	}
	now := time.Now
	g.mu.Lock()
	if g.now != nil {
		now = g.now
	}
	at := now()
	fresh := g.known && at.Sub(g.at) < taskPressureTTL
	read := g.read
	g.mu.Unlock()
	if fresh || read == nil {
		return
	}
	sample, known := read()

	g.mu.Lock()
	defer g.mu.Unlock()
	if !known {
		// A host that cannot answer is not cached as an answer: a machine that
		// grows a /proc between two passes should be believed on the second.
		g.known = false
		return
	}
	g.sample, g.known, g.at = sample, true, at
	// AND THE READING IS LEARNED FROM ONLY WHEN IT IS FRESH, because only then
	// is `running` the count the memory in it belongs to.
	if running <= 0 || !g.rested {
		g.restMB, g.rested = sample.workMB, true
	}
	if running > 0 {
		if share := g.visibleLocked() / running; share > g.peakShareMB {
			g.peakShareMB = share
		}
	}
}

// admits reports whether one more node may start beside the `running` lanes
// the process already has out. It is asked once per admission, with a count
// that includes every node the same pass has already started, so a fan is
// admitted one node at a time against the machine each start leaves behind.
//
// The count is THE PROCESS'S, for the reason [admissionGovernor.observe] gives:
// the reservation is taken off a reading of the whole process tree, so it has
// to be the whole process's work that is reserved for. A conversation whose
// neighbour is fanning out therefore meets a machine that is genuinely fuller,
// rather than reading its neighbour's visible memory as cover for its own
// nodes (#907).
//
// It reads the sample [admissionGovernor.observe] took and nothing else, so
// it is safe under the graph's lock.
//
// A NIL GOVERNOR ADMITS EVERYTHING, and that is the whole of what "the
// governor is off" means anywhere in this package: a scripted graph in a
// test, a session whose person zeroed both rows, and a build asked to
// schedule before this file existed all take the same path.
func (g *admissionGovernor) admits(running int) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.known {
		// The host would not say. Silence is not pressure.
		return true
	}
	reading := g.sample
	if g.maxLoad > 0 && reading.loadPerCore >= g.maxLoad {
		return false
	}
	if g.minFreeMB > 0 && reading.availableMB > 0 && reading.availableMB-g.unseenLocked(running) < g.minFreeMB {
		return false
	}
	return true
}

// unseenLocked is the memory the process's running lanes are expected to need
// that the reading does not show yet: every one of them at one footprint, less
// what they are already visibly holding, and never below zero — a node
// carrying more than a footprint is carrying it IN the reading, where it is
// already counted.
func (g *admissionGovernor) unseenLocked(running int) int {
	if running <= 0 {
		return 0
	}
	return max(0, running*g.footprintLocked()-g.visibleLocked())
}

// visibleLocked is the memory the running lanes visibly hold: this process's
// tree above what it held with no lane out anywhere in it.
func (g *admissionGovernor) visibleLocked() int {
	return max(0, g.sample.workMB-g.restMB)
}

// footprintLocked is the memory one running node is expected to need.
//
// IT IS THE LARGER OF TWO THINGS THIS MACHINE SAYS, and neither is a number
// written here.
//
// The measured one is [admissionGovernor.peakShareMB]: the most visible memory
// per running lane this session's own readings have seen. It is the footprint
// of THIS project's builds on THIS machine, and it is the one that grows when
// the work turns out to be heavier than a machine's rule of thumb.
//
// The other is one core's share of the machine's memory, MemTotal over cores,
// and it is what a node is assumed to need before any of it has been measured.
// A node is local work whose heaviest act is a compiler or a test run, which
// takes a core and memory in the proportion the machine was built in; and a
// footprint of zero before the first measurement would admit the whole of a
// session's first fan against a reading that cannot see it, which is the
// very burst this reservation exists to stop.
func (g *admissionGovernor) footprintLocked() int {
	share := 0
	if g.sample.cores > 0 {
		share = g.sample.totalMB / g.sample.cores
	}
	return max(share, g.peakShareMB)
}

// hostReading asks this machine what it is carrying. Every part is read
// independently, and a part that cannot be read is reported as zero rather
// than as a failure of the whole: a kernel with a loadavg and no meminfo still
// has one true thing to say.
func hostReading() (machineReading, bool) {
	cores := max(runtime.NumCPU(), 1)
	load, haveLoad := hostLoad()
	available, total, haveMemory := hostMemoryMB()
	if !haveLoad && !haveMemory {
		return machineReading{}, false
	}
	return machineReading{
		loadPerCore: load / float64(cores),
		availableMB: available,
		totalMB:     total,
		cores:       cores,
		workMB:      treeResidentMB(os.Getpid()),
	}, true
}

// hostLoad reads the one-minute load average out of /proc/loadavg.
func hostLoad() (float64, bool) {
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
	return load, true
}

// hostMemoryMB reads MemAvailable and MemTotal out of /proc/meminfo, in
// mebibytes.
//
// The file's own unit is kB and it is scanned line by line rather than parsed
// whole: the two rows wanted are the first and third on every kernel that has
// MemAvailable, and the rest of the file is fifty rows nobody here has a
// question about.
func hostMemoryMB() (available, total int, ok bool) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	haveAvailable := false
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found || (key != "MemAvailable" && key != "MemTotal") {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		kilobytes, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil || kilobytes < 0 {
			continue
		}
		if key == "MemAvailable" {
			available, haveAvailable = int(kilobytes/1024), true
		} else {
			total = int(kilobytes / 1024)
		}
	}
	return available, total, haveAvailable
}

// treeResidentMB is the resident memory of one process and every process
// below it, in mebibytes, and zero where /proc cannot say.
//
// THE WALK FOLLOWS THE TREE DOWN from the root, through each thread's
// `children` file, rather than reading every process on the machine and
// sorting out whose is whose: what it reads scales with what this process
// started, not with how busy the box is — a full scan was measured at thirteen
// milliseconds on a machine running sixteen hundred processes, and this is
// the frontier's own goroutine. The kernel promises the children file exactly
// only for a tree that is stopped, so a process born or reaped as it is read
// can be missed, and that is the right precision for a figure that is re-read
// every second: a process missed once is counted by the next reading.
func treeResidentMB(root int) int {
	pageKB := int64(os.Getpagesize() / 1024)
	var kilobytes int64
	seen := map[int]bool{}
	for queue := []int{root}; len(queue) > 0; {
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		dir := "/proc/" + strconv.Itoa(pid)
		if raw, err := os.ReadFile(dir + "/statm"); err == nil {
			// statm's second field is the resident set, in pages.
			if fields := strings.Fields(string(raw)); len(fields) > 1 {
				if pages, err := strconv.ParseInt(fields[1], 10, 64); err == nil && pages > 0 {
					kilobytes += pages * pageKB
				}
			}
		}
		threads, err := os.ReadDir(dir + "/task")
		if err != nil {
			continue
		}
		for _, thread := range threads {
			raw, err := os.ReadFile(dir + "/task/" + thread.Name() + "/children")
			if err != nil {
				continue
			}
			for _, field := range strings.Fields(string(raw)) {
				if child, err := strconv.Atoi(field); err == nil {
					queue = append(queue, child)
				}
			}
		}
	}
	return int(kilobytes / 1024)
}
