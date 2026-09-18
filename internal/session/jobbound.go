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
// CPU: a subtree may not sustain more than THREE FIFTHS OF THE MACHINE'S CORES,
// and never fewer than twelve — because a share alone would cut an honest build
// on a small box, where the work can legitimately fill every core there is. On
// the 20-core box the numbers came from, that ceiling is 12 cores: it clears the
// honest peak (8.5) by about two fifths, and cuts the smallest measured storm
// (16 loops → 16 cores) by a quarter. The floor of twelve is above the honest
// peak, so it cannot be what cuts honest work as measured, and on a machine of
// twelve cores or fewer the CPU half is simply inert — that machine cannot
// produce the storm's shape at the scale that mattered.
//
// PROCESSES: a subtree may not hold more than 256 processes — seven and a half
// times the honest peak of 34, so it is never what separates an honest run from
// the spinner storm, and low enough that a fork storm (measured here as
// thousands) is caught long before it eats the machine's pid space.
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
// No cgroup is required: a job is a process group and not a cgroup, so there is
// no per-job cgroup to read — the portable reading is the only one there is.

import (
	"math"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

const (
	// jobBoundCoreShare is the share of the machine's cores a job subtree may
	// sustain before its bound trips.
	jobBoundCoreShare = 3.0 / 5.0

	// jobBoundCoreFloor is the least number of cores any subtree's ceiling
	// allows, so a small machine's honest work — which can fill every core it
	// has — is never cut. It is above the honest peak measured on a 20-core box
	// (8.5 cores).
	jobBoundCoreFloor = 12.0

	// jobBoundProcesses is the process ceiling on one subtree. It is far above
	// the honest peak (34) so it is never what separates an honest run from the
	// spinner storm, and it catches the fork storm the CPU rate alone would
	// miss.
	jobBoundProcesses = 256

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
	jobBoundRule = "— it was ended for passing its bound: a job subtree may not hold more than its share of the machine's cores, or grow past its process ceiling, for long. The command was cut, not waited on; do not go looking for it."
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

// subtreeBound is the running state of the bound on ONE job subtree: the machine
// it is measured against, the previous reading a rate is taken from, and how
// many readings in a row have been over.
type subtreeBound struct {
	// machineCores is how many cores this machine has, from which the CPU
	// ceiling is drawn. A value of zero turns the bound off — a machine the
	// bound cannot size is a machine it must not cut on.
	machineCores int

	last    time.Time
	lastCPU float64
	have    bool
	strikes int
	// lastCores is the rate the last reading computed, kept so the record can
	// name it.
	lastCores float64
}

// newSubtreeBound builds the bound for the machine this process is running on.
func newSubtreeBound() *subtreeBound {
	return &subtreeBound{machineCores: runtime.NumCPU()}
}

// ceilingCores is the CPU ceiling this machine's subtree may sustain.
func (b *subtreeBound) ceilingCores() float64 {
	return math.Max(jobBoundCoreShare*float64(b.machineCores), jobBoundCoreFloor)
}

// strike records one reading of the subtree and reports whether the bound has
// tripped. A rate needs two readings, so the first reading can only be judged on
// its process count; that is why the bound takes a few ticks to arm and not an
// instant.
func (b *subtreeBound) strike(now time.Time, usage processgroup.Usage, ok bool) bool {
	if !ok || b.machineCores <= 0 {
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
