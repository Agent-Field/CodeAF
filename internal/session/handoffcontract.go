package session

// THE HANDOFF CONTRACT: A BRIEF, A GROUND, AND SOMETHING CHECKABLE BETWEEN THEM.
//
// ── THE RUN THIS WAS WRITTEN FROM ──
//
// A dividing parent wrote excellent briefs. They named exact files and exact
// symbols of the world it had just built: "taskstrip.go is gone; the survivors
// live in taskchip.go (stripKey, ParentID…)"; "hover.go lost hoverStrip and
// gained hoverYolo". Every one of those sentences was TRUE OF THE PARENT'S
// WORKING TREE and false of the folder its workers woke up in. A cheap worker
// then spent twenty-two minutes and $7.99 rewriting a test file for a component
// its own brief said had been deleted, four lines at a time, until the step cap
// killed it.
//
// The ground law next door (groundladder.go) is half of the answer and was the
// direct cause here. The other half is this file, and it is the half that
// survives every OTHER way a brief and a world can come apart: a re-grounded
// task, a folder somebody moved, a dependency that finished after the brief was
// written, a person editing between the proposal and the approval.
//
// ── THE LAW ──
//
// A HANDOFF IS A BRIEF, A GROUND, AND A CHECKABLE MANIFEST — AND THE WORKER'S
// FIRST SPEND IS VERIFYING THE CONTRACT. A mismatch must be an instant,
// costless, DIAGNOSABLE landing that names what was missing, rather than an
// expensive loop nobody can read afterwards.
//
// ── THE SHAPE, AND WHY IT HAS NO DOMAIN WORDS IN IT ──
//
// An expectation is A PLACE, and optionally SOMETHING THAT MUST BE FINDABLE IN
// IT, and whether it must be there or must not. That is the whole taxonomy, and
// it is deliberately not a list of kinds: a file, a folder, a note, an asset, a
// dataset and a transcript are all places, and a symbol, a heading, a column
// name and an identifier are all text inside one. A schema that enumerated
// "symbol", "column", "url" would be aforge deciding in advance what domains it
// works in, which it does not get to do.
//
// WHAT IT DOES NOT REACH, said out loud: an expectation aforge cannot check
// against a directory — a URL that must fetch, an account that must still be
// logged in — is not accepted here, because the value of the manifest is that
// every line of it is answered before the money is spent. Those belong in the
// brief's prose, where they already went.
//
// ── AND IT IS NEVER INVENTED ──
//
// NO EXPECTATION IS EVER MINED OUT OF PROSE. The divider writes them, because
// only the divider knows which sentences of its own brief are load-bearing. A
// harness that mined paths out of the prose would be guessing at what the brief
// meant and then landing somebody's work on the guess — and it would be wrong
// in exactly the direction that is most expensive, since a brief mentions many
// paths and depends on few. A brief with no expectations preflights nothing and
// costs nothing, which is the honest default and the common case.
//
// AND THERE IS EXACTLY ONE BRIEF WITH NO AUTHOR TO WRITE THEM. A conversation
// whose turn is moved out because it cannot carry on did not know it was going
// to be handed over, so nobody wrote a manifest and the harness holds facts
// nobody else does: the turn's own tool calls say which places it actually
// wrote. That is a RECORD and not a reading of anybody's meaning, which is why
// [Agent.continuationExpects] at the foot of this file is allowed to exist and
// why it is the only thing here that writes one.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Expectation is one thing a brief relies on being true of the world its worker
// will get.
type Expectation struct {
	// Path is where to look, relative to the folder the work is about or
	// absolute. It is required: an expectation with nowhere to look is one
	// nothing can answer, and this file's header says why that is refused
	// rather than carried.
	Path string `json:"path"`
	// Holds is text that must be findable at Path — a symbol, a heading, a
	// column name, an identifier. Empty asks only that the path is there.
	Holds string `json:"holds,omitempty"`
	// Absent turns the expectation around: this path must NOT be there. It is
	// how a divider says "the file you are thinking of is gone", which is
	// exactly the sentence the run this was written from got wrong.
	Absent bool `json:"absent,omitempty"`
	// Fact is the assumption in the divider's own words, and it is optional
	// because most expectations say themselves. When it is there it is what the
	// report reads, because the person who wrote the brief can say why the
	// thing matters and this file cannot.
	Fact string `json:"fact,omitempty"`
}

