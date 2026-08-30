package revision

// The fact the judge was never handed: a definition that kept its name and
// changed its shape, beside the code that still uses the old shape.
//
// igel s12 is the whole of the case. The presence photograph worked — the run
// deleted eight public attributes off `Igel`, the gate said so
// (`This work removed a public name that existed before it`), a repair round was
// bought, and the next reading came back `lost: 0`. Then all twenty-four hidden
// tests failed with `TypeError: 'Configs' object does not support item
// assignment`. The run had rebound the module name `configs` from a dict to an
// instance of a small class it wrote, and the class had `__getitem__` and no
// `__setitem__`; every name was still there and the thing behind one of them no
// longer answered to how it was used.
//
// Nothing in this harness could say that. The presence photograph compares NAMES
// and this name was kept. The check-level reading only sees what somebody wrote
// a check for. The assertion door only weighs behaviours the REQUEST states, and
// nobody states "and the config object must still support item assignment",
// because nobody has to.
//
// So the gate is handed the structure and nothing else: which declarations the
// run's own diff overlapped, and how the rest of the project uses those names —
// counted, grouped by a closed set of syntactic shapes, and sampled with the
// file, the line and the line's own text. Whether a class with `__getitem__` and
// no `__setitem__` will survive twenty callers that subscript-assign it is a
// judgement, and it is left to the reader that is paid to make judgements.
// FAILSAFE.md clause 1 for the reading, clause 2 for where it comes from.
//
// EVERY SILENCE FAVOURS THE WORK. No diff, no workspace, no reader for the
// language, no declaration the diff overlaps, no consumer found — each of those
// adds nothing at all, and the judge sees exactly what it saw before this
// existed.

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

const (
	// gateConsumersShare is what a known window spends on the changed
	// definitions and their consumers, and gateConsumersBytes is the bound when
	// the window is unknown. Both are in PERF.md.
	//
	// It is a share of its own rather than a slice of the tree block's, because
	// the tree block IS the deliverable and this is a record about it: a
	// definition's consumer list must never be the reason a changed file went
	// unprinted. Past the share the remaining definitions are NAMED with their
	// counts and their sites are left out, which is the tree block's own
	// whole-or-named rule read one level down.
	gateConsumersShare = 6
	gateConsumersBytes = 3 << 10

	// consumerSamples bounds how many sites one shape spells out. It is
	// observablesNamed's sibling and a slightly larger figure for the same
	// reason: a reader is learning WHERE to go and look, and three places say
	// that as well as thirty.
	consumerSamples = 3

	// consumerQuotes bounds how many consumer lines travel as the verdict
	// schema's enum. A refusal may be grounded on a line the judge was actually
	// SHOWN, so this is the sample above summed across the block, and past it
	// the enum stops growing while the block stays exactly as it is — which can
	// only make a refusal harder to state, never easier.
	consumerQuotes = 24
)

// changedDefinitions is what this JOB did to definitions the rest of the project
// uses, read from the job's own baseline and the tree on disk.
//
// THE BASELINE IS THE JOB'S AND NOT THIS NODE'S, which is the whole of what
// makes it reachable. A grown subtree does its work in children and is judged at
// the parent; the child's readings are the child's. verify.BaselineFor already
// holds the tree before the job's FIRST change, taken once and inherited by
// every continuation, and it carries the surface on every path including the
// ones where no check could be read at all.
//
// It is asked only where the job took a baseline AND there is a workspace to
// read the finished tree in. Either missing reads as no claim.
func (e Evidence) changedDefinitions(job string) []verify.ChangedDefinition {
	root := strings.TrimSpace(e.Workspace)
	if root == "" {
		return nil
	}
	held, ok := verify.BaselineFor(root, job)
	if !ok {
		return nil
	}
	record := e.recordFiles()
	changed := verify.ChangedDefinitions(root, held.Surface, record)
	if len(changed) == 0 {
		return nil
	}
	names := make([]string, 0, len(changed))
	for _, definition := range changed {
		names = append(names, definition.Name)
	}
	sites := verify.Consumers(root, names, verify.ChangedSources(root, record))
	used := make([]verify.ChangedDefinition, 0, len(changed))
	for _, definition := range changed {
		// A DEFINITION NOBODY USES IS NOT A FINDING AND NOT A BLOCK. The whole
		// value here is the tension between a declaration that moved and code
		// that did not, and a definition with no consumer has no tension in it —
		// carrying it would spend the judge's room saying nothing.
		if found := sites[definition.Name]; len(found) > 0 {
			definition.Consumers = found
			used = append(used, definition)
		}
	}
	return used
}

