package session

// WHAT THE PLACES A CONVERSATION IS FILED UNDER PUT IN FRONT OF THE MODEL.
//
// The desktop files conversations under places (internal/placegraph: named,
// tinted groupings with many parents), and a place carries plain-prose
// instructions and sources (design: Places 6e "Context merges from every
// place"). This file is the half that makes filing MEAN something to the model:
// it resolves the conversation's places through the one resolver
// ([placegraph.Snapshot.Resolve]) and renders what it gives into message[0].
//
// THESE ARE NOT ATTACHED FOLDERS, and the two blocks are kept apart on purpose.
// placescontext.go tells the model "the person attached these folders to THIS
// conversation themselves", which is a claim about a person's act in this
// conversation. A folder that arrives because the conversation was filed under a
// place that lists it was not attached by anybody here, and saying it was would
// put a false sentence about somebody's intent into their prompt. So a place's
// folders are named in this block, credited to the place, and the attached set
// (meta.json) is never written by a place.
//
// ── WHEN IT MOVES ───────────────────────────────────────────────────────────
//
// AT A TURN'S OPENING AND NEVER INSIDE ONE (6e risks: "Adding a place mid-run
// applies from the next turn"). The check is a stat of the graph file and the
// choices file. A stat that moved is then compared as [placegraph.ReadGeneration]
// (the store's revision). Only a moved generation, or a moved choices file,
// re-reads the graph and re-renders the block, and only when the rendered bytes
// differ does message[0] change (memory.go's [Agent.refreshSystemLocked] states
// why a changed byte there costs the whole transcript). A visit rewrites the
// file without moving the generation, so it does not re-render. Most turns of
// most conversations pay two stats.
//
// THE READ TAKES NO LOCK BUT THIS AGENT'S. The graph is read with
// [placegraph.ReadSnapshot], which never waits on the store's file lock and
// never writes, because this runs with a.mu held and a turn must not wait ten
// seconds on another window's save. A file caught damaged keeps the block the
// conversation already had, and the next turn tries again.
//
// ── WHAT IT SAYS WHEN IT MOVES ──────────────────────────────────────────────
//
// A place starting or stopping to reach the conversation posts one line into it
// ("Now also using Release: brand-voice.md", 6e "Always visible"), journaled as
// a session note so it replays, and seen by the model at the same request as the
// new block — a model whose instructions changed between two of its answers is
// owed a sentence saying so.

