package verify

// The baseline is a property of the TREE AND THE JOB, never of the leaf.
//
// SETTLEMENT §4 built a photograph with a before half and an after half, and the
// bare worker took both. What it could not do is see across a repair round. Every
// round is a new leaf with a new workspace object, so every round photographed
// the tree IT found — which, from the second round on, is a tree the job has
// already changed. A check the first round turned red is red in the second
// round's baseline, so it subtracts to nothing and is never a finding again.
// textual's s5 run walked 17 of 20 project checks down to 1 across four rounds
// and raised no regression at any of them.
//
// The rule this file states: THE BASELINE IS THE TREE BEFORE THE JOB'S FIRST
// CHANGE, TAKEN ONCE AND INHERITED BY EVERY CONTINUATION. Every round's after
// reading is subtracted from that one, so a check broken in round one is still a
// finding in round four.
//
// It is remembered against the tree's own path because that is what the baseline
// is a reading of, and against the job because a second job in the same
// directory is measuring a different piece of work — the first job's changes are
// the second job's world, and blaming them on it would convict every job that
// followed another. A job that arrives at a root somebody else's job baselined
// re-baselines it, which is the same rule read from the other side.

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync"
)

// rememberedTrees bounds how many trees this holds baselines for at once.
//
// It is a bound on a process's memory and not a policy: one reading holds two
// rosters and an entrypoint, so sixteen of them is kilobytes, and sixteen is
// more concurrent working directories than any surface in this program opens —
// the resident gives each job its own, and a headless run has exactly one. Past
// it the oldest is dropped, and dropping a baseline costs a re-photograph rather
// than a wrong answer.
const rememberedTrees = 16

type jobBaseline struct {
	job     string
	reading Reading
}

// baselines is process-scoped because a job is process-scoped: `aforge do` is
// one process for the whole job, and the resident holds every continuation of a
// job in the process that started it. It is deliberately NOT a file in the
// workspace — a run that wrote its own bookkeeping into the tree it is measuring
// would file that bookkeeping as something the work produced.
var baselines = struct {
	mutex sync.Mutex
	taken map[string]jobBaseline
	order []string
}{taken: map[string]jobBaseline{}}

// BaselineFor is the reading of this tree taken before this job's first change,
// when this job has one.
//
// ok is false for the first leaf of a job, and for a leaf whose job never took a
// reading at all — which is the same silence an unaffordable wall or an
// undiscoverable entrypoint produces, and it means the same thing: nobody
// looked.
func BaselineFor(root, job string) (Reading, bool) {
	key := treeKey(root)
	baselines.mutex.Lock()
	defer baselines.mutex.Unlock()
	held, ok := baselines.taken[key]
	if !ok || held.job != job {
		return Reading{}, false
	}
	return held.reading, true
}

// RememberBaseline records the reading this job will be judged against, for
// every leaf of it that follows.
//
// A reading that was never taken is not remembered: a zero Reading says nobody
// looked, and remembering it would make the next leaf inherit that silence
// instead of taking the photograph the job still owes.
func RememberBaseline(root, job string, reading Reading) {
	if !reading.Taken {
		return
	}
	key := treeKey(root)
	baselines.mutex.Lock()
	defer baselines.mutex.Unlock()
	if _, held := baselines.taken[key]; !held {
		baselines.order = append(baselines.order, key)
		for len(baselines.order) > rememberedTrees {
			delete(baselines.taken, baselines.order[0])
			baselines.order = baselines.order[1:]
		}
	}
	baselines.taken[key] = jobBaseline{job: job, reading: reading}
}

// ForgetBaselines drops everything remembered. Its only callers are tests, which
// share a process with each other and would otherwise inherit one another's
// trees.
func ForgetBaselines() {
	baselines.mutex.Lock()
	defer baselines.mutex.Unlock()
	baselines.taken = map[string]jobBaseline{}
	baselines.order = nil
}

// treeKey is the tree's own identity, spelled once. Cleaning rather than
// resolving symlinks is deliberate: every leaf of one job is handed the same
// spelling by the surface that built the workspace, and resolving would cost a
// stat of a directory that may have been removed since.
func treeKey(root string) string { return filepath.Clean(root) }

// JobKey is the identity a baseline is remembered against: a digest of the
// person's own request, whitespace-normalised.
//
// The request is the one thing every leaf of a job holds identically and no two
// jobs share — a continuation bought by the gate, an escalated retry and the
// first attempt are all working on the same ask, and the next errand in the same
// directory is not. It is digested rather than kept whole because this is a map
// key held for the life of a process and a request can be pages long.
func JobKey(request string) string {
	normalized := strings.Join(strings.Fields(request), " ")
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}
