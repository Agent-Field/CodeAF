package revision

// The fact one question further back than the changed-definition reading: a name
// the run's own sources READ that nothing in the tree binds.
//
// igel s14 is the whole of the case. The run rewrote `igel/configs.py` so that
// `temp_post_req_data_path` became a local inside a builder function, and
// `igel/servers/fastapi_server.py` still opened with
// `from igel.configs import temp_post_req_data_path`. All twenty-four hidden
// tests failed on `ImportError: cannot import name 'temp_post_req_data_path'`.
// The run's own checks were red and the gate said so — `The checks this work
// wrote fail: …` — and that sentence sends a worker to run a suite. It never
// said WHICH NAME, which is the one word the repair needed and the one word the
// tree could have supplied for nothing.
//
// Nothing else in this harness can reach it. The presence photograph compares
// two readings of the surface and this name is in neither; the changed-definition
// reading compares digests of names that are in both; the assertion door weighs
// behaviours the REQUEST states, and nobody states "and the names this code
// imports must exist". It is measured with no model, no toolchain and no type
// checker: verify.UnboundReferences reads the run's changed sources for
// references and the whole tree for bindings, and reports only the residue.
//
// EVERY SILENCE FAVOURS THE WORK, and there are a great many of them — see
// verify/unbound.go, where a dynamic construct, an unreadable base class, a
// module outside the tree and a partial index each cost this finding a whole
// scope rather than costing the work a false blocker.

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// unboundReferences is what THIS JOB's finished tree reads and does not bind,
// re-taken at the gate against the record it is judging.
//
// IT IS RE-TAKEN AND NEVER CARRIED, which is removedSinceTheJobBegan's rule
// applied to the same shape of finding and for the same reason: a name a round
// left unbound and a later round bound must stop being a finding, and only a
// reading of the finished tree can say so. It also makes the finding reachable
// at a gate that judges a parent — a grown subtree does its work in children,
// and the child's readings are the child's.
//
// It needs no baseline. The question is about the tree as it stands and not
// about what it used to be, which is the one reading in this file that a job
// with no photograph at all still gets.
//
// settled is false where there is no workspace to read, and there the leaf's own
// answer stands exactly as it did.
func (e Evidence) unboundReferences() (found []verify.UnboundName, settled bool) {
	root := strings.TrimSpace(e.Workspace)
	if root == "" {
		return nil, false
	}
	return verify.UnboundReferences(root, e.recordFiles()), true
}

// UnboundNames is the finding: this work reads names nothing defines.
//
// SOURCED, like the regression, the removed name and the run's own failing
// checks, and for the same reason: there is no citation to weigh. The request
// never said "and the names your code imports must exist", because nobody has
// to, and a door that asked for a quotation would refuse this finding every
// single time.
//
// It names the reference and the site and not a remedy. Binding the name and
// deleting the reference are both answers, and which one is right is the repair
// round's business — this says only what the tree does not hold.
//
// ok is false when nothing was measured or nothing was found. A settlement that
// cannot read the tree hands back nil, which reads as no claim and never as
// nothing unbound.
func UnboundNames(found []string) (judgment Judgment, ok bool) {
	kept := make([]string, 0, len(found))
	for _, one := range found {
		if one = strings.TrimSpace(one); one != "" {
			kept = append(kept, one)
		}
	}
	if len(kept) == 0 {
		return Judgment{}, false
	}
	named := kept
	if len(named) > regressionsNamed {
		named = named[:regressionsNamed]
	}
	gap := "This work reads names nothing defines: " + joinCitations(named) + "."
	if len(kept) > len(named) {
		gap += fmt.Sprintf(" And %d more.", len(kept)-len(named))
	}
	gap += " Each of these is a name a file this run left behind READS and that no file in " +
		"the tree binds — read from the source on disk, with no model and no type checker " +
		"in the loop. Bind them, or stop referring to them. Nothing that runs this code " +
		"reaches one of these lines without failing on it."
	return Judgment{
		Pass: false, Gaps: gap, Quote: joinCitations(named),
		Citations: named, Unbound: kept, Sourced: true, Checked: true,
		Finding: FindingUnbound,
	}, true
}

// journalUnbound writes what the reading found, INCLUDING when it found nothing.
//
// A row saying "the changed sources were read and every name they use is bound"
// is the difference between a run that looked and a run whose reader never ran,
// and those two were the same silence in every store this mechanism was built
// from (FAILSAFE.md clause 4). It is a measurement: a store that refuses the row
// changes nothing about what the gate does.
func journalUnbound(graph *store.Store, nodeID string, found []verify.UnboundName) {
	if graph == nil || strings.TrimSpace(nodeID) == "" {
		return
	}
	reading := store.UnboundReading{Found: len(found)}
	for _, one := range verify.UnboundNamed(found) {
		reading.Names = append(reading.Names, store.UnboundSite{
			Name: one.Name, Where: one.Where(), Ground: one.Ground})
	}
	_ = graph.RecordUnbound(nodeID, reading)
}
