// Package viewcache is the copy of another computer's chat that THIS computer
// keeps, so a chat that cannot be reached can still be read.
//
// A live view (another of the person's computers is running the chat and this
// one watches and types into it) leaves nothing behind on the relay: it carries
// bytes it cannot read and stores no chat text. That is the promise, and it is
// also the weakness, because when the other computer goes away there is nothing
// anywhere to show. So the viewer keeps, on its own disk and nowhere else, the
// last thing it drew: the final stretch of the transcript, the title and the
// moment it was last up to date. It is read-only by construction. Nothing here
// can send a word to the other computer, and the surface that draws it says so.
//
// THE TRUST BOUNDARY IS THE VIEWER'S OWN HOME. The copy is plain text, like the
// viewer's own transcripts under the same folder, readable by this person alone
// (directories 0700, files 0600). It holds what the host's transcript holds,
// which already leaves out whatever the seal guard keeps out of saved history,
// so it adds no new reach for a secret.
//
// IT IS BOUNDED, AND THE BOUNDS ARE ONE SET OF CONSTANTS. A copy that grew with
// every chat ever watched would be a second, unbounded transcript store on a
// laptop, so the last [MaxEntries] entries and at most [MaxBytes] of one chat
// are kept, for at most [MaxChats] chats, oldest out. PERF.md carries the same
// numbers and changes in the same commit as these.
package viewcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The bounds. A number that appears in two places drifts, so PERF.md and the
// manual quote these and nothing else spells them.
const (
	// MaxEntries is how many of the newest transcript entries one chat keeps.
	MaxEntries = 200
	// MaxBytes is the most one chat's file may weigh. Oldest entries go first
	// until it fits.
	MaxBytes = 1 << 20
	// MaxChats is how many chats the whole cache keeps, across every computer.
	MaxChats = 40
	// WriteEvery is the shortest gap between two writes of one chat's copy by
	// [Cache.Save]. A copy is written on a quiet beat, never per frame; this is
	// the floor under a caller that gets the beat wrong.
	WriteEvery = 2 * time.Second
)

// version is the on-disk format. It is the first field of every file.
const version = 1

// Clock is the one thing the cache asks the time for, so a test moves time
// instead of sleeping through it.
type Clock interface{ Now() time.Time }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Snapshot is one chat as this computer last saw it.
type Snapshot struct {
	// Host is the directory device id of the computer that runs the chat and
	// HostName the name a person knows it by.
	Host     string
	HostName string
	// Chat is the chat's cell id, the same id a Home row carries.
	Chat      string
	Title     string
	Model     string
	Workspace string
	// SeenAt is when this copy was last brought up to date from the live view.
	SeenAt time.Time
	// Base is the absolute index, counted from the start of the chat's
	// transcript, of Entries[0], and Total is how many entries the transcript
	// had. They keep "scroll up for older" honest after the copy has been cut to
	// its newest stretch.
	Base, Total int
	Entries     []session.DisplayEntry
}

// Cache is the viewer's folder of last-seen copies.
type Cache struct {
	root string
	clk  Clock

	mu    sync.Mutex
	wrote map[string]time.Time // key host/chat -> last write
}

// Open returns the cache under a CODEAF_HOME. Nothing is created until the
// first Save.
func Open(home string) *Cache { return OpenWith(home, realClock{}) }

// OpenWith is [Open] with a clock a test controls.
func OpenWith(home string, clk Clock) *Cache {
	if clk == nil {
		clk = realClock{}
	}
	return &Cache{
		root:  filepath.Join(home, "v3", "viewed"),
		clk:   clk,
		wrote: map[string]time.Time{},
	}
}

// fileDoc is what a file holds. V is first so a reader can tell the format
// before it trusts anything else in the file.
type fileDoc struct {
	V int `json:"V"`
	Snapshot
}

// folder turns a name a computer chose, or an id it was given, into one path
// element: letters, digits and three quiet punctuation marks, everything else a
// dash. It cannot name a parent folder, and two different names can only
// collide by being spelled almost identically.
func folder(name, empty string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" || strings.Trim(out, ".") == "" {
		return empty
	}
	return out
}

func (c *Cache) dir(host string) string { return filepath.Join(c.root, folder(host, "elsewhere")) }

func (c *Cache) path(host, chat string) string {
	return filepath.Join(c.dir(host), folder(chat, "chat")+".json")
}

func key(host, chat string) string { return folder(host, "elsewhere") + "/" + folder(chat, "chat") }

// Save writes the copy of one chat, unless it wrote that chat less than
// [WriteEvery] ago, in which case it does nothing and says nothing: the next
// quiet beat carries the newer one. Use [Cache.SaveNow] when the view is about
// to go away.
func (c *Cache) Save(host, chat string, s Snapshot) error {
	c.mu.Lock()
	last, ok := c.wrote[key(host, chat)]
	c.mu.Unlock()
	if ok && c.clk.Now().Sub(last) < WriteEvery {
		return nil
	}
	return c.SaveNow(host, chat, s)
}