// expectsSchemaJSON is the `expects` property, and BOTH doors that take a brief
// from a model carry it: the divider's (task_divide.go) and the conversation's
// (task.go). It is ONE constant interpolated twice rather than two literals,
// because a second account of the same shape is how a field comes to mean one
// thing at one door and something else at the other.
//
// THE SECOND DOOR WAS PAID FOR AND NOT BORROWED, said here because this is
// where somebody will look for the reason. propose_task's schema rides in front
// of every request of every turn of every conversation and is the heaviest tool
// on that belt, so this text reached it only once its 1,230 bytes had been
// taken back out of prompts/system.md: four lines whose every clause the tool
// block already carried, each in the description of the tool it was about
// (prefixbudget_test.go states that law, and is what would have failed).
//
// The asymmetry between the doors is real and was never the argument: a divider
// writes a brief about a world its reader has not been given yet, while a
// conversation proposing a task can see the folder it is proposing against —
// and a conversation proposing work against a folder it has NOT opened, or
// against a project three directories away, is the same handoff this file was
// written from. Nothing below cares which door filled the list.
//
// IT IS WRITTEN FOR DENSITY, like everything else that rides in front of every
// request: each rule is stated once, in the field it governs.
const expectsSchemaJSON = `"expects":{"type":"array","description":"Optional. What this brief assumes is already true of the folder the worker gets, checked before it spends anything. One entry per assumption it leans on, never a survey of the folder","items":{"type":"object","properties":` +
	`{"path":{"type":"string","description":"Where to look: a path relative to the folder the work is about, or absolute"},` +
	`"holds":{"type":"string","description":"Optional text that must be findable there. Left out, only the path itself is checked"},` +
	`"absent":{"type":"boolean","description":"Optional. true when this path must NOT be there, which is how you say a file has been deleted"},` +
	`"fact":{"type":"string","description":"Optional. The assumption in your own words, which is what the report says when it does not hold"}},` +
	`"required":["path"],"additionalProperties":false}}`

// expectsRemembered caps how many expectations one handoff carries.
//
// It is a bound on the CONTRACT and not on the work: a divider that wrote forty
// expectations has written a survey of the folder rather than the few facts its
// brief depends on, and every one of them is read on the way into a prompt and
// walked at the start of a run. The cap refuses the surplus at the door rather
// than silently dropping it, so a divider is told it wrote too many.
const expectsRemembered = 24

// expectsScanLimit and expectsReadLimit bound the walk one expectation may
// cost. A `holds` aimed at a folder searches the files under it, and a folder
// with a build output in it holds hundreds of thousands; a `holds` aimed at a
// file reads it, and a file may be a gigabyte of captured log. Both bounds are
// generous for the thing being asked and small enough that the whole preflight
// stays what it promises to be — cheaper than one model call.
const (
	expectsScanLimit = 2000
	expectsReadLimit = 4 << 20
)

// parseExpectations reads the wire form and says, in plain words, what is wrong
// with it. An absent list is not an error and never will be: the manifest is
// optional, and a brief that assumes nothing is the common case.
func parseExpectations(raw []Expectation) ([]Expectation, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	if len(raw) > expectsRemembered {
		return nil, fmt.Sprintf("Invalid arguments: expects carries %d entries, which is more than the %d one handoff holds — keep the ones the brief actually depends on", len(raw), expectsRemembered)
	}
	out := make([]Expectation, 0, len(raw))
	for _, one := range raw {
		one.Path = strings.TrimSpace(one.Path)
		one.Holds = strings.TrimSpace(one.Holds)
		one.Fact = strings.TrimSpace(one.Fact)
		if one.Path == "" {
			return nil, "Invalid arguments: every entry in expects needs a path — an expectation with nowhere to look is one nothing can check"
		}
		if one.Absent && one.Holds != "" {
			// The two together say "this must not be there, and this must be
			// inside it", which cannot both be wanted. Refusing beats picking
			// one and landing somebody's work on the choice.
			return nil, "Invalid arguments: an entry in expects cannot be both absent and hold something: " + one.Path
		}
		out = append(out, one)
	}
	return out, ""
}

