package session

// A LANDING DOES NOT WRITE OVER A FILE THAT CHANGED UNDER IT.
//
// ── WHAT THIS REPAIRS ──
//
// A folder family works in a private copy of the person's folder for as long as
// the work takes, and lays its ledger back over that folder at the end
// (task_run.go's [mirrorGround] and [taskTree.landMirror], task_audit.go's
// [layWork]). The laying is `RemoveAll` and copy, and nothing anywhere recorded
// what those files looked like when the copy was made — so an edit the person
// made in their own folder while the work ran was overwritten, silently, with
// nothing in the report, the card or the job log to say it had happened. Since
// the late roads became real (#237) the window is not one run but one morning: a
// family that settled needing a look is laid over the folder when somebody types
// `accept` hours later, from a ledger recorded before lunch.
//
// This is the folder ground's half of what git already does for a repository
// ground, where a person's own edit to a file the branch touches is exactly what
// makes the merge refuse. So the answer is the same answer, in the folder's own
// words: the changed files are NAMED and NOTHING IS LAID.
//
// ── WHY THE BASELINE IS ITS OWN RECORD ──
//
// The family tree's baseline commit is byte-exact and free where it exists
// (task_tree_mirror.go's [openFamilyRepository] commits the mirror as it stands
// the moment it is carved), and being asked to use it is the right question to
// ask: one source of truth beats two files. It is not used, for one reason. That
// commit exists only where `git init` succeeded, and the family whose init
// failed is the DEGRADED one — the read-only disk, the machine with no git,
// the folder somebody moved — whose parts already share a directory and which is
// the last family that should also leave without a manners check. A road that
// covers four families out of five is a road somebody has to remember the shape
// of; this one is asked at every landing and answers for all of them. It is also
// the cheaper reading at the moment it matters: comparing one ledger path costs
// one digest of one file, where the commit would cost a `git` process per
// landing in a directory whose history is the family's medium rather than the
// person's.
//
// So the folder as it stood is written down beside the copy, in the tree's own
// private corner, exactly as the leavings snapshot already is (task_run.go's
// [rememberLeftBehind] pair): it survives a dead process, an accept the next
// morning and a re-audit, and not a byte of it reaches the checkpoint, which is
// rewritten as the graph moves and has no business carrying twenty thousand
// digests.
//
// ── AND ABSENCE IS ORDINARY ──
//
// A folder with no record is a folder from an older build, or one this could not
// read in full, and it lands exactly as it landed before this file existed. A
// manners check that turned a missing file into a refusal would make every
// family carried across an upgrade need somebody's look.
//
// AND THE WINDOW IS NARROWED, NOT CLOSED. A plain folder has nothing to lock, so
// the milliseconds between the last comparison and the copy that follows it
// belong to whoever writes first — exactly as they do for git, which reads its
// index and then writes the working tree. What this changes is the size of the
// window a person can lose work in: it was the whole of a run, and a whole
// morning on the late roads.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// groundBaselineRecord is the folder's own snapshot, beside [leftBehindRecord]
// in the same private corner and for the same reason: it belongs to the tree, so
// it lives with the tree.
const groundBaselineRecord = "ground-baseline.json"

// groundBaseline is the file's shape. The digests are keyed by the SAME
// slash-spelled path a ledger names ([normalizeScopePath]), so a comparison is a
// map lookup rather than a second reading of what a path means.
type groundBaseline struct {
	Paths map[string]string `json:"paths"`
}

// The two answers a digest can carry that are not a digest.
//
// UNKNOWN IS NEVER EQUAL TO ANYTHING, including another unknown, and that is the
// whole of its meaning: a file this could not read is a file nobody may claim is
// unchanged. It is compared for explicitly rather than by string equality, which
// would quietly make two unreadable files "the same".
const (
	digestAbsent  = ""
	digestUnknown = "?"
)

// rememberGroundBaseline writes down what the person's folder held at the moment
// the family took its copy of it.
//
// IT IS CALLED WHERE THE COPY IS MADE AND NOWHERE ELSE ([prepareTaskTreeOn]), so
// a RESUMED node keeps the baseline its first run recorded — which is the only
// reading that is true. Re-recording it on the resume road would take today's
// folder as the world the work started in, and an edit made while the process
// was down would be exactly the edit that disappeared.
//
// AND IT READS THE COPY, NOT THE FOLDER, which is the one reading with no race
// in it. Copying a folder takes time, and a person who saves a file during those
// seconds would have that save read back as the original if this walked the
// folder afterwards — and then written over at the landing, which is the whole
// defect. The copy holds the bytes the family was actually given, so a save made
// while the copy was being taken lands on the refusing side, where it belongs.
func rememberGroundBaseline(dir string) {
	if dir = strings.TrimSpace(dir); dir == "" {
		return
	}
	paths := map[string]string{}
	walk := &digestWalk{budget: auditRestoreEntries}
	if !gatherDigests(dir, "", paths, walk) {
		// PAST THE CAP THERE IS NO HONEST RECORD TO WRITE. A half-walked folder
		// would say "absent" about files that are sitting right there, and every
		// one of them would land as a refusal naming a file nobody touched. No
		// record is the old behaviour, and the old behaviour is at least a
		// behaviour somebody can predict.
		return
	}
	metadata := filepath.Join(dir, aforgeDroppings)
	if err := os.MkdirAll(metadata, 0o755); err != nil {
		return
	}
	contents, err := json.Marshal(groundBaseline{Paths: paths})
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(metadata, groundBaselineRecord), contents, 0o600)
}