// consumersBlock is that reading as the judge is shown it.
//
// WHOLE OR NAMED, which is the tree block's rule applied to a smaller thing. A
// definition's sites are printed entire or the definition appears with its name,
// its file and its counts and no sites at all — never a list that stops. A
// sample cut in half reads as a project that uses the name twice.
func consumersBlock(changed []verify.ChangedDefinition, budget ctxbudget.Budget) string {
	if len(changed) == 0 {
		return ""
	}
	var body strings.Builder
	body.WriteString("What this run did to definitions the rest of the project uses. " +
		"This was read from the run's own diff and the files on disk, with no model in the " +
		"loop: each definition below KEPT ITS NAME and had its declaration rewritten, and the " +
		"sites under it are places this project still uses that name, outside the lines this " +
		"run changed.\n\n" +
		"A NAME IS NOT A CONTRACT. If what now stands behind one of these names cannot answer " +
		"to the way its consumers use it, this delivery breaks them whatever the checks say — " +
		"and you may refuse it on that, quoting the consumer's own line and naming its file.\n")
	room := budget.Share(gateConsumersShare, gateShareTotal, gateConsumersBytes)
	for _, definition := range changed {
		head := "\n" + definitionWords(definition) + "\n"
		sites := definitionSites(definition)
		if len(head)+len(sites) <= room {
			room -= len(head) + len(sites)
			body.WriteString(head + sites)
			continue
		}
		if len(head) > room {
			continue
		}
		room -= len(head)
		body.WriteString(head)
	}
	return strings.TrimRight(body.String(), "\n")
}

// definitionWords is one definition's own line: what it is, where it stands on
// either side of the work, and how many places still use it.
func definitionWords(definition verify.ChangedDefinition) string {
	line := "- `" + definition.Name + "` (" + definition.File + ") — the diff replaced " +
		definition.Before.Words() + " of that file as it was"
	if after := definition.After.Words(); after != "" {
		line += "; the declaration now stands at " + after
	}
	return line + ". " + plural(len(definition.Consumers), "place") + " still use this name:"
}

// definitionSites is the consumers grouped by shape, each group naming its size
// and spelling out a bounded sample with the line's own text.
//
// The text is there because the SHAPE is a reading and the line is the evidence
// for it. A reader that disagrees with `subscript-assign` can see what it was
// read off, which is the difference between a fact and an assertion.
func definitionSites(definition verify.ChangedDefinition) string {
	var body strings.Builder
	for _, group := range definition.Grouped() {
		body.WriteString("    " + group.Shape + " — " + plural(len(group.Sites), "site"))
		sample := group.Sites
		if len(sample) > consumerSamples {
			sample = sample[:consumerSamples]
		}
		body.WriteString(":\n")
		for _, site := range sample {
			body.WriteString("      " + site.Where() + "  " + site.Text + "\n")
		}
		if len(group.Sites) > len(sample) {
			body.WriteString("      and " + strconv.Itoa(len(group.Sites)-len(sample)) +
				" more like it\n")
		}
	}
	return body.String()
}

// consumerGrounds is what a refusal built on this reading may name and quote:
// the files the consumers live in, and the exact lines the block printed.
//
// ONLY WHAT WAS SHOWN. A refusal may be grounded on a line the judge read, never
// on one this program held back, because a contract nobody was shown is a
// contract nobody can satisfy — which is behaviourSpans' rule at the other door.
func consumerGrounds(changed []verify.ChangedDefinition) (files, lines []string) {
	seenFile, seenLine := map[string]bool{}, map[string]bool{}
	for _, definition := range changed {
		for _, group := range definition.Grouped() {
			sample := group.Sites
			if len(sample) > consumerSamples {
				sample = sample[:consumerSamples]
			}
			for _, site := range sample {
				if text := strings.TrimSpace(site.Text); text != "" && !seenLine[text] &&
					len(lines) < consumerQuotes {
					seenLine[text] = true
					lines = append(lines, text)
				}
				if !seenFile[site.File] {
					seenFile[site.File] = true
					files = append(files, site.File)
				}
			}
		}
	}
	return files, lines
}

