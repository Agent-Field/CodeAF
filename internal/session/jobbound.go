package session

// A JOB SUBTREE HAS NO CEILING ON WHAT IT BURNS.
//
// #1158 gave a run a soft bound on its MEMORY. It gave it nothing on its CPU
// and nothing on how many processes it may hold, and those are the two a run
// that never spends a token can spend instead. The measured case: a model
// spawned busy loops as bash jobs — `yes`, `awk 'BEGIN{for(;;){}}'` — and one
// attempt drove 64 of them and the next 16, load 30-43 on a 20-core box.
// Nothing inside this program stopped it; only a teardown outside and a
// signature guard did, because every bound the run carried was stated in
// DOLLARS and a busy loop costs none.
//
// So the job subtree — the process group #1155 already records at launch with
// its leader's identity — gets a bound of its own, and a subtree that passes it
// is CUT and the run is TOLD, never stopped silently.
//
// ── WHAT THE SIGNAL IS, AND WHY CPU RATE IS THE ONE THAT SEPARATES ──
//
// The two shapes have to be told apart, and the process count cannot do it: the
// honest peak an actual verify reached was 34 processes and 8.5 cores, while
// the 16-loop storm is 16 processes — FEWER than the honest peak — and far more
// CPU than the honest peak ever held. So the CPU RATE is what separates them,
// and the process count is a second, much higher ceiling kept for the
// orthogonal storm (a fork that has not yet accumulated CPU).
//
// The rate is taken from two readings of the subtree's cumulative CPU over the
// wall between them, and the reading is the subtree's WHOLE tree — the shell
// and every compiler, spinner or child under it — so a command that forks is
// bounded as one job and not as its leader alone.
//
// ── THE CEILINGS, AGAINST THE NUMBERS THAT WERE MEASURED ──
//
// CPU: a subtree may not sustain more than THREE FIFTHS OF THE CORES IT CAN
// ACTUALLY USE, floored at ONE. The cores it can use are the effective cores —
// the smallest of the machine's cores, the cgroup CPU quota, and the affinity
// mask ([processgroup.EffectiveCores]) — and NOT the machine's raw count,
// because a job runs held under a quota: a cell's own subtree is capped at a few
// cores, and a ceiling drawn from the machine's twenty would sit above anything
// that quota lets the subtree reach and never trip. On the 20-core box the
// numbers came from, with nothing capping it, the ceiling is 12 cores: it clears
// the honest peak (8.5) and cuts the smallest measured storm (16 loops → 16
// cores). Under a four-core quota the same rule gives 2.4 cores, which is what
// cuts a storm on the machines cells actually run on. The floor of one keeps a
// single-core box from a ceiling of zero, where every subtree would be over at
// once.
//
// PROCESSES: a subtree may not hold more than 128 processes — about four times
// the honest peak of 34, so it is never what separates an honest run from the
// spinner storm, and low enough that a fork storm (measured here as thousands)
// is caught long before it eats the machine's pid space.
//
// ── SUSTAINED, NOT A SPIKE ──
//
// A single reading above a ceiling is a spike, and honest work has them. The
// bound trips only after the subtree has been over on jobBoundStrikes
// consecutive readings, so the transient never cuts anything and a real storm —
// which is over on every reading by construction — is cut within seconds.
//
// ── PORTABLE FIRST ──
//
// The watch is this program's own and runs everywhere. It reads the subtree
// through the process-group handle #1155 gave, which works wherever a process
// group is a process group; the CPU-and-process reading itself is /proc, so off
// Linux it says it cannot say and the bound never cuts ([processgroup.Usage]).
// The subtree's USAGE needs no cgroup — a job is a process group and not a
// cgroup, so there is no per-job cgroup to read. The CEILING it is judged
// against is drawn from the process's OWN cgroup CPU quota where there is one
// ([processgroup.EffectiveCores]), which is a different question — how many cores
// the subtree could ever reach — and off Linux falls back to the machine count.
//
// ── SILENCE IS NEVER A CUT ──
//
// A subtree whose usage cannot be read, or a machine whose cores cannot be
// sized, gets exactly the scheduler it had before the bound existed.

