package session

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ONE ROW OF THE RECORD, READ DEEPER THAN THE WALK READS IT.
//
// [ReadWorld] carries what a project's index FILE says about a piece of work —
// its title, the state it came home in, what it cost, how many files it wrote,
// the branch or the worktree it left behind ([TaskIndexEntry]) — and that is
// everything a list of work draws. The CARD behind one of those rows draws one
// thing more: THE LAST THING THE NODE ITSELF SAID, which lives in the node's own
// journal and is not in the index at all.
//
// IT IS A SECOND READING AND NOT A FIELD ON THE WALK, and what decides that is
// what it costs. The walk is taken on a beat and answers five screens; a report
// is a forward scan of a whole session journal ([PeekReport]), and it is wanted
// for exactly ONE row — the one somebody just pressed. A walk that read every
// journal on the machine would be paying for four hundred reports to draw a list
// that shows none of them.

// TaskRecord is that reading: what the card knows about one piece of work beyond
// the row it was opened from.
//
// IT CROSSES THE WIRE AS ITSELF (internal/remote's Decision 1), because over a
// connection the journal is on the machine that ran the work and this surface
// has no way to open it — which is the whole reason the type exists rather than
// the card simply calling [PeekReport] on a path.
type TaskRecord struct {
	// Report is the last thing the node said, cut the way [PeekReport] cuts it,
	// and empty where the journal holds no report of its own.
	Report string `json:"report,omitempty"`
	// Kept says the journal the row names is STILL ON THE DISK OF THE MACHINE
	// THAT RAN THE WORK.
	//
	// IT IS A FACT AND NOT AN INFERENCE FROM AN EMPTY REPORT. A session folder
	// somebody deleted and a journal that never held a report are two different
	// sentences on a card, and only the machine holding the file can tell them
	// apart — a surface that guessed from an empty string would say "its
	// transcript is not on this disk any more" about a file that is sitting
	// perfectly well on the other one.
	Kept bool `json:"kept,omitempty"`
}

// TaskRecordPath is the LOCAL FILE a row's URI names, or "" for a URI that names
// anything else.
//
// The record carries two URIs and one of them is sometimes not a file at all: a
// node whose worktree was pruned keeps its branch, spelled `git:task/…`
// ([taskArtifactURI]). A file URI naming a host names another machine's disk,
// which is the same refusal the surface's path linker makes about one. Everything
// this returns is a path the machine that WROTE the URI can be asked to open.
func TaskRecordPath(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	parsed, err := url.Parse(uri)
	if err != nil || !strings.EqualFold(parsed.Scheme, "file") || parsed.Path == "" {
		return ""
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		return ""
	}
	return parsed.Path
}

// ReadTaskRecord reads the journal one row names, for its last word.
//
// IT MAKES NO DECISION ABOUT WHOSE DISK THIS IS. A surface reading its own
// machine's record calls it directly; an engine answering for its machine over a
// connection applies its own boundary first ([ReadTaskRecordUnder]), which is
// the same division internal/remote keeps everywhere else — the reading is one
// function and the permission is the engine's to make.
func ReadTaskRecord(uri string) TaskRecord {
	path := TaskRecordPath(uri)
	if path == "" {
		return TaskRecord{}
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return TaskRecord{}
	}
	report, _ := PeekReport(path)
	return TaskRecord{Report: report, Kept: true}
}

// ReadTaskRecordUnder is that reading with the one boundary an ENGINE has to
// apply: the journal must be a file under the places root this machine answers
// for.
//
// THE PATH CAME FROM HERE IN THE FIRST PLACE. A remote surface only ever asks
// about a URI this machine wrote into its own index and handed over on the world
// walk — but a door that trusted that would be a permission decision taken on
// the strength of what the other end says, so the root is checked here, on the
// machine that owns it. It is internal/remote's two-roots law restated for the
// one directory this door answers about.
func ReadTaskRecordUnder(root, uri string) (TaskRecord, error) {
	path := TaskRecordPath(uri)
	if path == "" {
		return TaskRecord{}, fmt.Errorf("engine: %s does not name a file on this machine", strings.TrimSpace(uri))
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		// A JOURNAL THAT IS GONE IS AN ANSWER AND NOT A REFUSAL. The row still
		// names it, the card still says where it was, and `Kept` false is the
		// sentence the card has for exactly this. A path that never was under the
		// root is a different matter and falls through to the refusal below.
		if !underRoot(root, path) {
			return TaskRecord{}, outsideTheRecord(uri)
		}
		return TaskRecord{}, nil
	}
	if !underRoot(root, real) {
		return TaskRecord{}, outsideTheRecord(uri)
	}
	return ReadTaskRecord("file://" + real), nil
}

func outsideTheRecord(uri string) error {
	return fmt.Errorf("engine: %s is outside this machine's record and nothing outside it crosses this connection", strings.TrimSpace(uri))
}

// underRoot reports whether a path sits under a root.
//
// IT ASKS AGAINST THE ROOT AS WRITTEN AND AGAINST THE ROOT RESOLVED, and either
// answer is a yes. The two are different on any machine whose state root reaches
// it through a symlink — a Mac's `/var` is `/private/var` — and the two sides of
// this comparison are not always resolved the same way: a journal that has been
// DELETED cannot be resolved at all, so it arrives here as written, and measuring
// it against a resolved root would refuse the machine's own file for the crime of
// no longer being there.
func underRoot(root, path string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	if within(root, path) {
		return true
	}
	resolved, err := filepath.EvalSymlinks(root)
	return err == nil && within(resolved, path)
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