// ConsumerFinding is the gate line a consumer-grounded refusal gets: the
// definition that moved, how many places still use it and how, and where they
// are.
//
// It is its own sentence rather than a paragraph inside the judge's prose for
// the reason every other finding in this package is one: the stream prints the
// first line of a gap, the journal keeps it as a field, and a finding that lives
// only inside somebody else's sentence is reachable by neither.
//
// ok is false when the quote names no site this reading holds, which is a
// verdict grounded on something else and left exactly as it is.
func ConsumerFinding(quote string, changed []verify.ChangedDefinition) (string, bool) {
	want := foldedSpan(quote)
	if want == "" {
		return "", false
	}
	for _, definition := range changed {
		for _, group := range definition.Grouped() {
			for _, site := range group.Sites {
				if have := foldedSpan(site.Text); have == "" || !strings.Contains(have, want) {
					continue
				}
				where := make([]string, 0, consumerSamples)
				for _, sample := range group.Sites {
					if len(where) == consumerSamples {
						break
					}
					where = append(where, sample.Where())
				}
				finding := definition.Name + " changed and its " +
					plural(len(group.Sites), "consumer") + " still use it as " +
					group.Shape + ": " + strings.Join(where, ", ")
				if len(group.Sites) > len(where) {
					finding += " and " + strconv.Itoa(len(group.Sites)-len(where)) + " more"
				}
				return finding, true
			}
		}
	}
	return "", false
}

// removedSinceTheJobBegan re-settles what this JOB has deleted from the public
// surface, against the tree as it stands at judging time.
//
// A NAME THE JOB LOST IS A FINDING AT EVERY GATE OF THAT JOB UNTIL THE TREE HAS
// IT BACK, and until this existed it was a finding only at the gate of the leaf
// that happened to lose it. igel s14 is the measured case: three grown leaves
// journaled `surface {"lost": 3, "names": ["init_file_path", "res_path",
// "temp_post_req_data_path"]}`, every one of the twenty-four hidden tests failed
// with `ImportError: cannot import name 'temp_post_req_data_path'`, and both of
// that job's gates cited a missing file and nothing else — because the gate read
// the judged node's own outcome and the judged node was not the leaf.
//
// IT REPLACES rather than adds to what a leaf measured, and that is the point of
// re-taking it: a name a round lost and a later round put back must stop being a
// finding, and only a fresh reading of the finished tree can say so. This is
// SETTLEMENT §8's rule — a repair is settled against the world — applied to the
// one finding that was still being carried from a leaf.
//
// settled is false where there is no baseline, no workspace, or no changed
// source to compare, and there the leaf's own answer stands exactly as it did.
func (e Evidence) removedSinceTheJobBegan(job string) (lost []string, settled bool) {
	root := strings.TrimSpace(e.Workspace)
	if root == "" {
		return nil, false
	}
	held, ok := verify.BaselineFor(root, job)
	if !ok || len(held.Surface) == 0 {
		return nil, false
	}
	gone, compared := verify.LostNames(root, held.Surface, e.recordFiles())
	if compared == 0 {
		return nil, false
	}
	return gone, true
}

// journalConsumers writes what the reading found, INCLUDING when it found
// nothing that mattered.
//
// A row saying "three definitions were touched and nothing else uses them" is
// the difference between a run that looked and a run whose reader never ran, and
// those two were the same silence in every store this mechanism was built from
// (FAILSAFE.md clause 4). It is a measurement: a store that refuses the row
// changes nothing about what the gate does.
func journalConsumers(graph *store.Store, nodeID string, changed []verify.ChangedDefinition) {
	if graph == nil || strings.TrimSpace(nodeID) == "" {
		return
	}
	reading := store.ConsumersReading{Weighed: len(changed)}
	for _, definition := range changed {
		row := store.ChangedDefinition{Name: definition.Name, File: definition.File,
			Sites: len(definition.Consumers)}
		for _, group := range definition.Grouped() {
			sample := make([]string, 0, consumerSamples)
			for _, site := range group.Sites {
				if len(sample) == consumerSamples {
					break
				}
				sample = append(sample, site.Where())
			}
			row.Shapes = append(row.Shapes, store.ConsumerShape{
				Shape: group.Shape, Count: len(group.Sites), Sample: sample})
		}
		reading.Changed = append(reading.Changed, row)
	}
	_ = graph.RecordConsumers(nodeID, reading)
}