// says is the one sentence an expectation makes about itself: the divider's own
// words when they wrote any, and otherwise the plainest reading of the fields.
func (e Expectation) says() string {
	if e.Fact != "" {
		return e.Fact
	}
	switch {
	case e.Absent:
		return e.Path + " is gone"
	case e.Holds != "":
		return e.Path + " holds " + e.Holds
	}
	return e.Path + " is there"
}

// unmet is the sentence a person and a parent read when this expectation did not
// hold. It says WHAT WAS FOUND rather than restating what was wanted, because
// the difference between the two is the whole of the diagnosis.
func (e Expectation) unmet(found string) string {
	line := "· " + e.says() + " — " + found
	if e.Fact != "" {
		// The divider's own words say why it matters and never where to look,
		// so the place is added back for whoever has to go and see.
		line += " (" + e.Path + ")"
	}
	return line
}

// preflightExpectations answers every expectation against one folder, and hands
// back the sentences for the ones that did not hold.
//
// IT COSTS NO MODEL CALL AND MAKES NO NETWORK CONNECTION. That is the promise
// the whole file rests on: a handoff whose contract does not hold has to be
// cheaper to discover than the first step of working on it, or nobody will ever
// leave the check turned on.
//
// dir is the folder the worker will actually work in — its world, whichever
// rung of the ground ladder made it (groundladder.go) — and not the ground it
// was copied from. Checking the ground would answer a question nobody asked: the
// worker cannot see the ground, and a contract held up against a folder the
// worker will never open is a contract that passes while the work fails.
func preflightExpectations(dir string, expects []Expectation) []string {
	dir = strings.TrimSpace(dir)
	if dir == "" || len(expects) == 0 {
		return nil
	}
	var unmet []string
	for _, one := range expects {
		path := one.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, filepath.FromSlash(path))
		}
		info, err := os.Stat(path)
		if one.Absent {
			if err == nil {
				unmet = append(unmet, one.unmet("it is still there"))
			}
			continue
		}
		if err != nil {
			unmet = append(unmet, one.unmet("it is not there"))
			continue
		}
		if one.Holds == "" {
			continue
		}
		switch found, readable := findsText(path, info, one.Holds); {
		case !readable:
			unmet = append(unmet, one.unmet("it could not be read"))
		case !found && info.IsDir():
			unmet = append(unmet, one.unmet("nothing under it says "+one.Holds))
		case !found:
			unmet = append(unmet, one.unmet("it does not say "+one.Holds))
		}
	}
	return unmet
}