// SaveNow writes the copy whatever was written a moment ago. It is for the
// beats that end a view (the person left it, the link closed), because those
// are the last chance to keep what was on screen.
func (c *Cache) SaveNow(host, chat string, s Snapshot) error {
	s.Host, s.Chat = host, chat
	if s.SeenAt.IsZero() {
		s.SeenAt = c.clk.Now()
	}
	s = bound(s)
	raw, err := marshalBounded(&s)
	if err != nil {
		return err
	}
	dir := c.dir(host)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// MkdirAll leaves an existing folder's mode alone; a folder somebody made
	// wider is narrowed again, because this one is private.
	_ = os.Chmod(c.root, 0o700)
	_ = os.Chmod(dir, 0o700)
	if err := writeAtomic(c.path(host, chat), raw); err != nil {
		return err
	}
	c.mu.Lock()
	c.wrote[key(host, chat)] = c.clk.Now()
	c.mu.Unlock()
	c.Prune()
	return nil
}

// bound cuts a snapshot to its newest [MaxEntries] and keeps Base and Total
// consistent with what it cut.
func bound(s Snapshot) Snapshot {
	if len(s.Entries) > MaxEntries {
		drop := len(s.Entries) - MaxEntries
		s.Entries = append([]session.DisplayEntry(nil), s.Entries[drop:]...)
		s.Base += drop
	}
	if s.Total < s.Base+len(s.Entries) {
		s.Total = s.Base + len(s.Entries)
	}
	return s
}

// marshalBounded renders the file and drops the oldest entries until it weighs
// no more than [MaxBytes]. A chat whose single newest entry is larger than that
// is kept with no entries at all, which still says when it was last seen and
// what it was called.
func marshalBounded(s *Snapshot) ([]byte, error) {
	for {
		raw, err := json.Marshal(fileDoc{V: version, Snapshot: *s})
		if err != nil {
			return nil, err
		}
		if len(raw) <= MaxBytes || len(s.Entries) == 0 {
			return raw, nil
		}
		// Drop the share of the entries that the excess is of the file, plus
		// one, so a long chat of long entries is not marshalled once per entry.
		over := len(raw) - MaxBytes
		drop := min(len(s.Entries), len(s.Entries)*over/len(raw)+1)
		s.Entries = append([]session.DisplayEntry(nil), s.Entries[drop:]...)
		s.Base += drop
	}
}

// writeAtomic puts the bytes where the file belongs through a temporary file in
// the same folder, so a reader never meets half of one.
func writeAtomic(path string, raw []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".viewed-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := tmp.Write(raw); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := fsreplace.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// Load reads the copy of one chat. It answers false for a chat that was never
// kept, for a file it cannot read, and for one that says it is about another
// chat or another computer: a copy it cannot vouch for is not shown.
func (c *Cache) Load(host, chat string) (Snapshot, bool) {
	raw, err := os.ReadFile(c.path(host, chat))
	if err != nil {
		return Snapshot{}, false
	}
	var doc fileDoc
	if err := json.Unmarshal(raw, &doc); err != nil || doc.V != version {
		return Snapshot{}, false
	}
	if doc.Host != host || doc.Chat != chat {
		return Snapshot{}, false
	}
	return doc.Snapshot, true
}

// Forget removes every copy kept for one computer. It is what removing the
// computer from the person's devices does, and what moving a chat here does for
// that chat ([Cache.ForgetChat]).
func (c *Cache) Forget(host string) {
	_ = os.RemoveAll(c.dir(host))
	prefix := folder(host, "elsewhere") + "/"
	c.mu.Lock()
	for k := range c.wrote {
		if strings.HasPrefix(k, prefix) {
			delete(c.wrote, k)
		}
	}
	c.mu.Unlock()
}

// ForgetChat removes the copy of one chat.
func (c *Cache) ForgetChat(host, chat string) {
	_ = os.Remove(c.path(host, chat))
	c.mu.Lock()
	delete(c.wrote, key(host, chat))
	c.mu.Unlock()
}

// ForgetChatEverywhere removes the copy of one chat kept for every computer.
// Moving a chat here uses it, because the window that moved it does not know
// which computer the copy was last seen on, and a copy of a chat that now runs
// on this computer is only a stale picture of it.
func (c *Cache) ForgetChatEverywhere(chat string) {
	hosts, err := os.ReadDir(c.root)
	if err != nil {
		return
	}
	for _, host := range hosts {
		if !host.IsDir() {
			continue
		}
		_ = os.Remove(filepath.Join(c.root, host.Name(), folder(chat, "chat")+".json"))
		c.mu.Lock()
		delete(c.wrote, host.Name()+"/"+folder(chat, "chat"))
		c.mu.Unlock()
	}
}

type found struct {
	path string
	at   time.Time
}

// Prune keeps the newest [MaxChats] copies across every computer, removes the
// stray temporary files a crashed write left behind, and removes a computer's
// folder once nothing is left in it.
func (c *Cache) Prune() {
	hosts, err := os.ReadDir(c.root)
	if err != nil {
		return
	}
	var all []found
	for _, h := range hosts {
		if !h.IsDir() {
			continue
		}
		dir := filepath.Join(c.root, h.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(e.Name(), ".tmp") {
				if info, err := e.Info(); err == nil && c.clk.Now().Sub(info.ModTime()) > time.Minute {
					_ = os.Remove(path)
				}
				continue
			}
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			at := seenAt(path)
			if at.IsZero() {
				_ = os.Remove(path)
				continue
			}
			all = append(all, found{path: path, at: at})
		}
		if len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
	if len(all) <= MaxChats {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	for _, old := range all[MaxChats:] {
		_ = os.Remove(old.path)
	}
}

// seenAt is when a file was last written, which is when its copy was last
// brought up to date. It is read from the file's own time and not from its
// contents, so pruning forty chats does not read forty megabytes.
func seenAt(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
