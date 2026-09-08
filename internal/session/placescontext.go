package session

// WHAT AN ATTACHED FOLDER PUTS IN FRONT OF THE MODEL.
//
// places.go answers "which folders is this conversation about" and writes the
// answer down. This file is the half that was missing: THE MODEL BEING TOLD.
//
// The set fed the ground ladder (taskstands.go) and the standing trees
// (standingtree.go), and nothing else — so a person who picked a folder saw a
// dim line on their screen saying the pick had landed, and the very next request
// went out with no mention of it anywhere. The model then did the only thing it
// could: it went looking for the folder, usually by walking the home directory
// for a name that sounded right, and sometimes it found the wrong one. A
// DISPLAYED LINE IS NOT A MESSAGE, and this file is the difference.
//
// ── WHAT IT IS AND IS NOT ───────────────────────────────────────────────────
//
// AN ATTACHMENT IS A REFERENCE AND NOT A TREE. Nothing here reads a folder's
// contents, counts its files or walks it: the block names the absolute path, says
// the two cheap facts already on the record, and tells the model how to look —
// `ls` that exact path, `grep` for the file, `read` to open it. A chooser that
// pulled a repository into the prompt would spend somebody's whole context on a
// folder they may only have wanted one file out of.
//
// AN ATTACHMENT DOES NOT MOVE THE WORKING DIRECTORY. `Project`'s working
// directory is [Config.Workspace], it is captured at launch, and referring to a
// place has never moved it (places.go states that law and this block repeats it
// to the model, because a model told about a second folder with no ranking
// between them will guess at one).
//
// THE INSTRUCTIONS OF SEVERAL FOLDERS ARE NEVER BLENDED. An attached folder's
// own AGENTS.md/CLAUDE.md is quoted under a heading that names ITS path and says
// in a sentence that it holds under that path and nowhere else. Two attached
// repositories with contradictory house rules is an ordinary thing to have open,
// and a prompt that ran their rules together would hand the model one composite
// project that does not exist.
//
// ── WHAT IT COSTS ───────────────────────────────────────────────────────────
//
// IT RIDES message[0] AND IS REBUILT ONLY WHEN THE SET MOVES. A changed byte in
// message[0] re-prices the whole transcript at the uncached rate on the next
// request (memory.go's refreshSystemLocked states the law), so this is composed
// once per DELIBERATE ACT — a person attaching a folder, a person removing one,
// a conversation being reopened — and never per turn. That is the same trade the
// clock refresh already makes: a model reasoning about a folder nobody attached
// is worse than one re-priced conversation.
//
// A conversation with nothing attached renders NOTHING AT ALL, which is the
// emptiness law and also the ordinary state of nearly every conversation.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// attachedFileLimit is how much of ONE attached folder's instruction file rides
// in the prompt, and it is deliberately half [agentsFileLimit].
//
// The workspace's own AGENTS.md is the house rules of the place the work
// happens; an attached folder's is context about somewhere the conversation is
// merely reading. Several of them can be attached at once, so the per-file bound
// is tighter and there is a budget across all of them below. What is cut is
// named, with the path to read the rest from.
const attachedFileLimit = 4 << 10

// attachedFilesBudget is how much of the prompt EVERY attached folder's
// instructions may take between them. A person with six repositories attached
// has said something about the shape of their work and not about how much of
// their context window they would like spent before they have typed anything.
//
// Folders are read newest-attached first, which is the order the set is held in
// and the order a person's own attention is in, and the folders past the budget
// are named with their file rather than quoted.
const attachedFilesBudget = 12 << 10

// The two headings and the label the block is found by. They are constants
// because the tests read them and because a heading is what a model searches its
// own instructions for.
const (
	attachedHeading = "# Attached folders"
	attachedRules   = "## Instructions for "
)

// attachedPlaces is the folders the PERSON attached, newest first.
//
// IT IS THE SAID SET AND ONLY THE SAID SET. A [PlaceKept] row is a ground the
// ladder worked out and cached (places.go), which is a fact about where work
// went and not a folder anybody handed over — telling the model that a directory
// it resolved for itself last Tuesday was "attached by the person" would put a
// claim about somebody's intent into their instructions.
func attachedPlaces(places []PlaceRef) []PlaceRef {
	var out []PlaceRef
	for _, place := range places {
		if place.Arrival == PlaceSaid && strings.TrimSpace(place.Path) != "" {
			out = append(out, place)
		}
	}
	return out
}

// attachedBlock is the whole block, and "" for a conversation with nothing
// attached.
//
// The workspace is passed so the block can say what it is NOT: the one sentence
// that keeps an attached folder from being read as a move.
func attachedBlock(places []PlaceRef, workspace string) string {
	attached := attachedPlaces(places)
	if len(attached) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\n\n" + attachedHeading + "\n\n")
	out.WriteString("The person attached these folders to THIS conversation themselves. Each path below is exact and absolute, chosen by them — so never search for one of them, never ask which folder was meant, and never work from a path that merely looks like one of these.\n\n")
	for _, place := range attached {
		out.WriteString("- " + place.Path)
		if facts := attachedFacts(place); facts != "" {
			out.WriteString(" — " + facts)
		}
		out.WriteString("\n")
	}
	// AND WHAT THEY ARE NOT, in the same breath as what they are. A model handed
	// a second directory and no ranking will write into whichever one it read
	// last; the working directory above is the one the guard is cut from and the
	// one every relative path is resolved against, and that has not moved.
	fmt.Fprintf(&out, "\nThey are REFERENCES AND NOT THE WORKING DIRECTORY: %s is still where work happens, still what a relative path means, and still what may be written to. Attaching a folder moved none of that. Read inside an attached folder freely, by its exact path above. Before you write anything under one, say which folder you mean and why.\n", workspace)
	// AND HOW TO LOOK, because the alternative to saying it is a model that
	// assumes the whole tree is somewhere in its context and answers from a
	// listing it never read.
	out.WriteString("\nNothing here lists what is inside them. `ls` on one of the exact paths above is the overview, `grep` finds the file, `read` opens it — one folder at a time, and never a walk of all of them at once.\n")
	out.WriteString(attachedInstructions(attached))
	return out.String()
}