// rememberedGroundBaseline reads it back. NIL IS NO RECORD — an older folder, a
// walk that ran past its cap, a write that failed — and it is what turns the
// whole check off for that family; an empty map is a folder that really was
// empty when the copy was made, and every path in the ledger is measured against
// it.
func rememberedGroundBaseline(dir string) map[string]string {
	contents, err := os.ReadFile(filepath.Join(dir, aforgeDroppings, groundBaselineRecord))
	if err != nil {
		return nil
	}
	var record groundBaseline
	if err := json.Unmarshal(contents, &record); err != nil {
		return nil
	}
	return record.Paths
}

// groundChanged answers which of the paths a family means to lay were changed in
// the person's own folder while it worked. An empty answer is a landing that may
// go ahead.
//
// THE QUESTION IS ASKED OF THE LEDGER AND NOT OF THE FOLDER. What ships is
// `wrote` — the whole of [layWork]'s law — so a person who spent the morning in
// a file no part of the family is going to touch is a person this must not stop.
//
// THREE READINGS SETTLE ONE PATH. What the folder holds now, what the baseline
// says it held, and what the family would put there. Equal to the baseline —
// including absent then and absent now — and the file is the one the family was
// given, so laying it is what was asked for. Equal instead to WHAT WOULD BE
// LAID, and there is nothing to protect: the lay would not change a byte, which
// is what makes a landing that has already happened safe to ask about a second
// time on the accept road.
func groundChanged(dir, ground string, wrote []string) []string {
	base := rememberedGroundBaseline(dir)
	if base == nil {
		return nil
	}
	recorded := &baselineIndex{paths: base}
	walk := &digestWalk{budget: auditRestoreEntries}
	seen := make(map[string]bool, len(wrote))
	var changed []string
	for _, raw := range wrote {
		relative, err := normalizeScopePath(dir, raw)
		if err != nil || seen[relative] {
			// A path outside the working copy is not part of what ships and is not
			// laid either, so it is not this file's business ([layWork] drops it).
			continue
		}
		seen[relative] = true
		now := pathDigest(ground, relative, walk)
		if now != digestUnknown && now == recorded.digest(relative) {
			continue
		}
		if want := pathDigest(dir, relative, walk); now != digestUnknown && now == want {
			continue
		}
		changed = append(changed, relative)
	}
	return changed
}

// groundChangedSentence is the one line a person reads about it, and it is
// [conflictSentence]'s sentence in the folder's words: WHERE THE FAMILY'S OWN
// VERSION IS, and WHICH FILES it stood back from. Both halves are the actionable
// ones — the folder holds their edit, the directory holds the work, and the
// names are what tells them which is which.
//
// It names the first few and counts the rest through [namedFew], exactly as a
// conflict's does, so that a family that would have laid three thousand files
// over a folder somebody reorganised is still one line on a card.
func groundChangedSentence(dir, ground string, changed []string) string {
	return "its work is in " + dir + " and was not laid over " + ground + ": " +
		namedFew(changed, conflictNamesShown) + " changed there while this ran"
}

// digestWalk is what one reading is allowed to spend and what it could not read.
//
// THE SECOND FIELD IS THE ONE THAT MATTERS. A directory this could not open is a
// directory whose contents nobody knows, and an answer that folded it in as
// "empty" would let a landing remove files it had never seen. So the trouble is
// carried out of the walk and turned into [digestUnknown], which is never equal
// to anything.
type digestWalk struct {
	budget int
	unread bool
}

