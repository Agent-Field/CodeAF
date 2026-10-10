package council

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	lockPoll    = 10 * time.Millisecond
	maxFileSize = 8 << 20
)

// Errors a caller can match. A missing or archived place is the place graph's
// own error, returned unchanged, because that graph is the authority for
// whether the place exists.
var (
	ErrInvalid   = errors.New("council: invalid")
	ErrNotFound  = errors.New("council: not found")
	ErrOpen      = errors.New("council: that pair is already discussing this topic")
	ErrCooldown  = errors.New("council: that pair cannot reopen this topic yet")
	ErrEnded     = errors.New("council: that discussion has ended")
	ErrLocked    = errors.New("council: lock busy")
	ErrNewerFile = errors.New("council: file is from a newer version")
)

// Options configures a Store. Path, SessionsDir and Places are required.
type Options struct {
	// Path is the councils document. The directory is created if missing.
	// "<Path>.lock" is the lock file beside it.
	Path string
	// SessionsDir is the bucket ordinary session folders are minted in.
	// Each council's chat is one folder named by its session id.
	SessionsDir string
	// Places is the graph the chat is filed into. Both places must already exist.
	Places *placegraph.Store
	// Now is the clock. Tests inject one. Cooldown is measured with it.
	Now func() time.Time
	// NewID mints council ids. The default is "cn_" plus 16 hex characters.
	NewID func() string
	// NewChatID mints the session id. The default is [session.NewSessionID].
	NewChatID func() string
	// LockTimeout bounds the wait for another process's lock. Default 10s.
	LockTimeout time.Duration
}

// Store is the councils document plus the sessions it mints. It is safe for
// concurrent use. Every read and write takes the file lock, so two processes
// sharing Path cannot act on a stale cooldown index.
type Store struct {
	opts Options
	mu   sync.Mutex
}

type document struct {
	Version  int       `json:"version"`
	Councils []Council `json:"councils"`
}