import (
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// NoteKindPlaces is a line saying a place started or stopped reaching this
// conversation. It is persisted bytes like the other NoteKind constants.
const NoteKindPlaces = "places"

// placeGraphHeading is the block's heading. The block states its own ranking,
// disagreement and reference rules when it is present, so the fixed prefix
// does not repeat them. Tests find the block by this heading.
const placeGraphHeading = "# Places this conversation belongs to"

// PlaceGraphDoor is how a conversation finds the place graph. Nil (the default)
// is a conversation with no places: nothing is read and nothing is rendered.
//
// THE CONVERSATION'S OWN ID IS THE MEMBERSHIP KEY — the journal header's id,
// which is the session folder's name and [SessionRow.ID] — so there is no field
// to name another chat with. A conversation that lives only in memory has no
// id anyone could have filed, and reads nothing.
type PlaceGraphDoor struct {
	// Path is the graph file, the same one the desktop bridge's
	// placegraph.Store is opened on.
	Path string
	// ChoicesPath is the remembered-picks file (placegraph.OpenChoices), or "".
	ChoicesPath string
	// Sources is the source policy. The zero value is the default policy for
	// this user's home.
	Sources placegraph.SourcePolicy
}

// placeGraphStamp is what a turn compares to decide whether to re-read.
// generation is the graph revision last resolved. It is filled in only after a
// stat miss, so the fast path (two stats, nothing else) does not parse the file.
type placeGraphStamp struct {
	graph, choices os.FileInfo
	generation     uint64
}

func statStamp(door *PlaceGraphDoor) placeGraphStamp {
	var s placeGraphStamp
	s.graph, _ = os.Stat(door.Path)
	if door.ChoicesPath != "" {
		s.choices, _ = os.Stat(door.ChoicesPath)
	}
	return s
}

func sameInfo(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func (s placeGraphStamp) same(o placeGraphStamp) bool {
	return sameInfo(s.graph, o.graph) && sameInfo(s.choices, o.choices)
}

// refreshPlaceGraphLocked is the turn-opening check. Called with a.mu held, from
// [Agent.startTurnLocked], before message[0] is rebuilt.
func (a *Agent) refreshPlaceGraphLocked() {
	door := a.config.PlaceGraph
	if door == nil || door.Path == "" || a.file == nil || a.id == "" {
		return
	}
	stamp := statStamp(door)
	if a.placeGraphRead && stamp.same(a.placeGraphStamp) {
		return
	}
	// The generation is the revision. A file that moved without it (a visit)
	// cannot have changed what this conversation is told. A damaged file is an
	// error here and is left for the store; the block already composed stays.
	gen, err := placegraph.ReadGeneration(door.Path)
	if err != nil {
		return
	}
	stamp.generation = gen
	if a.placeGraphRead && gen == a.placeGraphStamp.generation && sameInfo(stamp.choices, a.placeGraphStamp.choices) {
		a.placeGraphStamp = stamp
		return
	}
	bundle, err := resolvePlaceGraph(door, a.id)
	if err != nil {
		// KEEP WHAT THE CONVERSATION HAD. The stamp is not taken, so the next
		// turn reads again; a half-saved or damaged file is the store's to set
		// aside (placegraph's Store), not this reader's to act on.
		return
	}
	text := placeGraphBlock(bundle, strings.TrimSpace(a.config.Workspace))
	changes := placegraph.Changes(a.placeGraphBundle, bundle)
	var receipts []string
	if len(changes) > 0 && a.placeGraphBundle != nil {
		receipts = placegraph.ContextUndoReceipt(door.Path, a.placeGraphBundle.Revision, bundle.Revision)
	}
	for _, change := range changes {
		a.queuePlaceNoteWithUndoLocked(change.Text, receipts)
	}
	a.placeGraphStamp, a.placeGraphRead, a.placeGraphBundle = stamp, true, bundle
	a.placeGraphText = text
	a.refreshRememberPlaceToolLocked()
}

// resolvePlaceGraph reads both files and resolves chatID through the one
// resolver and its one budget.
func resolvePlaceGraph(door *PlaceGraphDoor, chatID string) (*placegraph.Bundle, error) {
	snap, err := placegraph.ReadSnapshot(door.Path)
	if err != nil {
		return nil, err
	}
	var choices []placegraph.Choice
	if door.ChoicesPath != "" {
		if choices, err = placegraph.ReadChoices(door.ChoicesPath, chatID); err != nil {
			return nil, err
		}
	}
	return snap.Resolve(chatID, placegraph.ResolveOptions{Choices: choices, Sources: door.Sources}), nil
}

// PlaceGraphUsing is the Using popover's data for one conversation: the same
// resolver, budget and source policy the engine renders message[0] from, so the
// popover lists exactly what the model was given. The bridge reads the snapshot
// through its own Store and the picks through its own ChoiceBook.
func PlaceGraphUsing(snap *placegraph.Snapshot, chatID string, choices []placegraph.Choice, sources placegraph.SourcePolicy) *placegraph.Bundle {
	return snap.Resolve(chatID, placegraph.ResolveOptions{Choices: choices, Sources: sources})
}

// queuePlaceNoteLocked puts one "Now also using …" line on the steering queue,
// which the turn about to open drains in front of its first request
// ([Agent.drainSteering]) and journals, so the line is on the screen and in the
// replay where the change took effect. It is [Agent.enqueueNote] for a caller
// already holding a.mu, and it never wakes a turn: one is opening.
func (a *Agent) queuePlaceNoteLocked(text string) {
	a.queuePlaceNoteWithUndoLocked(text, nil)
}

func (a *Agent) queuePlaceNoteWithUndoLocked(text string, receipts []string) {
	text = strings.TrimSpace(text)
	if text == "" || a.closed {
		return
	}
	note := userText(text)
	note.message = textMessage("user", text)
	note.authored = true
	note.facts = noteFacts{Kind: NoteKindPlaces, UndoReceipts: append([]string(nil), receipts...)}
	a.steering = append(a.steering, note)
}

// placeGraphBlock renders a Bundle, and "" for a conversation in no place (the
// emptiness law, and the ordinary state of most conversations).
func placeGraphBlock(b *placegraph.Bundle, workspace string) string {
	if b.Empty() {
		return ""
	}
	names := map[string]string{}
	for _, p := range b.Places {
		names[p.ID] = p.Name
	}
	var out strings.Builder
	out.WriteString("\n\n" + placeGraphHeading + "\n\n")
	out.WriteString("The person filed this conversation under these places in codeaf. What the places say below holds for THIS conversation, credited to the place that says it. A place reached through a parent is inherited.\n\n")
	for _, p := range b.Places {
		out.WriteString("- " + p.Name)
		if p.Inherited {
			out.WriteString(" — inherited through " + joinNames(p.Through, names))
		} else {
			out.WriteString(" — filed here")
		}
		out.WriteString("\n")
	}
	writePlaceInstructions(&out, b, names)
	writePlaceSources(&out, b, names, workspace)
	return out.String()
}

func joinNames(ids []string, names map[string]string) string {
	var parts []string
	for _, id := range ids {
		if n := names[id]; n != "" {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, ", ")
}

// writePlaceInstructions quotes each place's words under its own heading.
//
// THEY ARE NEVER BLENDED, the attached-folder law: two places with rules that
// contradict each other is an ordinary thing to be filed under, and one
// composite set of rules would be a place that does not exist. Contradiction is
// not judged here — that takes reading for meaning — so the design's rule for it
// is stated to the model, which is reading them anyway.
func writePlaceInstructions(out *strings.Builder, b *placegraph.Bundle, names map[string]string) {
	shown := 0
	for _, in := range b.Instructions {
		if in.Text == "" {
			continue
		}
		shown++
		credit := names[in.PlaceID]
		if len(in.AlsoFrom) > 0 {
			credit += " (also said by " + joinNames(in.AlsoFrom, names) + ")"
		}
		fmt.Fprintf(out, "\n## Instructions from %s\n\n", credit)
		fence := fenceFor(in.Text)
		out.WriteString(fence + "markdown\n" + in.Text + "\n" + fence + "\n")
		if in.Trimmed {
			fmt.Fprintf(out, "\n(The rest of these instructions is past the %d bytes of place instructions one conversation is given, and is not shown. The person can read it on the place's Home.)\n", placegraph.ContextInstructionBudget)
		}
	}
	for _, in := range b.Instructions {
		if in.Text == "" {
			fmt.Fprintf(out, "\n%s also has instructions, and none of them fit in the %d bytes of place instructions one conversation is given. The person can read them on the place's Home.\n", names[in.PlaceID], placegraph.ContextInstructionBudget)
		}
	}
	if shown > 1 {
		out.WriteString("\nWhen instructions from two places contradict each other, the place both are filed under decides: follow what their nearest shared place above says, counting a place as above itself. When no shared place speaks to it, ask the person once which holds, and keep to the answer for the rest of this conversation. All place instructions rank below the project's own instructions above and below what the person says now.\n")
	} else if shown == 1 {
		out.WriteString("\nPlace instructions rank below the project's own instructions above and below what the person says now.\n")
	}
}

// writePlaceSources names each source and how to look at it. A source is a
// REFERENCE: nothing here reads, lists or fetches one.
func writePlaceSources(out *strings.Builder, b *placegraph.Bundle, names map[string]string, workspace string) {
	if len(b.Sources) == 0 && len(b.Trimmed) == 0 {
		return
	}
	out.WriteString("\n## Sources these places give\n\n")
	paths := false
	for _, s := range b.Sources {
		var credit []string
		for _, o := range s.From {
			credit = append(credit, names[o.PlaceID])
		}
		facts := []string{sourceKindWords(s)}
		if s.RepoRoot != "" && s.RepoRoot != s.Ref {
			facts = append(facts, "inside the repository "+s.RepoRoot)
		}
		if s.Status == placegraph.SourceMissing {
			facts = append(facts, "NOT on this disk right now")
		}
		label := s.Ref
		if s.Kind == placegraph.SourceChat && s.Label != "" {
			label = fmt.Sprintf("%s (%q)", s.Ref, s.Label)
		}
		fmt.Fprintf(out, "- %s — %s; from %s\n", label, strings.Join(facts, ", "), strings.Join(credit, ", "))
		switch s.Kind {
		case placegraph.SourceFile, placegraph.SourceFolder, placegraph.SourceRepo:
			paths = true
		}
	}
	if paths {
		fmt.Fprintf(out, "\nPaths here are REFERENCES AND NOT THE WORKING DIRECTORY: %s is still where work happens and what a relative path means. Nothing here quotes or lists them — `read` a file or `ls` a folder by its exact path when the work needs it, one at a time.\n", workspace)
	}
	if n := len(b.Trimmed); n > 0 {
		fmt.Fprintf(out, "\n%d more sources from these places are left out: one conversation is given at most %d. The person can see which in the Using list.\n", n, placegraph.ContextSourceBudget)
	}
}

func sourceKindWords(s placegraph.UsedSource) string {
	switch s.Kind {
	case placegraph.SourceFile:
		return "a file"
	case placegraph.SourceRepo:
		return "a repository"
	case placegraph.SourceFolder:
		return "a folder"
	case placegraph.SourceURL:
		return "a web page, named here and not fetched"
	case placegraph.SourceChat:
		return "another conversation, whose words are not included here"
	}
	return string(s.Kind)
}