// gatherDigests walks a folder from one path down, writing a digest per file
// under the ROOT-relative name a ledger would use for it. It answers false when
// it ran past what a folder is worth walking, and the caller decides what that
// means.
//
// IT SKIPS WHAT EVERY OTHER WALK IN THIS PROGRAM SKIPS ([mirrorGround],
// [copyOriginal]): a repository's own metadata and the harness's private corner
// are nobody's deliverable, and a family tree opened inside the mirror would
// otherwise put its whole object store into the comparison.
func gatherDigests(root, from string, into map[string]string, walk *digestWalk) bool {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(from)))
	if err != nil {
		// NOT FATAL, AND NOT SILENT EITHER. [mirrorGround] passes over the same
		// directory, so refusing here would refuse work that copied perfectly well;
		// what this owes instead is to say that it did not see it.
		walk.unread = true
		return true
	}
	for _, entry := range entries {
		child := entry.Name()
		if from != "" {
			child = from + "/" + entry.Name()
		}
		if entry.Name() == ".git" || child == aforgeDroppings {
			continue
		}
		if walk.budget--; walk.budget < 0 {
			return false
		}
		if entry.IsDir() {
			if !gatherDigests(root, child, into, walk) {
				return false
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if digest, ok := oneDigest(filepath.Join(root, filepath.FromSlash(child)), info); ok {
			into[child] = digest
		}
	}
	return true
}

// pathDigest is what one ledger path IS, as a single string, whatever it turns
// out to be: a file, a link, a whole directory, or nothing at all.
//
// A DIRECTORY IS FOLDED INTO ONE ANSWER because a ledger path that names one is
// laid as one — [layWork] removes the target and copies the tree over it — so
// the question "did this change under us" has to be asked of everything that
// laying would take away.
func pathDigest(root, relative string, walk *digestWalk) string {
	full := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(full)
	if err != nil {
		return digestAbsent
	}
	if info.IsDir() {
		under := map[string]string{}
		unread := walk.unread
		// A CORNER OF THIS DIRECTORY THAT COULD NOT BE OPENED MAKES THE WHOLE
		// ANSWER UNKNOWN, because laying this path removes the directory whole and
		// what is inside that corner would go with it.
		if !gatherDigests(root, relative, under, walk) || walk.unread != unread {
			return digestUnknown
		}
		return foldDigests(under)
	}
	if walk.budget--; walk.budget < 0 {
		return digestUnknown
	}
	digest, ok := oneDigest(full, info)
	if !ok {
		return digestUnknown
	}
	return digest
}

// baselineIndex is [pathDigest]'s answer read off the record instead of off the
// disk, and the two are written to agree: a file is its own digest, a directory
// is the fold of everything the record holds beneath it, and a path the record
// never heard of is absent.
//
// THE SORTED KEYS ARE BUILT ONCE AND ONLY WHEN THEY ARE NEEDED. Nearly every
// ledger path is a file the record names outright, which costs a map lookup; a
// path that names a whole directory has to find everything beneath it, and
// walking twenty thousand recorded paths per ledger entry to do that is the
// quadratic this avoids. One sort, then a binary search per directory.
type baselineIndex struct {
	paths  map[string]string
	sorted []string
	built  bool
}

func (b *baselineIndex) digest(relative string) string {
	if digest, held := b.paths[relative]; held {
		return digest
	}
	if !b.built {
		b.sorted, b.built = make([]string, 0, len(b.paths)), true
		for recorded := range b.paths {
			b.sorted = append(b.sorted, recorded)
		}
		sort.Strings(b.sorted)
	}
	prefix := relative + "/"
	under := map[string]string{}
	for at := sort.SearchStrings(b.sorted, prefix); at < len(b.sorted); at++ {
		if !strings.HasPrefix(b.sorted[at], prefix) {
			break
		}
		under[b.sorted[at]] = b.paths[b.sorted[at]]
	}
	return foldDigests(under)
}

// foldDigests turns a set of paths and their digests into one, in a stable order
// so that two readings of the same tree fold to the same string.
//
// AN EMPTY SET IS ABSENT, and that is deliberate. A record cannot tell an empty
// directory from a directory that was never there, so both readings answer the
// same thing and an empty folder that is still empty compares equal rather than
// refusing a landing over nothing.
func foldDigests(paths map[string]string) string {
	if len(paths) == 0 {
		return digestAbsent
	}
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	sort.Strings(names)
	sum := sha256.New()
	for _, name := range names {
		_, _ = io.WriteString(sum, name+"\x00"+paths[name]+"\n")
	}
	return "tree " + hex.EncodeToString(sum.Sum(nil))
}

// oneDigest is what one file or one link is. The kind is part of the answer, so
// that a file somebody replaced with a symlink of the same bytes is a change and
// not a match.
//
// THE PERMISSION BITS ARE DELIBERATELY NOT IN IT. They would be the honest thing
// to compare if they survived the copy, and they do not: [copyPath] creates the
// mirror's file with the source's mode THROUGH THE UMASK, so a folder holding an
// ordinary 0664 file is copied to a 0644 one on most machines. A mode in the
// digest would therefore refuse a landing over files nobody had been near, which
// is the one failure this whole file must not have. What a person edits is the
// contents.
//
// WHAT CANNOT BE COPIED IS NOT COMPARED. A socket, a device node or a fifo is
// nobody's deliverable and [copyPath] passes over it without an error, so this
// gives every one of them the same answer rather than an opinion nobody can act
// on.
func oneDigest(full string, info os.FileInfo) (string, bool) {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		where, err := os.Readlink(full)
		if err != nil {
			return "", false
		}
		return "link " + where, true
	case !info.Mode().IsRegular():
		return "other", true
	}
	file, err := os.Open(full)
	if err != nil {
		return "", false
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", false
	}
	return "file " + hex.EncodeToString(sum.Sum(nil)), true
}
