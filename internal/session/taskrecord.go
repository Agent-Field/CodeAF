package session

import (
	"bytes"
	"fmt"
	"io"
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
	// Journal is the END of the node's own transcript, whole lines, and nil
	// whenever the caller did not ask for any.
	//
	// IT IS BYTES AND NOT A PARSED PAGE. The journal's framing is one JSON object
	// per line and the surface already has the reader for it (internal/tui3's
	// readJournalLines), so a shape invented here would be a second parser that
	// has to agree with that one for ever — and Decision 1's whole bargain is
	// that a payload is the thing itself rather than a translation of it.
	//
	// IT IS A TAIL AND NEVER THE FILE. A node's transcript is megabytes and the
	// page that draws it keeps the last screenful of blocks anyway
	// ([tui3.readRoomJournalTail]), so sending the whole of one would be paying a
	// connection for lines nothing will draw. The cut is made at a LINE BOUNDARY
	// — the first newline after it is dropped along with the partial line before
	// it — because a torn first line is a line the reader throws away and a
	// caller cannot tell that from a line that was never written.
	Journal []byte `json:"journal,omitempty"`
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
//
// TAIL IS HOW MANY BYTES OF THE JOURNAL'S END TO CARRY BACK, and zero — the
// card's own answer — carries none. A room wants the transcript and a card wants
// one paragraph of it, and the difference between the two is a megabyte on a
// wire, so the caller says which it is asking for rather than every caller
// paying for the larger.
func ReadTaskRecord(uri string, tail int) TaskRecord {
	path := TaskRecordPath(uri)
	if path == "" {
		return TaskRecord{}
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return TaskRecord{}
	}
	report, _ := PeekReport(path)
	return TaskRecord{Report: report, Kept: true, Journal: readTail(path, info.Size(), tail)}
}

// TaskJournalTail is what a ROOM asks for: the last of a node's transcript, in
// bytes.
//
// HALF A MEGABYTE IS THE SCREENFUL WITH ROOM TO SPARE. The page keeps the last
// [tui3.roomTail] blocks and throws the rest away, and a block is a message —
// so what is actually drawn is tens of kilobytes even on a node that ran for an
// hour. The margin is for the one line a journal is allowed to be enormous on: a
// tool result, capped at 4k by the display but not by the file.
const TaskJournalTail = 512 << 10

// readTail is the last n bytes of a file, cut back to a whole line, or nil for a
// caller that asked for none.
func readTail(path string, size int64, n int) []byte {
	if n <= 0 || size <= 0 {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	from := size - int64(n)
	if from < 0 {
		from = 0
	}
	if from > 0 {
		if _, err := file.Seek(from, io.SeekStart); err != nil {
			return nil
		}
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil
	}
	if from > 0 {
		// THE FIRST LINE IS TORN AND IS DROPPED WITH THE CUT. A reader cannot tell
		// half a JSON object from one this build does not understand, and both are
		// skipped — but only one of them is a line somebody wrote.
		if at := bytes.IndexByte(data, '\n'); at >= 0 {
			data = data[at+1:]
		} else {
			data = nil
		}
	}
	return data
}

// ReadTaskRecordUnder is that reading with the one boundary an ENGINE has to
// apply: the journal must be a file under one of the roots this machine answers
// for ([RecordRoots]).
//
// THE PATH CAME FROM HERE IN THE FIRST PLACE. A remote surface only ever asks
// about a URI this machine wrote into its own index and handed over on the world
// walk — but a door that trusted that would be a permission decision taken on
// the strength of what the other end says, so the roots are checked here, on the
// machine that owns them. It is internal/remote's two-roots law restated for the
// directories this door answers about.
//
// IT TAKES A LIST BECAUSE THE RECORD IS IN MORE THAN ONE PLACE, and taking one
// root was a bug and not a simplification: an adaptive run's node journals live
// under the runs root, so a door holding only the places root refused every one
// of them — the file was on the disk, this machine had written its path into its
// own index, and the surface was told it could not be read.
func ReadTaskRecordUnder(roots []string, uri string, tail int) (TaskRecord, error) {
	path := TaskRecordPath(uri)
	if path == "" {
		return TaskRecord{}, fmt.Errorf("engine: %s does not name a file on this machine", strings.TrimSpace(uri))
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		// A JOURNAL THAT IS GONE IS AN ANSWER AND NOT A REFUSAL. The row still
		// names it, the card still says where it was, and `Kept` false is the
		// sentence the card has for exactly this. A path that never was under any
		// root is a different matter and falls through to the refusal below.
		if !underAnyRoot(roots, path) {
			return TaskRecord{}, outsideTheRecord(uri)
		}
		return TaskRecord{}, nil
	}
	if !underAnyRoot(roots, real) {
		return TaskRecord{}, outsideTheRecord(uri)
	}
	return ReadTaskRecord("file://"+real, tail), nil
}

// underAnyRoot is [underRoot] asked of every root this machine answers for. An
// empty list admits nothing, which is the same refusal an empty root has always
// been: a door with no boundary set is a door that is not open.
func underAnyRoot(roots []string, path string) bool {
	for _, root := range roots {
		if underRoot(root, path) {
			return true
		}
	}
	return false
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
