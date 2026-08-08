// Forging is the other half. Recognition without a writer is a shelf someone
// else has to stock, and nobody writes a workflow file by hand for a system
// that is supposed to learn — so the distiller, which already judges what a
// finished job taught, also judges whether its SHAPE is worth keeping. What it
// writes lands as an ordinary version in the craft repository: git history is
// the version history, the commit message is the evidence, and a file the
// parser refuses never reaches the shelf at all.
package resident

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// craftForgeRepairs is how many times a refused candidate is handed back to
// its writer. One: the parser's errors were written to be read by a model and
// fixed in a single edit, and a writer that cannot use them twice will not use
// them on the third try either.
const craftForgeRepairs = 1

// CraftCandidate is one workflow file exactly as the distiller wrote it —
// unparsed, because whether it is a workflow at all is this side's judgment.
type CraftCandidate struct {
	Name string
	YAML string
}

// CraftRepairFunc hands a refused candidate back to its writer with the
// parser's own words attached. Errors from craft.Parse and Validate name the
// step, the field, and the likely intent; this is the reader they were written
// for.
type CraftRepairFunc func(ctx context.Context, candidate CraftCandidate, problem string) (CraftCandidate, error)

// splitCraftDrafts separates the forge's candidates from the notebook's
// memories. One distiller call answers both questions, so the craft rides out
// on the same slice — and it is split off before the notebook's own limit
// truncates anything, because a workflow is not one of the five things a job
// may teach.
func splitCraftDrafts(facts []Learned) ([]Learned, []CraftCandidate) {
	var kept []Learned
	var drafts []CraftCandidate
	for _, fact := range facts {
		if fact.Craft != nil {
			if strings.TrimSpace(fact.Craft.YAML) != "" {
				drafts = append(drafts, *fact.Craft)
			}
			continue
		}
		kept = append(kept, fact)
	}
	return kept, drafts
}

// forgeCraft lands one candidate: parse, one repair round on the parser's own
// words, then a version whose commit message carries the job it came from. A
// candidate that is still invalid after its repair is dropped into a lesson
// rather than announced — the user did not ask for a workflow, so a failed
// attempt at one is the resident's own business to remember.
func (r *Reconciler) forgeCraft(ctx context.Context, node store.Node, candidate CraftCandidate) {
	if r == nil || r.store == nil {
		return
	}
	if r.craftMind == nil || r.craftMind.shelf == nil {
		// A distiller that judged this job's shape worth keeping has just had
		// that judgment thrown away because this process was assembled without
		// a craft repository. The user was never promised a workflow, so this
		// stays out of the thread — but it went out with no trace at all, which
		// made a whole class of missing know-how invisible. The lesson is the
		// trace.
		r.recordCraftLesson(node, candidate.Name, "this process has no craft repository, so the workflow was dropped")
		return
	}
	workflow, problem := parseCraftCandidate(candidate)
	for attempt := 0; problem != "" && attempt < craftForgeRepairs && r.craftMind.repair != nil; attempt++ {
		repaired, err := r.craftMind.repair(ctx, candidate, problem)
		if err != nil || strings.TrimSpace(repaired.YAML) == "" {
			break
		}
		if strings.TrimSpace(repaired.Name) == "" {
			repaired.Name = candidate.Name
		}
		candidate = repaired
		workflow, problem = parseCraftCandidate(candidate)
	}
	if problem != "" {
		r.recordCraftLesson(node, candidate.Name, problem)
		return
	}

	existing, err := r.craftMind.shelf.Load(workflow.Name)
	refined := err == nil && existing != nil
	message := fmt.Sprintf("forged from job %s: %s", node.ID, craftEvidenceLine(node))
	if refined {
		message = fmt.Sprintf("refined after %s\n\nfrom job %s: %s",
			r.craftRefinementReason(node), node.ID, craftEvidenceLine(node))
	}
	if _, err := r.craftMind.shelf.Save(workflow, message); err != nil {
		r.recordCraftLesson(node, workflow.Name, firstLine(err.Error()))
		return
	}
	r.queueLearningMoment(node.ID, forgedCraftMoment(workflow.Name, refined))
}

// parseCraftCandidate applies the repository's own read law before anything is
// written: shape, then the whole structural law at once. The returned problem
// is what the writer is shown, so it carries every breach rather than the
// first.
func parseCraftCandidate(candidate CraftCandidate) (*craft.Workflow, string) {
	workflow, err := craft.Parse([]byte(candidate.YAML))
	if err != nil {
		return nil, firstLine(err.Error())
	}
	if problems := workflow.Validate(); len(problems) > 0 {
		return nil, errors.Join(problems...).Error()
	}
	workflow.Clamp()
	return workflow, ""
}