import (
	"math"
	"strconv"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

const (
	// jobBoundCoreShare is the share of the effective cores a job subtree may
	// sustain before its bound trips.
	jobBoundCoreShare = 3.0 / 5.0

	// jobBoundCoreFloor is the least number of cores any subtree's ceiling
	// allows: one core, so that a single-core machine — where the effective
	// cores are one and three fifths of that rounds toward nothing — never draws
	// a ceiling of zero, under which every subtree would be over at once. On any
	// larger machine the share is what governs.
	jobBoundCoreFloor = 1.0

	// jobBoundProcesses is the process ceiling on one subtree. It is about four
	// times the honest peak (34) so it is never what separates an honest run from
	// the spinner storm, and it catches the fork storm the CPU rate alone would
	// miss.
	jobBoundProcesses = 128

	// jobBoundStrikes is how many consecutive readings over the ceiling the
	// bound waits for before it trips: one reading is a spike, a storm is over on
	// every reading.
	jobBoundStrikes = 3

	// jobBoundInterval is how often the bound samples a subtree.
	jobBoundInterval = 2 * time.Second

	// jobBoundReason is what the run is handed when the bound trips: the family
	// the settle turn and the park speak in ([taskAskSettleReason],
	// [jobParkBoundReason]), said about the subtree. The work did not settle
	// within its bound, so it was ended — said out loud, never as a silent kill.
	jobBoundReason = "the job subtree was not settled within its bound"

	// jobBoundRule is the one clause the record carries about itself: the subtree
	// is not still running and nothing is coming for it, so a model must not go
	// looking for the job or run the command again.
	jobBoundRule = "— it was ended for passing its bound: a job subtree may not hold more than its share of the cores it can use, or grow past its process ceiling, for long. The command was cut, not waited on; do not go looking for it."
)

// jobSubtreeUsage reads one job subtree's usage. It is a variable so the bound
// can be exercised against injected readings — the storm a test states is a
// sequence of numbers and never real load, which on a shared box is the only
// safe way to prove a bound.
var jobSubtreeUsage = func(group processgroup.Group) (processgroup.Usage, bool) {
	return group.Usage()
}

// jobBoundNow is the clock the bound reads its intervals against. It is a
// variable for [jobSubtreeUsage]'s reason: a test drives the interval without a
// sleep standing in for causality.
var jobBoundNow = time.Now

// subtreeBound is the running state of the bound on ONE job subtree: the cores it
// is measured against, the previous reading a rate is taken from, and how many
// readings in a row have been over.
type subtreeBound struct {
	// effectiveCores is how many cores this subtree could ever reach — the
	// smallest of the machine, its cgroup quota and its affinity mask — from
	// which the CPU ceiling is drawn. A value of zero turns the bound off: a
	// subtree whose cores cannot be sized is one the bound must not cut on.
	effectiveCores float64

	last    time.Time
	lastCPU float64
	have    bool
	strikes int
	// lastCores is the rate the last reading computed, kept so the record can
	// name it.
	lastCores float64
}

// newSubtreeBound builds the bound for the cores this process can actually use.
func newSubtreeBound() *subtreeBound {
	return &subtreeBound{effectiveCores: processgroup.EffectiveCores()}
}

// ceilingCores is the CPU ceiling this subtree may sustain.
func (b *subtreeBound) ceilingCores() float64 {
	return math.Max(jobBoundCoreShare*b.effectiveCores, jobBoundCoreFloor)
}

// strike records one reading of the subtree and reports whether the bound has
// tripped. A rate needs two readings, so the first reading can only be judged on
// its process count; that is why the bound takes a few ticks to arm and not an
// instant.
func (b *subtreeBound) strike(now time.Time, usage processgroup.Usage, ok bool) bool {
	if !ok || b.effectiveCores <= 0 {
		// SILENCE IS NEVER A CUT. A subtree this machine cannot read gets the
		// scheduler it had before the bound existed.
		b.strikes, b.have = 0, false
		return false
	}
	cores := 0.0
	if b.have && now.After(b.last) {
		cores = (usage.CPUSeconds - b.lastCPU) / now.Sub(b.last).Seconds()
	}
	b.last, b.lastCPU, b.have, b.lastCores = now, usage.CPUSeconds, true, cores
	if usage.Processes > jobBoundProcesses || cores > b.ceilingCores() {
		b.strikes++
	} else {
		b.strikes = 0
	}
	return b.strikes >= jobBoundStrikes
}

// boundStep takes one reading of one job subtree and, when the bound trips, ends
// the subtree and hands the run the record. It reports whether the bound tripped.
// It is the whole body of the watch loop, factored out so a test drives it with
// injected readings and no goroutine and no clock.
func (r *jobRegistry) boundStep(one *job, bound *subtreeBound) bool {
	usage, ok := jobSubtreeUsage(one.group)
	if !bound.strike(jobBoundNow(), usage, ok) {
		return false
	}
	r.cutSubtreeBound(one, usage, bound.lastCores)
	return true
}

// watchSubtreeBound samples one job subtree on its own beat until the subtree
// ends or the bound cuts it. It runs for the life of the job and stops when the
// job does ([job.done]).
func (r *jobRegistry) watchSubtreeBound(one *job) {
	// ONLY A PROCESS SUBTREE HAS ONE. A watch, a task node and a render are jobs
	// for everything around them but none of them is a process group with a tree
	// under it, so there is nothing here for the bound to measure.
	if one.kind != jobKindBash {
		return
	}
	bound := newSubtreeBound()
	ticks := time.NewTicker(jobBoundInterval)
	defer ticks.Stop()
	for {
		select {
		case <-one.done:
			return
		case <-ticks.C:
			if r.boundStep(one, bound) {
				return
			}
		}
	}
}

// cutSubtreeBound ends a subtree that passed its bound and hands the run the
// record of why.
//
// IT IS A KILL THIS SESSION ASKED FOR, so the job's own ending reports nothing:
// a requested death speaks for itself only through the record written here, and
// a second note from [jobRegistry.settleExit] would be the registry narrating
// what this line just explained. The kin is the park's bound ([parkBoundNote]) —
// the news reaches the model on the same lane, in the same family.
func (r *jobRegistry) cutSubtreeBound(one *job, usage processgroup.Usage, cores float64) {
	if !one.requestKill() {
		// The job is already ending under somebody else's hand; there is nothing
		// left to cut and no reason to say anything twice.
		return
	}
	one.signal(syscall.SIGKILL)
	if r.notify == nil {
		return
	}
	r.notify(jobBoundRecord(one, usage, cores))
}

// jobBoundRecord is the record a subtree's bound hands the run. It names the
// subtree, says what it was holding, and carries the family's rule about itself.
func jobBoundRecord(one *job, usage processgroup.Usage, cores float64) string {
	held := strconv.Itoa(usage.Processes) + " processes"
	if cores > 0 {
		held += ", " + strconv.FormatFloat(cores, 'f', 1, 64) + " cores"
	}
	return jobBoundReason + ": job " + strconv.Itoa(one.id) + " was holding " + held + "\n" + jobBoundRule
}
