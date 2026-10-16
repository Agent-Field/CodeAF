package session

// deliverable_branch_cache.go is what git says about task branches, asked ONCE
// per request however many rows name the same branch.
//
// A listing of a hundred rows a task left on one branch used to ask git five
// questions per row - the tip, two merge-bases, a whole-branch diff to count
// files nobody reads here, and the tree entry - none of them under the
// listing's deadline. The source authority needs two facts about a branch (its
// tip, and that HEAD does not reach it) and one about each rel (the entry the
// tip holds there). So:
//   - a branch's tip and "unmerged" are asked once per (workspace, branch),
//     with rev-parse and merge-base --is-ancestor under the caller's context;
//   - a tip's entries are asked once per (tip, folder): one ls-tree lists the
//     folder, and every rel in it is answered from that, so a task's documents
//     side by side cost one call between them;
//   - an answer git could not give in time is UNKNOWN, never "gone": the caller
//     stops and continues later rather than describe a row it did not check.
// A cache lives for one request and is then dropped; nothing here outlives it.
//
// AND IT IS BOUNDED WHILE IT LIVES. A count may walk the whole history, so a
// cache that kept every folder and tip it met would grow with the history. One
// budget covers everything it retains - tips, folder listings, and the marks
// of folders too big to keep - in bytes (every string's length plus a fixed
// deliverCacheSlotCost for its slot) and in slots. A folder is parsed into a
// map that may never cost more than the room left (and never more than
// deliverCacheFolderMax), and is given up the moment it would; it is then
// answered one rel at a time. Once anything does not fit, the cache stops
// listing and stops keeping: every later question is asked exactly, of the
// pinned tip, under the caller's deadline. A fact is never guessed or dropped
// for want of room - only asked again.

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
)

// deliverFolderListMax bounds one folder listing's output. A folder past it is
// answered one rel at a time instead.
const deliverFolderListMax = 4 << 20

// branchVerdict is what git said about a task branch.
type branchVerdict int

const (
	branchUnknown  branchVerdict = iota // git could not say (in time)
	branchUnmerged                      // it exists and HEAD does not reach it
	branchSettled                       // it is gone, or HEAD reaches it
)

type branchTip struct {
	verdict branchVerdict
	tip     string
}

// The retained budget of one request's cache. Variables only so a test can
// make a small one.
var (
	// deliverCacheRetainMax is the most bytes one cache keeps, by its own
	// conservative count.
	deliverCacheRetainMax int64 = 2 << 20
	// deliverCacheSlotsMax is the most tips, folder keys and entries it keeps.
	deliverCacheSlotsMax = 16384
	// deliverCacheFolderMax is the most one folder's listing may cost.
	deliverCacheFolderMax int64 = 1 << 20
)

// deliverCacheSlotCost is what one kept slot is counted at beyond its strings:
// the map slot, the string headers and the allocation rounding, generously.
const deliverCacheSlotCost = 128

// DeliverSourceCache is one request's memory of what git said. The zero value
// is not usable; make one with [NewDeliverSourceCache]. It is safe for
// concurrent use.
type DeliverSourceCache struct {
	mu      sync.Mutex
	tips    map[string]branchTip
	folders map[string]map[string]treeEntry // tip + folder -> path -> entry
	// large marks folders too big to keep (or that git would not list): they
	// are answered one rel at a time.
	large map[string]bool
	// retained and slots are what is kept, by the budget's count; full says
	// something did not fit, and nothing more is listed or kept.
	retained int64
	slots    int
	full     bool
	// calls counts the git processes this cache started, for a test to bound.
	calls int
}

// NewDeliverSourceCache is an empty cache for one request.
func NewDeliverSourceCache() *DeliverSourceCache {
	return &DeliverSourceCache{tips: map[string]branchTip{}, folders: map[string]map[string]treeEntry{}, large: map[string]bool{}}
}

// GitCalls is how many git processes the cache has started.
func (c *DeliverSourceCache) GitCalls() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// Retained is what the cache keeps, by its budget's count: bytes and slots.
func (c *DeliverSourceCache) Retained() (int64, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.retained, c.slots
}

func (c *DeliverSourceCache) counted(n int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls += n
}

// reserveLocked takes bytes and slots from the budget, or marks the cache full
// and takes nothing.
func (c *DeliverSourceCache) reserveLocked(bytes int64, slots int) bool {
	if c.full || c.retained+bytes > deliverCacheRetainMax || c.slots+slots > deliverCacheSlotsMax {
		c.full = true
		return false
	}
	c.retained += bytes
	c.slots += slots
	return true
}