// findsText looks for one string at one place: inside a file, or inside the
// files under a folder. It answers whether the text was found and whether the
// place could be read at all, because "not there" and "I could not look" are
// different news and only the first is the contract failing.
func findsText(path string, info fs.FileInfo, text string) (found, readable bool) {
	if !info.IsDir() {
		if info.Size() > expectsReadLimit {
			return false, false
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return false, false
		}
		return strings.Contains(string(contents), text), true
	}
	scanned := 0
	_ = filepath.WalkDir(path, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if scanned++; scanned > expectsScanLimit {
			return filepath.SkipAll
		}
		details, err := entry.Info()
		if err != nil || details.Size() > expectsReadLimit {
			return nil
		}
		contents, err := os.ReadFile(name)
		if err != nil {
			return nil
		}
		if strings.Contains(string(contents), text) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found, true
}

// staleGroundReport is what the parent and the person read when the contract
// did not hold, and it is written to be ACTED ON rather than filed.
//
// It names every expectation that failed — not the first, because a brief
// written against a world one commit behind fails several and a parent shown one
// at a time fixes them one at a time — and then says the three things anybody
// can do about it in the words they would use themselves. There is no machinery
// vocabulary in it: nothing here was refuted or invalidated, a world simply was
// not what somebody said it would be.
func staleGroundReport(world string, unmet []string) string {
	var out strings.Builder
	out.WriteString("its world is not what its brief describes, so nothing was spent on it.\n\n")
	out.WriteString(strings.Join(unmet, "\n"))
	if strings.TrimSpace(world) != "" {
		out.WriteString("\n\nit was given " + world + ".")
	}
	out.WriteString("\n\nEither bring that work into the folder this was cut from and start it again, hand the part out again with a brief that matches what is really there, or say which of the two is right.")
	return out.String()
}

// ── what the worker is told ─────────────────────────────────────────────────

// briefExpectsHeading and briefExpectsRule are the manifest's section in the
// worker's opening document (task_brief.go decides the order of all of them).
//
// THE WORKER IS SHOWN WHAT WAS CHECKED FOR IT, and that is worth a section
// rather than being kept in the machinery: a worker that knows its brief was
// held up against its folder and held knows something real about how far it can
// trust the sentences above — and one that has just been handed a world it was
// told nothing about is exactly the worker that starts by re-reading the
// folder.
const (
	briefExpectsHeading = "WHAT THIS BRIEF ASSUMES, AND WAS CHECKED BEFORE YOU STARTED"
	briefExpectsRule    = "Each line held when your folder was made. Anything else you find that contradicts the brief is news: say so rather than working around it."
)

// expectsSection is the manifest as the worker reads it, and nothing at all when
// there is none — the emptiness law, applied to a document.
func expectsSection(expects []Expectation) string {
	if len(expects) == 0 {
		return ""
	}
	lines := make([]string, 0, len(expects))
	for _, one := range expects {
		lines = append(lines, "· "+one.says())
	}
	return strings.Join(lines, "\n")
}

// ── the one manifest the harness itself may write ───────────────────────────

// continuationExpects is the manifest a CONTINUATION carries: every place this
// turn wrote, asserted to still be there when the worker's folder is made.
//
// ── AND THIS IS NOT THE MINING THIS FILE REFUSES ──
//
// The header above says NOTHING IN THIS FILE WRITES AN EXPECTATION, and the
// reason it gives is exact: a harness that mined paths out of a brief's PROSE
// would be guessing at which sentences the brief depended on, and landing
// somebody's work on the guess. That refusal stands and this does not touch it.
// What is read here is not prose. It is the turn's own tool calls — the record
// of what it actually put on the disk, which the harness compiled for the
// reader's account already ([checkpointLedger]) — and "the file this turn wrote
// is still there" is not an inference about anybody's meaning. It is the one
// assumption a continuation certainly makes, because the work it is carrying on
// from IS those files.
//
// ── WHY WRITES AND NOT READS ──
//
// A turn opens many places and changes few, which is the same asymmetry the
// header gives for not mining prose. A manifest listing everything the turn
// looked at would fail a worker over a file that was only ever glanced at, and
// the value of a manifest is that every line of it is worth the landing it
// causes.
//
// ── AND ONLY ON THE CONTINUATION ROAD ──
//
// Groomed work is written for a worker by somebody who knows which of their own
// sentences are load-bearing, and that somebody is the divider — the header's
// own answer. A continuation has no such author: the turn did not know it was
// going to be handed over, so nobody wrote a manifest and the harness is the
// only participant that holds the facts. That is the whole of the difference,
// and it is why this takes the road as an argument rather than deciding.
//
// AN EMPTY ANSWER IS THE ORDINARY ONE. A turn that changed nothing assumes
// nothing about the folder, preflights nothing and costs nothing — which is the
// honest default this file already states.
func (a *Agent) continuationExpects(why taskModelReason) []Expectation {
	if why != taskModelContinuation {
		return nil
	}
	_, written, _, _, _ := checkpointLedger(a.snapshot())
	expects := make([]Expectation, 0, len(written))
	for _, line := range written {
		// The account writes a path and then what went into it; the manifest wants
		// only the place, because what a file HOLDS after an edit is the worker's
		// to read and not the harness's to assert.
		path := line
		if at := strings.Index(path, checkpointWroteArrow); at >= 0 {
			path = path[:at]
		}
		if path = strings.TrimSpace(path); path == "" {
			continue
		}
		expects = append(expects, Expectation{Path: path, Fact: continuationWroteFact})
		if len(expects) >= expectsRemembered {
			break
		}
	}
	if len(expects) == 0 {
		return nil
	}
	return expects
}

// continuationWroteFact is what the manifest says about each of those places in
// the words of whoever wrote it — which on this road is the harness, so it says
// the plain thing it actually knows and claims nothing about the contents.
const continuationWroteFact = "the conversation this work continues wrote this before it was handed over"