// attachedFacts is the short tail of one row: what is already on the record, and
// nothing that costs a walk.
//
// THE EMPTINESS LAW RUNS THROUGH IT. A plain folder that is there says nothing
// at all rather than "not a repository", which is a fact about nothing.
func attachedFacts(place PlaceRef) string {
	var facts []string
	if place.Repository {
		facts = append(facts, "a repository")
	}
	// AND WHAT THEY POINTED AT, WHERE THAT IS NOT WHAT THEY GAINED. The snap to
	// the repository root is real (places.go), and a model told only the root
	// would answer a question about "the folder I attached" about somewhere wider
	// than the person meant — while a model told both can start where they pointed
	// and still reach the rest.
	if chose := strings.TrimSpace(place.Chose); chose != "" && chose != place.Path {
		facts = append(facts, "they pointed at "+chose+" inside it, so start there")
	}
	// AND A FOLDER THAT IS NO LONGER THERE SAYS SO. The set is a history and is
	// not rewritten when a disk is unplugged (places.go), so the honest thing to
	// put in front of the model is the record AND the reading — a model that
	// spends three calls discovering a path does not exist has spent them on
	// something this stat answered.
	if info, err := os.Stat(place.Path); err != nil || !info.IsDir() {
		facts = append(facts, "NOT on this disk right now")
	}
	return strings.Join(facts, ", ")
}

// attachedInstructions quotes each attached folder's own house rules under a
// heading that names the folder they hold for.
//
// EVERY BLOCK IS SCOPED OUT LOUD AND NONE OF THEM IS BLENDED, for this file's
// stated reason. The budget is spent newest-attached first, and a folder past it
// is NAMED with its file rather than dropped in silence: "there are rules here
// and you may read them" is a true sentence and a useful one, and pretending a
// folder has none would be the prompt lying by omission.
func attachedInstructions(attached []PlaceRef) string {
	var out strings.Builder
	spent := 0
	for _, place := range attached {
		for _, name := range []string{agentsFileName, claudeFileName} {
			file := filepath.Join(place.Path, name)
			if _, err := os.Stat(file); err != nil {
				continue
			}
			if spent >= attachedFilesBudget {
				fmt.Fprintf(&out, "\n%s%s\n\nThat folder has its own %s and it is NOT quoted here — the instructions of the folders above it filled the room this prompt gives them. Read %s if the work goes into that folder.\n",
					attachedRules, place.Path, name, file)
				continue
			}
			rules, truncated := readInstructionFileWithin(place.Path, name, attachedFileLimit)
			if rules == "" {
				continue
			}
			spent += len(rules)
			fmt.Fprintf(&out, "\n%s%s\n\nFrom %s at the root of that attached folder. THEY HOLD FOR WORK UNDER %s AND NOWHERE ELSE — not for the working directory, and not for another attached folder, whose own rules may say the opposite. They rank below the project's own instructions above and below what the person says now.\n\n",
				attachedRules, place.Path, name, place.Path)
			fence := fenceFor(rules)
			out.WriteString(fence + "markdown\n")
			out.WriteString(rules)
			if !strings.HasSuffix(rules, "\n") {
				out.WriteString("\n")
			}
			out.WriteString(fence + "\n")
			if truncated {
				fmt.Fprintf(&out, "\n(%s is longer than %dKiB; the rest is on disk — read %s if you need it.)\n",
					name, attachedFileLimit>>10, file)
			}
		}
	}
	return out.String()
}

// ── keeping it true ─────────────────────────────────────────────────────────

// keepAttached recomposes the block and puts it in front of the model.
//
// THE DISK IS READ WITHOUT THE LOCK AND THE FIELD IS WRITTEN WITH IT, which is
// [Agent.refer]'s own shape and for its reason: a turn is waiting on that lock,
// and reading two instruction files off a slow disk is not work to make it wait
// for. The set is taken as a copy first, so what is composed is one photograph of
// it rather than a slice moving under the reader.
func (a *Agent) keepAttached() {
	text := attachedBlock(a.referredPlaces(), strings.TrimSpace(a.config.Workspace))
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.placesText == text {
		// NOTHING MOVED, SO message[0] IS NOT TOUCHED. A ground resolved onto a
		// folder the ladder already knew changes the set without changing what the
		// model is told, and rewriting the same bytes would still cost the compare
		// — but a caller that wrote them anyway would make it far too easy for a
		// later road to re-price a conversation for nothing.
		return
	}
	a.placesText = text
	a.refreshSystemLocked()
}
