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
// ── WHERE EACH HALF OF THE BOUND APPLIES ──
//
// PROCESSES, ALWAYS. A subtree may not hold more than jobBoundProcesses (128,
// about four times the honest peak of 34). This is the fork storm's ceiling and
// it applies to every subtree, quota'd or not, because a fork that has not yet
// accumulated CPU is invisible to a rate and a cgroup CPU quota does nothing to
// a process count.
//
// CPU, ONLY WHERE NOTHING ELSE CAPS IT. When a cgroup CPU quota or an affinity
// mask BINDS this process — the effective cores it may use are fewer than the
// machine's ([processgroup.EffectiveCores] below runtime.NumCPU) — the quota is
// already the ceiling on what any subtree under it can burn, and #1187's park
// bound is what returns a turn wedged on a job that never ends. Adding a CPU
// rate rule there would only cut honest work early: inside a four-core cell an
// ordinary `go test` legitimately uses most of its four cores, and a share of
// them would end it. So the CPU rule is OFF under a binding quota, and only the
// process backstop applies.
//
// When NOTHING binds — the process may use the whole machine — there is no other
// ceiling, and the CPU rule is the only thing between a spinner storm and the
// box: a subtree may not sustain more than THREE FIFTHS of runtime.NumCPU. On
// the 20-core box the numbers came from that is 12 cores, clearing the honest
// peak (8.5) and cutting the storm (16 loops → 16 cores, 64 → 20).
//
// ── HOW THE CPU RATE IS TAKEN ──
//
// The rate is taken from two readings of the subtree's cumulative CPU over the
// wall between them, and the reading is the subtree's WHOLE tree — the shell
// and every compiler, spinner or child under it — so a command that forks is
// bounded as one job and not as its leader alone.
//
// ── SUSTAINED OVER HALF A MINUTE, NOT A SPIKE ──
//
// A build burst can pass 12 cores for several seconds on a big box, so the bound
// trips only after the subtree has been over on jobBoundStrikes consecutive
// readings — 15 at jobBoundInterval of 2 s, thirty seconds sustained. The
// transient never cuts anything and a real storm, over on every reading by
// construction, is cut half a minute in.
//
// ── PORTABLE FIRST ──
//
// The watch is this program's own and runs everywhere. It reads the subtree
// through the process-group handle #1155 gave, which works wherever a process
// group is a process group; the CPU-and-process reading itself is /proc, so off
// Linux it says it cannot say and the bound never cuts ([processgroup.Usage]).
// The subtree's USAGE needs no cgroup — a job is a process group and not a
// cgroup. Whether a quota BINDS is a different question — how many cores the
// subtree could ever reach — answered from the process's own cgroup CPU quota and
// affinity mask ([processgroup.EffectiveCores]), which off Linux is the machine
// count, so nothing binds and the CPU rule governs as it always did.

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
	// sustain before its bound trips, and it is read only when NOTHING caps the
	// process below the machine (see the file header).
	jobBoundCoreShare = 3.0 / 5.0

	// jobBoundProcesses is the process ceiling on one subtree, and it applies
	// always. It is about four times the honest peak (34) so it is never what
	// separates an honest run from the spinner storm, and it catches the fork
	// storm the CPU rate alone would miss.
	jobBoundProcesses = 128

	// jobBoundStrikes is how many consecutive readings over a ceiling the bound
	// waits for before it trips: at jobBoundInterval that is thirty seconds, long
	// enough that an honest build burst is never cut and a storm still is.
	jobBoundStrikes = 15

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
	jobBoundRule = "— it was ended for passing its bound: a job subtree may not sustain more than its share of the machine's cores, or grow past its process ceiling, for long. The command was cut, not waited on; do not go looking for it."
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
// it runs on, the cores it may actually use, the previous reading a rate is taken
// from, and how many readings in a row have been over.
type subtreeBound struct {
	// machineCores is runtime.NumCPU: the whole machine, and the number the CPU
	// ceiling is drawn from when nothing caps the process below it. Zero turns the
	// bound off — a machine the bound cannot size is one it must not cut on.
	machineCores float64
	// effectiveCores is the smallest of the machine, the cgroup CPU quota and the
	// affinity mask: how many cores this subtree could ever reach. When it is
	// below machineCores a quota or mask BINDS, and the CPU rule is off.
	effectiveCores float64

	last    time.Time
	lastCPU float64
	have    bool
	strikes int
	// lastCores is the rate the last reading computed, kept so the record can
	// name it.
	lastCores float64
}

// newSubtreeBound builds the bound for the machine this process runs on and the
// cores it can actually use.
func newSubtreeBound() *subtreeBound {
	return &subtreeBound{
		machineCores:   float64(runtime.NumCPU()),
		effectiveCores: processgroup.EffectiveCores(),
	}
}

// quotaBinds reports whether a cgroup CPU quota or an affinity mask holds this
// subtree below the machine's cores. When it does, the quota is already the
// ceiling and the CPU rate rule is off — only the process backstop applies.
func (b *subtreeBound) quotaBinds() bool {
	return b.effectiveCores > 0 && b.effectiveCores < b.machineCores
}

// ceilingCores is the CPU ceiling this subtree may sustain, or +Inf when a quota
// binds and the CPU rule does not apply.
func (b *subtreeBound) ceilingCores() float64 {
	if b.quotaBinds() {
		return math.Inf(1)
	}
	return jobBoundCoreShare * b.machineCores
}

// strike records one reading of the subtree and reports whether the bound has
// tripped. A rate needs two readings, so the first reading can only be judged on
// its process count; that is why the CPU half takes a tick to arm.
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