// room is how much one more folder listing may cost now: nothing once full.
func (c *DeliverSourceCache) room() (int64, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.full {
		return 0, 0
	}
	return min(deliverCacheRetainMax-c.retained, deliverCacheFolderMax), deliverCacheSlotsMax - c.slots
}

func (c *DeliverSourceCache) tipFor(key string) (branchTip, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	got, ok := c.tips[key]
	return got, ok
}

func (c *DeliverSourceCache) rememberTip(key string, tip branchTip) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.tips[key]; ok {
		return
	}
	if c.reserveLocked(int64(len(key)+len(tip.tip))+deliverCacheSlotCost, 1) {
		c.tips[key] = tip
	}
}

// folderFor is a kept listing, or large when the folder is answered rel by rel
// (marked so, or because the cache is full and will list nothing more).
func (c *DeliverSourceCache) folderFor(key string) (entries map[string]treeEntry, listed, large bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.large[key] {
		return nil, false, true
	}
	if entries, ok := c.folders[key]; ok {
		return entries, true, false
	}
	return nil, false, c.full
}

// rememberFolder keeps a listing that cost cost, or marks the folder large
// when it does not fit.
func (c *DeliverSourceCache) rememberFolder(key string, entries map[string]treeEntry, cost int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.folders[key]; ok || c.large[key] {
		return
	}
	keyCost := int64(len(key)) + deliverCacheSlotCost
	if entries != nil && c.reserveLocked(keyCost+cost, 1+len(entries)) {
		c.folders[key] = entries
		return
	}
	// Too big (or not listable): marked, so it is not listed again. Should
	// even the mark not fit, the cache is full and lists nothing more anyway.
	if c.reserveLocked(keyCost, 1) {
		c.large[key] = true
	}
}

// branchTipOf is branch's tip and verdict in workspace, from the cache or from
// git. An unknown answer is not remembered, so a later call may still learn it.
func (c *DeliverSourceCache) branchTipOf(ctx context.Context, workspace, branch string) branchTip {
	key := workspace + "\x00" + branch
	if c != nil {
		if got, ok := c.tipFor(key); ok {
			return got
		}
	}
	got := askBranchTip(ctx, c, workspace, branch)
	if c != nil && got.verdict != branchUnknown {
		c.rememberTip(key, got)
	}
	return got
}