// recordCraftLesson files a failed forge where the next one will see it. The
// scope is the craft's own, which is the same family the survival record uses,
// so a workflow's history and its failed attempts sit on one shelf.
func (r *Reconciler) recordCraftLesson(node store.Node, name, problem string) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "unnamed"
	}
	body := fmt.Sprintf("tried to forge the %s craft from this job and the file was invalid: %s",
		name, firstLine(problem))
	if _, err := r.store.RecordFactFrom(store.FactWriterDistiller, node.ID,
		CraftSurvivalKey(name), store.FactLesson, clipFactBody(body)); err != nil {
		// The lesson is the only trace a failed forge leaves — the user was
		// never told, by design. A write that vanished silently means the next
		// forge repeats the same mistake with nothing to learn from.
		log.Printf("craft forge lesson %s: %v", name, err)
	}
}

// craftRefinementReason says what a new version answers. A craft is refined
// because something went wrong in the open — a run that failed, or a user who
// took the wheel — and the commit that follows should name which.
func (r *Reconciler) craftRefinementReason(node store.Node) string {
	if redirected, err := r.store.HasCommandTarget(node.ID, store.CommandRedirect); err == nil && redirected {
		return "the user redirected this run"
	}
	if node.Status == store.Failed {
		if failure := firstLine(node.Error); failure != "" {
			return "a failed run: " + clipLabel(failure, 120)
		}
		return "a failed run"
	}
	return "what the last run showed"
}

func craftEvidenceLine(node store.Node) string {
	for _, candidate := range []string{node.Provenance.Intent, node.Title, node.Brief} {
		if line := firstLine(candidate); line != "" {
			return clipLabel(line, 160)
		}
	}
	return node.ID
}

// craftDistillerNote tells the distiller that this job ran learned know-how
// rather than a fresh plan, and how that went. It is the only way an updated
// workflow can be asked for: the pass that judges what a job taught is also
// the pass that can see the file's shape was what failed.
func (r *Reconciler) craftDistillerNote(node store.Node) string {
	reference := strings.TrimSpace(node.Provenance.Craft)
	if r == nil || r.craftMind == nil || reference == "" {
		return ""
	}
	name, commit := reference, ""
	if cut := strings.LastIndex(reference, "@"); cut > 0 {
		name, commit = reference[:cut], reference[cut+1:]
	}
	version := ""
	if commit != "" {
		version = " (version " + craftShortCommit(commit) + ")"
	}
	outcome := "it settled"
	switch {
	case node.Status == store.Failed:
		outcome = "it failed"
	case node.Status == store.Cancelled:
		outcome = "it was cancelled"
	}
	if redirected, err := r.store.HasCommandTarget(node.ID, store.CommandRedirect); err == nil && redirected {
		outcome += " and the user redirected it mid-run"
	}
	return fmt.Sprintf("[This job ran the %s craft%s rather than a fresh plan, and %s. "+
		"If the craft's own shape is what went wrong, emit an updated craft with the SAME name and the whole corrected file. "+
		"If the craft was fine and only this run's circumstances were not, leave it alone.]",
		name, version, outcome)
}

// craftForgedSince is the arrival brief's line about know-how that appeared
// while the user was away. It reads the repository rather than the journal
// because the repository is where a version's identity and its timestamp
// actually live — git history IS the version history, and a second copy of it
// in the store would be one more thing to keep true.
func (r *Reconciler) craftForgedSince(since time.Time) []BriefEvent {
	if r == nil || r.craftMind == nil || r.craftMind.shelf == nil {
		return nil
	}
	summaries, err := r.craftMind.shelf.List()
	if err != nil {
		return nil
	}
	var events []BriefEvent
	for _, summary := range summaries {
		if summary.When.IsZero() || !summary.When.After(since) {
			continue
		}
		events = append(events, BriefEvent{
			Time: summary.When, Kind: store.BriefSkill,
			Text: r.craftForgedLine(summary),
		})
	}
	return events
}

func (r *Reconciler) craftForgedLine(summary craft.Summary) string {
	line := "Learned how to do " + summary.Name
	if versions, err := r.craftMind.shelf.History(summary.Name, 1); err == nil && len(versions) > 0 {
		subject := strings.TrimSpace(strings.TrimPrefix(versions[0].Subject, summary.Name+":"))
		if rest, ok := strings.CutPrefix(subject, "forged"); ok {
			line = "Learned how to do " + summary.Name + " " + strings.TrimSpace(rest)
		} else if rest, ok := strings.CutPrefix(subject, "refined"); ok {
			line = "Got better at " + summary.Name + " " + strings.TrimSpace(rest)
		}
	}
	return strings.TrimSpace(line) + " — I'll work this way next time."
}
