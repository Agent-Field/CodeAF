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
	"slices"
	"sort"
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
	// tree is the workspace's own account of what the job had produced or
	// changed at the moment this reading was taken, digested. It is what makes
	// "has the tree moved since somebody looked" answerable without looking
	// again — see TreeState and TreeUnchangedSince.
	tree string
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

// BaselineFor is what this job settled about its own tree before its first
// change: the reading it took, or the reason it could not take one.
//
// ok is false only for the first leaf of a job. A later leaf inherits whichever
// answer the first one reached, and inherits it rather than re-deriving it,
// because both answers are facts about a tree that has since moved.
func BaselineFor(root, job string) (Reading, bool) {
	reading, _, ok := BaselineOf(root, job)
	return reading, ok
}

// BaselineOf is the same answer with the tree-state it was taken against, for
// the two readers that have to know whether anything has happened since.
//
// The pair exists rather than one call with three results because most readers
// want the reading and nothing else — the coverage settlement, the consumer
// scan, the removed-surface comparison — and a caller that has to ignore a
// return value is a caller that will one day ignore the wrong one.
func BaselineOf(root, job string) (Reading, string, bool) {
	key := treeKey(root)
	baselines.mutex.Lock()
	defer baselines.mutex.Unlock()
	held, ok := baselines.taken[key]
	if !ok || held.job != job {
		return Reading{}, "", false
	}
	return held.reading, held.tree, true
}

// TreeUnchangedSince says the job has produced or changed nothing since its
// reading was taken: the tree carries the same account of the work now as it did
// then.
//
// IT IS THE WHOLE CONDITION FOR NOT LOOKING AGAIN. A reading is a measurement of
// a tree, so a tree that has not moved has already been measured — and the run
// that measured it wrote the answer down. Every retake of an unchanged tree is
// an eighth of a wall spent to reproduce a roster the run is already holding.
//
// It is false where nothing was ever remembered, which is the honest answer: a
// question about "since" needs a moment to be since, and a caller with none must
// look rather than assume.
func TreeUnchangedSince(root, job, tree string) bool {
	_, held, ok := BaselineOf(root, job)
	return ok && held == tree
}

// TreeState is the workspace's own account of what a job has produced or
// changed, as one comparable word.
//
// The account is the artifact record — every file the run created or changed,
// settled against the disk by whoever holds it — and it is digested rather than
// kept because this is compared, never read: two states are the same state or
// they are not. The empty string is the honest spelling of an UNTOUCHED tree,
// which is the state most errands are in for their whole life, and it is what a
// job that has produced nothing yields at every reader.
//
// Sorted first, so two accounts of one tree that were assembled in different
// orders are one state. Paths are compared as they are recorded, because both
// sides of every comparison come from the same recorder.
func TreeState(record []string) string {
	paths := make([]string, 0, len(record))
	for _, entry := range record {
		if clean := strings.TrimSpace(entry); clean != "" {
			paths = append(paths, clean)
		}
	}
	if len(paths) == 0 {
		return ""
	}
	sort.Strings(paths)
	sum := sha256.Sum256([]byte(strings.Join(slices.Compact(paths), "\n")))
	return hex.EncodeToString(sum[:8])
}

// RememberBaseline records what this job's leaves are measured against, for
// every leaf of it that follows.
//
// IT REMEMBERS THE REFUSAL AS WELL AS THE READING. Why a reading could not be
// taken is a fact about the tree, the project and the wall, and none of those
// change between one round of a job and the next — so a job that could not
// photograph its tree pays for finding that out ONCE. textual's s6 leaf spent
// five and a half minutes of its wall on a suite that was killed at the
// ceiling; without this, every continuation of that job spends the same five
// and a half minutes to learn the same thing.
//
// tree is the state of the tree the reading is of, as TreeState spells it: what
// the job had produced or changed when it was taken. It is remembered beside the
// reading because the ONE question every later reader asks is whether anything
// has happened since, and the reading itself cannot answer it — a photograph
// holds no account of the world outside its own frame.
//
// A Reading that says nothing at all — neither taken nor carrying a reason — is
// not remembered. That is the zero value, it is what a caller that never looked
// produces, and remembering it would make the next leaf inherit a silence
// instead of taking the photograph the job still owes.
func RememberBaseline(root, job, tree string, reading Reading) {
	if !reading.Taken && strings.TrimSpace(reading.Unread) == "" {
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
	baselines.taken[key] = jobBaseline{job: job, reading: reading, tree: tree}
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