// Open prepares the store. A missing file is an empty document. A file this
// build cannot read is left untouched and returned as an error: starting
// empty would forget the cooldown index and let a pair reopen a topic early.
func Open(opts Options) (*Store, error) {
	if strings.TrimSpace(opts.Path) == "" {
		return nil, fmt.Errorf("%w: empty path", ErrInvalid)
	}
	if strings.TrimSpace(opts.SessionsDir) == "" {
		return nil, fmt.Errorf("%w: empty sessions directory", ErrInvalid)
	}
	if opts.Places == nil {
		return nil, fmt.Errorf("%w: no place graph", ErrInvalid)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.NewID == nil {
		opts.NewID = newCouncilID
	}
	if opts.NewChatID == nil {
		opts.NewChatID = session.NewSessionID
	}
	if opts.LockTimeout <= 0 {
		opts.LockTimeout = 10 * time.Second
	}
	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.SessionsDir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{opts: opts}
	release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	if _, err := s.loadLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// Begin opens a discussion between two places on one topic.
//
// It mints an ordinary session titled "<A> with <B>" (the place names, in the
// order given), files that chat in both places with addedBy ai, and records
// the council. The same unordered pair and normalised topic is refused while
// a discussion is still running or paused, and for [Cooldown] after one opened.
func (s *Store) Begin(placeA, placeB, topic string) (Council, error) {
	key, err := TopicKey(topic)
	if err != nil {
		return Council{}, err
	}
	if placeA == "" || placeB == "" || placeA == placeB {
		return Council{}, fmt.Errorf("%w: a council needs two different places", ErrInvalid)
	}
	release, err := s.acquire()
	if err != nil {
		return Council{}, err
	}
	defer release()

	doc, err := s.loadLocked()
	if err != nil {
		return Council{}, err
	}
	if err := doc.blocked(placeA, placeB, key, s.now()); err != nil {
		return Council{}, err
	}
	nameA, nameB, err := s.placeNames(placeA, placeB)
	if err != nil {
		return Council{}, err
	}
	chatID := s.opts.NewChatID()
	if !validSessionID(chatID) {
		return Council{}, fmt.Errorf("%w: bad chat id", ErrInvalid)
	}
	id := s.opts.NewID()
	if err := validCouncilID(id); err != nil {
		return Council{}, err
	}
	if doc.hasID(id) || doc.hasChat(chatID) {
		return Council{}, fmt.Errorf("%w: id already used", ErrInvalid)
	}
	dir := s.sessionDir(chatID)
	if _, err := os.Stat(dir); err == nil {
		return Council{}, fmt.Errorf("%w: session %s already exists", ErrInvalid, chatID)
	} else if !os.IsNotExist(err) {
		return Council{}, err
	}

	label := Label(nameA, nameB)
	now := s.now()
	if err := mintSession(dir, chatID, label, now); err != nil {
		return Council{}, err
	}
	if _, _, err := s.opts.Places.AddChat(chatID, placeA, placegraph.AddedByAI); err != nil {
		_ = os.RemoveAll(dir)
		return Council{}, err
	}
	if _, _, err := s.opts.Places.AddChat(chatID, placeB, placegraph.AddedByAI); err != nil {
		_, _ = s.opts.Places.RemoveChat(chatID, placeA)
		_ = os.RemoveAll(dir)
		return Council{}, err
	}
	c := Council{
		ID:       id,
		Places:   [2]string{placeA, placeB},
		Topic:    key,
		ChatID:   chatID,
		Label:    label,
		Turns:    0,
		Cap:      TurnCap,
		Spend:    0,
		CapUSD:   SpendCapUSD,
		State:    StateRunning,
		OpenedAt: now,
	}
	if err := c.validate(); err != nil {
		_, _ = s.opts.Places.RemoveChat(chatID, placeA)
		_, _ = s.opts.Places.RemoveChat(chatID, placeB)
		_ = os.RemoveAll(dir)
		return Council{}, err
	}
	doc.Councils = append(doc.Councils, c)
	if err := s.writeLocked(doc); err != nil {
		_, _ = s.opts.Places.RemoveChat(chatID, placeA)
		_, _ = s.opts.Places.RemoveChat(chatID, placeB)
		_ = os.RemoveAll(dir)
		return Council{}, err
	}
	return c, nil
}

// Get returns one council by id.
func (s *Store) Get(id string) (Council, error) {
	release, err := s.acquire()
	if err != nil {
		return Council{}, err
	}
	defer release()
	doc, err := s.loadLocked()
	if err != nil {
		return Council{}, err
	}
	c, ok := doc.find(id)
	if !ok {
		return Council{}, ErrNotFound
	}
	return c, nil
}

// List returns every council in the order it was opened.
func (s *Store) List() ([]Council, error) {
	release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	doc, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	out := append([]Council(nil), doc.Councils...)
	return out, nil
}

// InPlace returns the councils filed in one place, oldest first.
func (s *Store) InPlace(placeID string) ([]Council, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	out := make([]Council, 0)
	for _, c := range all {
		if c.Places[0] == placeID || c.Places[1] == placeID {
			out = append(out, c)
		}
	}
	return out, nil
}

// Record moves a discussion: its turn count, its spend, and its state.
//
// Turns cannot go backwards and cannot pass [TurnCap]. Spend cannot go
// backwards. It may pass [SpendCapUSD], because the turn that crosses the
// cap is still that discussion's cost and is recorded before the state
// becomes escalated. A decided or escalated discussion is finished; repeating
// the same record is a no-op and any change is [ErrEnded].
func (s *Store) Record(id string, turns int, spend float64, state State, outcome string) (Council, error) {
	if !state.Valid() {
		return Council{}, fmt.Errorf("%w: state %q", ErrInvalid, state)
	}
	outcome = strings.TrimSpace(outcome)
	if !state.Ended() && outcome != "" {
		return Council{}, fmt.Errorf("%w: an open discussion has no outcome", ErrInvalid)
	}
	release, err := s.acquire()
	if err != nil {
		return Council{}, err
	}
	defer release()
	doc, err := s.loadLocked()
	if err != nil {
		return Council{}, err
	}
	i := doc.index(id)
	if i < 0 {
		return Council{}, ErrNotFound
	}
	cur := doc.Councils[i]
	if cur.State.Ended() {
		if turns == cur.Turns && !spendLess(spend, cur.Spend) && !spendLess(cur.Spend, spend) && state == cur.State && outcome == cur.Outcome {
			return cur, nil
		}
		return Council{}, ErrEnded
	}
	if !canMove(cur.State, state) {
		return Council{}, fmt.Errorf("%w: %s to %s", ErrInvalid, cur.State, state)
	}
	if turns < cur.Turns || turns > TurnCap {
		return Council{}, fmt.Errorf("%w: turns", ErrInvalid)
	}
	if spendLess(spend, cur.Spend) || spend < 0 {
		return Council{}, fmt.Errorf("%w: spend", ErrInvalid)
	}
	cur.Turns = turns
	cur.Spend = spend
	cur.State = state
	cur.Outcome = outcome
	if state.Ended() {
		cur.ClosedAt = s.now()
	} else {
		cur.ClosedAt = time.Time{}
		cur.Outcome = ""
	}
	if err := cur.validate(); err != nil {
		return Council{}, err
	}
	doc.Councils[i] = cur
	if err := s.writeLocked(doc); err != nil {
		return Council{}, err
	}
	return cur, nil
}

// SessionDir is the ordinary session folder for a chat id. An id that is not
// a session id answers "", so a caller cannot walk out of the sessions bucket.
func (s *Store) SessionDir(chatID string) string {
	if !validSessionID(chatID) {
		return ""
	}
	return s.sessionDir(chatID)
}

func (s *Store) sessionDir(chatID string) string {
	return filepath.Join(s.opts.SessionsDir, chatID)
}

func (s *Store) now() time.Time {
	return s.opts.Now().UTC()
}

func (s *Store) placeNames(a, b string) (string, string, error) {
	snap, err := s.opts.Places.Snapshot()
	if err != nil {
		return "", "", err
	}
	pa, ok := snap.Place(a)
	if !ok {
		return "", "", fmt.Errorf("%w: %s", placegraph.ErrNotFound, a)
	}
	pb, ok := snap.Place(b)
	if !ok {
		return "", "", fmt.Errorf("%w: %s", placegraph.ErrNotFound, b)
	}
	if pa.Archived {
		return "", "", fmt.Errorf("%w: %s", placegraph.ErrArchived, a)
	}
	if pb.Archived {
		return "", "", fmt.Errorf("%w: %s", placegraph.ErrArchived, b)
	}
	return pa.Name, pb.Name, nil
}

func canMove(from, to State) bool {
	if from == to {
		return true
	}
	switch from {
	case StateRunning:
		return to == StatePaused || to.Ended()
	case StatePaused:
		return to == StateRunning || to.Ended()
	default:
		return false
	}
}

// spendLess reports whether a is meaningfully under b. A fraction of a cent
// of float noise is not a decrease.
func spendLess(a, b float64) bool {
	return a < b && b-a > 1e-9
}

// blocked is the cooldown index. It is computed from the councils themselves,
// not stored beside them, so a row and the index cannot disagree. The pair is
// unordered. A discussion that is still running or paused blocks even after
// the hour; one that has ended blocks until an hour after it opened.
func (d *document) blocked(a, b, topic string, now time.Time) error {
	cooling := false
	for i := range d.Councils {
		c := &d.Councils[i]
		if c.Topic != topic || !SamePair(c.Places, a, b) {
			continue
		}
		if c.State == StateRunning || c.State == StatePaused {
			return ErrOpen
		}
		if now.Before(c.OpenedAt.Add(Cooldown)) {
			cooling = true
		}
	}
	if cooling {
		return ErrCooldown
	}
	return nil
}

func (d *document) find(id string) (Council, bool) {
	i := d.index(id)
	if i < 0 {
		return Council{}, false
	}
	return d.Councils[i], true
}

func (d *document) index(id string) int {
	for i := range d.Councils {
		if d.Councils[i].ID == id {
			return i
		}
	}
	return -1
}

func (d *document) hasID(id string) bool { return d.index(id) >= 0 }

func (d *document) hasChat(chatID string) bool {
	for i := range d.Councils {
		if d.Councils[i].ChatID == chatID {
			return true
		}
	}
	return false
}

// mintSession writes one ordinary session folder: meta.json titled with the
// label, a journal that already carries that title so a resume does not ask
// the namer to replace it, and work/ so an empty journal is not litter the
// launch groom may remove. Nobody has spoken in it.
func mintSession(dir, id, label string, now time.Time) error {
	place := session.Place{Dir: dir, Owned: true}
	place.Workspace = place.Work()
	if err := os.MkdirAll(place.Workspace, 0o700); err != nil {
		return err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	header, err := json.Marshal(struct {
		Type      string `json:"type"`
		Version   int    `json:"version"`
		ID        string `json:"id"`
		Cwd       string `json:"cwd"`
		Timestamp string `json:"timestamp"`
	}{Type: "session", Version: 1, ID: id, Cwd: place.Workspace, Timestamp: stamp})
	if err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	title, err := json.Marshal(struct {
		Type      string `json:"type"`
		Title     string `json:"title"`
		Timestamp string `json:"timestamp"`
	}{Type: "title", Title: label, Timestamp: stamp})
	if err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	body := append(append(header, '\n'), title...)
	body = append(body, '\n')
	if err := os.WriteFile(place.Transcript(), body, 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	if err := session.SaveMeta(dir, session.Meta{
		ID:        id,
		Title:     label,
		Workspace: place.Workspace,
		Owned:     true,
		Created:   now.UTC(),
	}); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	return nil
}

func (s *Store) acquire() (func(), error) {
	s.mu.Lock()
	f, err := os.OpenFile(s.opts.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	deadline := time.Now().Add(s.opts.LockTimeout)
	for {
		err := filelock.Lock(f, true, true)
		if err == nil {
			break
		}
		if !filelock.IsBusy(err) {
			f.Close()
			s.mu.Unlock()
			return nil, err
		}
		if time.Now().After(deadline) {
			f.Close()
			s.mu.Unlock()
			return nil, ErrLocked
		}
		time.Sleep(lockPoll)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
		s.mu.Unlock()
	}, nil
}

func (s *Store) loadLocked() (*document, error) {
	data, err := os.ReadFile(s.opts.Path)
	if errors.Is(err, os.ErrNotExist) {
		return &document{Version: SchemaVersion, Councils: []Council{}}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("%w: councils file is too large", ErrInvalid)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var doc document
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: councils file is not valid JSON: %v", ErrInvalid, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: unexpected data after the councils document", ErrInvalid)
	}
	if doc.Version > SchemaVersion {
		return nil, fmt.Errorf("%w (file version %d, this build %d)", ErrNewerFile, doc.Version, SchemaVersion)
	}
	if doc.Councils == nil {
		doc.Councils = []Council{}
	}
	seenID := map[string]bool{}
	seenChat := map[string]bool{}
	for _, c := range doc.Councils {
		if err := c.validate(); err != nil {
			return nil, err
		}
		if seenID[c.ID] || seenChat[c.ChatID] {
			return nil, fmt.Errorf("%w: duplicate council", ErrInvalid)
		}
		seenID[c.ID] = true
		seenChat[c.ChatID] = true
	}
	doc.Version = SchemaVersion
	return &doc, nil
}

func (s *Store) writeLocked(doc *document) error {
	doc.Version = SchemaVersion
	if doc.Councils == nil {
		doc.Councils = []Council{}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxFileSize {
		return fmt.Errorf("%w: councils file is too large", ErrInvalid)
	}
	dir := filepath.Dir(s.opts.Path)
	tmp, err := os.CreateTemp(dir, ".councils-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, s.opts.Path); err != nil {
		_ = os.Remove(name)
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func newCouncilID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("cn_%016x", time.Now().UnixNano())
	}
	return "cn_" + hex.EncodeToString(raw[:])
}