// askBranchTip asks git, under ctx: the tip of refs/heads/branch, and whether
// HEAD reaches it. Exit 1 from each is git's own "no"; anything else, or a
// context that ended, is unknown.
func askBranchTip(ctx context.Context, c *DeliverSourceCache, workspace, branch string) branchTip {
	if !validTaskBranch(branch) || strings.TrimSpace(workspace) == "" {
		return branchTip{verdict: branchSettled}
	}
	ctx, cancel := context.WithTimeout(ctx, deliverGitCallMax)
	defer cancel()
	c.counted(1)
	out, err := gitContext(ctx, workspace, nil, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	switch code, ran := gitExit(err); {
	case ctx.Err() != nil:
		return branchTip{}
	case err == nil:
	case ran && code == 1:
		return branchTip{verdict: branchSettled} // no such branch
	default:
		return branchTip{}
	}
	tip := strings.TrimSpace(out)
	if len(tip) < 40 || !isHex(tip) {
		return branchTip{}
	}
	c.counted(1)
	_, err = gitContext(ctx, workspace, nil, "merge-base", "--is-ancestor", tip, "HEAD")
	switch code, ran := gitExit(err); {
	case ctx.Err() != nil:
		return branchTip{}
	case err == nil:
		return branchTip{verdict: branchSettled} // HEAD reaches it: merged
	case ran && code == 1:
		return branchTip{verdict: branchUnmerged, tip: tip}
	}
	return branchTip{}
}

// entryAt is what tip holds at rel, from the folder's one listing when it can
// be had and otherwise from a listing of rel alone. known is false when git
// could not say.
func (c *DeliverSourceCache) entryAt(ctx context.Context, workspace, tip, rel string) (entry treeEntry, present, known bool) {
	if !validBranchRel(rel) {
		return treeEntry{}, false, true
	}
	if c == nil {
		// One question with nothing to share: the rel alone.
		single, err := treeEntries(ctx, workspace, tip, []string{rel})
		if err != nil {
			return treeEntry{}, false, false
		}
		entry, present = single[rel]
		return entry, present, true
	}
	folder := path.Dir(rel)
	key := tip + "\x00" + folder
	entries, listed, large := c.folderFor(key)
	if !listed && !large {
		bytes, slots := c.room()
		var cost int64
		entries, cost = listFolder(ctx, c, workspace, tip, folder, bytes, slots)
		if ctx.Err() != nil {
			return treeEntry{}, false, false
		}
		c.rememberFolder(key, entries, cost)
	}
	if entries != nil {
		entry, present = entries[rel]
		return entry, present, true
	}
	// The folder is not kept whole: this one rel, by itself, of the same tip.
	c.counted(1)
	single, err := treeEntries(ctx, workspace, tip, []string{rel})
	if err != nil {
		return treeEntry{}, false, false
	}
	entry, present = single[rel]
	return entry, present, true
}

// listFolder is every entry tip holds directly in folder ("." is the top), in
// one bounded ls-tree, with what keeping it would cost; nil when it could not
// be had or would cost more than bytes or slots. The output is read once and
// parsed a record at a time, each kept string COPIED out of it (so nothing
// kept holds the output alive), and parsing stops at the first record past the
// budget.
func listFolder(ctx context.Context, c *DeliverSourceCache, workspace, tip, folder string, bytes int64, slots int) (map[string]treeEntry, int64) {
	if bytes <= 0 || slots <= 1 {
		return nil, 0
	}
	args := []string{"ls-tree", "-z", "--full-tree", tip}
	if folder != "." {
		if !validBranchRel(folder) {
			return nil, 0
		}
		args = append(args, "--", folder+"/")
	}
	ctx, cancel := context.WithTimeout(ctx, deliverGitCallMax)
	defer cancel()
	c.counted(1)
	out, ok := gitBounded(ctx, workspace, deliverFolderListMax, args...)
	if !ok {
		return nil, 0
	}
	entries := map[string]treeEntry{}
	var cost int64
	for len(out) > 0 {
		record := out
		if end := bytesIndex(out, 0); end >= 0 {
			record, out = out[:end], out[end+1:]
		} else {
			out = nil
		}
		name, entry, ok := parseTreeRecord(record)
		if !ok {
			continue
		}
		cost += int64(len(name)+len(entry.oid)) + deliverCacheSlotCost
		if cost > bytes || len(entries)+2 > slots {
			return nil, 0 // too big to keep: answered rel by rel
		}
		entries[name] = entry
	}
	return entries, cost
}

// parseTreeRecord is one ls-tree record, "<mode> <type> <oid>\t<path>", with
// the path and id copied and the mode and type interned.
func parseTreeRecord(record []byte) (string, treeEntry, bool) {
	tab := bytesIndex(record, '\t')
	if tab < 0 {
		return "", treeEntry{}, false
	}
	fields := strings.Fields(string(record[:tab]))
	if len(fields) != 3 {
		return "", treeEntry{}, false
	}
	return string(record[tab+1:]), treeEntry{mode: internTreeWord(fields[0]), kind: internTreeWord(fields[1]), oid: fields[2]}, true
}

// internTreeWord is the shared spelling of a mode or type ls-tree prints.
func internTreeWord(word string) string {
	for _, known := range [...]string{"100644", "100755", "120000", "160000", "040000", "blob", "tree", "commit"} {
		if word == known {
			return known
		}
	}
	return word
}

func bytesIndex(data []byte, b byte) int {
	for i, c := range data {
		if c == b {
			return i
		}
	}
	return -1
}

// gitBounded runs git in dir under ctx, with literal pathspecs, and answers
// its output when it exited cleanly within max bytes.
func gitBounded(ctx context.Context, dir string, max int, args ...string) ([]byte, bool) {
	if strings.TrimSpace(dir) == "" {
		return nil, false
	}
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	command.Env = append(command.Env, literalPaths...)
	out, err := command.StdoutPipe()
	if err != nil {
		return nil, false
	}
	if err := command.Start(); err != nil {
		return nil, false
	}
	data, readErr := io.ReadAll(io.LimitReader(out, int64(max)+1))
	_ = out.Close() // past max git sees a closed pipe and stops
	waitErr := command.Wait()
	if readErr != nil || waitErr != nil || len(data) > max || ctx.Err() != nil {
		return nil, false
	}
	return data, true
}
