package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// A session is one room of conversation. Until this table existed, `session_id`
// was an untyped column on messages — a string that appeared in rows and
// belonged to nothing — so a thread had no title, no birthday, no surface and
// no lifecycle, and the only way to learn that a thread existed was to find a
// message carrying its name.
//
// The table is a projection like every other view in this store: it is derived
// from the messages that name a session, minted by the first one and raised by
// each one after it, so a rebuild reconstructs it from the journal alone. Rows
// are never deleted here — a thread that stops being spoken in is a thread with
// an old activity mark, not a thread that never happened.
const sessionSchema = `
CREATE TABLE IF NOT EXISTS sessions (
    id             TEXT PRIMARY KEY,
    title          TEXT NOT NULL DEFAULT '',
    tags           JSON NOT NULL DEFAULT '[]' CHECK (json_valid(tags)),
    surface        TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    last_active_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_last_active ON sessions (last_active_at DESC, id);

-- The per-session resume read below asks for the newest non-user row of every
-- session at once. This index carries all three columns it touches, so that
-- read is an ordered covering scan rather than a row lookup per message; it
-- indexes the messages table but exists for this file's queries, which is why
-- it is declared here rather than beside the table.
CREATE INDEX IF NOT EXISTS messages_session_role_seq ON messages (session_id, role, seq);
`

// Session is one conversation thread's own row.
type Session struct {
	ID    string
	Title string
	// Tags are the subject words the naming pass filed this room under. They
	// are a FINDING aid and not a second title: nothing draws them as chips,
	// and the one place they earn their keep is a switcher's filter, where a
	// person who remembers what a conversation was ABOUT but not what it ended
	// up called can still type their way back into it.
	//
	// They are written by the same pass that names the room and by nothing
	// else. A person renaming a tab states a new NAME; they are not restating
	// what the conversation is about, so [Store.RenameSession] leaves whatever
	// is here exactly as it stands.
	Tags    []string
	Surface string
	// Created is the first message that named this session; LastActive is the
	// newest one. Both are message times rather than wall-clock times taken
	// here, so a rebuild reproduces them exactly.
	Created    time.Time
	LastActive time.Time
}

// MaxSessionTitleBytes bounds a room's display title. A title is a tab label,
// not a description; anything longer is truncated rather than refused, because
// what a person types into a rename box should never fail to open a room.
const MaxSessionTitleBytes = 256

const (
	// MaxSessionTags is how many subjects one room may be filed under. Three,
	// because a tag list long enough to need scrolling is a summary wearing a
	// filter's clothes — and because a room that is about six things is a room
	// whose tags will match everything a person types.
	MaxSessionTags = 3
	// MaxSessionTagBytes bounds one tag. A tag is a word or two; anything
	// longer is a sentence, and a sentence never helps a subsequence filter.
	MaxSessionTagBytes = 32
)

// normalizeSessionTags is the one door a tag list takes into the store:
// lower-cased, trimmed, bounded, de-duplicated, and capped.
//
// It runs on the write path rather than at the caller so a replay lands exactly
// what the live write landed — the projection is derived from the journal, and
// a normalizer that lived in the head would leave a rebuilt store holding
// different rows from the one it rebuilt.
func normalizeSessionTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	out := make([]string, 0, MaxSessionTags)
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		tag = strings.TrimSpace(bounded(tag, MaxSessionTagBytes))
		if tag == "" || slices.Contains(out, tag) {
			continue
		}
		out = append(out, tag)
		if len(out) == MaxSessionTags {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// migrateSessionSchema gives an existing database the tags column. It follows
// the thread migration's shape exactly ([addJSONColumn]) and is idempotent: an
// empty list is the true thing to say about every room named before tags
// existed, and the naming pass never runs again on a room that already has a
// title, so those rooms stay findable by name alone rather than being re-billed
// for a label.
func migrateSessionSchema(db *sql.DB) error {
	return addJSONColumn(db, "sessions", "tags")
}

// sessionOpenedPayload is a room's birth certificate on the wire. There is no
// created-at field: the event's own journal timestamp IS the birthday, exactly
// as a message's timestamp is what dates the room a message mints. A second
// time in the payload could disagree with the envelope, and replay would have
// to choose which of the two lies.
type sessionOpenedPayload struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title,omitempty"`
	Surface   string `json:"surface,omitempty"`
}

// OpenSession mints an empty room — one that exists before anything has been
// said in it — and is the single door for doing so. It is the capability a
// thread switcher needs: a new conversation is a place first and a transcript
// second, and until this event existed the store could only learn of a room by
// being spoken to in it.
//
// Opening is a mint, not a touch. A room that already exists has already been
// opened, so a second call journals nothing and returns the row as it stands:
// appending a second birthday would put a fact in the journal that is not
// true, and replay would faithfully reproduce it. Callers that want to know
// which happened compare the returned Created against their own clock, or read
// the room first.
//
// The empty id is not a room, for the reason EnsureSession states: messages
// posted to no room in particular carry one, and a nameless row would put a
// phantom thread in every thread list.
func (s *Store) OpenSession(id, title, surface string) (Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Session{}, fmt.Errorf("open session: %w: empty id", ErrInvalid)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Session{}, fmt.Errorf("open session: %w", err)
	}
	defer tx.Rollback()

	// Read and mint in one transaction: two switchers opening the same id must
	// not both decide the room is new.
	existing, err := readSessionTx(tx, id)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Session{}, fmt.Errorf("open session: %w", err)
	}

	payload := sessionOpenedPayload{
		SessionID: id,
		Title:     bounded(title, MaxSessionTitleBytes),
		Surface:   strings.TrimSpace(surface),
	}
	// The room is not a node, so the event carries no node id — the same shape
	// TouchSeen uses for a fact about a lens rather than about the graph.
	_, at, err := appendEvent(tx, "", EventSessionOpened, payload)
	if err != nil {
		return Session{}, fmt.Errorf("open session: %w", err)
	}
	if err := applySessionOpened(tx, payload, at); err != nil {
		return Session{}, fmt.Errorf("open session: %w", err)
	}
	opened, err := readSessionTx(tx, id)
	if err != nil {
		return Session{}, fmt.Errorf("open session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf("open session: %w", err)
	}
	return opened, nil
}

// applySessionOpened is the projection write for a room's birth, and the one
// place a session row's title is written. It is deliberately the same
// never-lower shape ensureSessionTx uses — an open replays among the messages
// in journal order, and a room whose first message beat its open event (an id
// reused across builds) keeps the earlier birthday and the later activity mark
// rather than having either rewritten under it.
func applySessionOpened(tx *sql.Tx, payload sessionOpenedPayload, at time.Time) error {
	id := strings.TrimSpace(payload.SessionID)
	if id == "" {
		return fmt.Errorf("session opened without an id")
	}
	if at.IsZero() {
		at = time.Now()
	}
	stamp := formatTime(at)
	_, err := tx.Exec(`
		INSERT INTO sessions (id, title, surface, created_at, last_active_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			last_active_at = MAX(sessions.last_active_at, excluded.last_active_at),
			created_at     = MIN(sessions.created_at, excluded.created_at),
			title          = CASE WHEN sessions.title = '' THEN excluded.title ELSE sessions.title END,
			surface        = CASE WHEN sessions.surface = '' THEN excluded.surface ELSE sessions.surface END`,
		id, bounded(payload.Title, MaxSessionTitleBytes), strings.TrimSpace(payload.Surface), stamp, stamp)
	return err
}

// sessionRenamedPayload is a room taking a new title on the wire. There is no
// time field for the same reason sessionOpenedPayload has none: the event's
// own journal timestamp is available to any reader that wants "when", and a
// rename does not need it anyway — applySessionRenamed deliberately does not
// touch last_active_at, so the payload carries nothing that could disagree
// with the envelope.
// Tags is ABSENT rather than empty when the rename does not state them, and
// the distinction is the whole of "a person's rename never touches tags": an
// omitted list means leave what is there, and only a rename that carries one
// writes one. A scribe that produced a title and no tags therefore also leaves
// them alone, which is the same answer for the same reason — nothing was said
// about the subject, so nothing about the subject changes.
type sessionRenamedPayload struct {
	SessionID string   `json:"session_id"`
	Title     string   `json:"title"`
	Tags      []string `json:"tags,omitempty"`
}

// RenameSession retitles an existing room. It is the door OpenSession
// deliberately is not: OpenSession refuses to re-title a room that already
// exists, because a second birthday would be a false fact, but a person
// renaming a tab is not claiming the room was just born — they are stating a
// new name for something that already has one. Renaming a room that does not
// exist is refused rather than minting it: a rename names an intent about an
// existing place, and a caller with no room to rename has a bug, not a new
// room to open.
//
// Renaming to the title the room already has journals nothing, the same
// idempotence OpenSession gives a second open: two switchers racing to set
// the identical name must not put two facts in the journal for one true
// state, and a rebuild replaying the single event they agree on must land on
// the same row either way.
//
// The projection write touches only the title. A rename is not activity —
// nobody spoke, nothing happened in the room — so created_at and
// last_active_at are exactly what they were before this call.
func (s *Store) RenameSession(id, title string) (Session, error) {
	return s.renameSession(id, title, nil)
}

// RenameSessionTagged is RenameSession plus the subjects the room is filed
// under. It is the naming pass's door and deliberately not a second verb: a
// title and the tags beside it come out of ONE reading of what the room is
// about, so they are one event and land or fail together.
//
// An empty tag list states nothing about the subject and therefore changes
// nothing about it — see [sessionRenamedPayload].
func (s *Store) RenameSessionTagged(id, title string, tags []string) (Session, error) {
	return s.renameSession(id, title, normalizeSessionTags(tags))
}

func (s *Store) renameSession(id, title string, tags []string) (Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Session{}, fmt.Errorf("rename session: %w: empty id", ErrInvalid)
	}
	title = bounded(title, MaxSessionTitleBytes)

	tx, err := s.db.Begin()
	if err != nil {
		return Session{}, fmt.Errorf("rename session: %w", err)
	}
	defer tx.Rollback()

	existing, err := readSessionTx(tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, fmt.Errorf("rename session: %w: %q", ErrNotFound, id)
	}
	if err != nil {
		return Session{}, fmt.Errorf("rename session: %w", err)
	}
	if existing.Title == title && (len(tags) == 0 || slices.Equal(existing.Tags, tags)) {
		return existing, nil
	}

	payload := sessionRenamedPayload{SessionID: id, Title: title, Tags: tags}
	// The room is not a node, the same shape OpenSession's mint uses.
	_, _, err = appendEvent(tx, "", EventSessionRenamed, payload)
	if err != nil {
		return Session{}, fmt.Errorf("rename session: %w", err)
	}
	if err := applySessionRenamed(tx, payload); err != nil {
		return Session{}, fmt.Errorf("rename session: %w", err)
	}
	renamed, err := readSessionTx(tx, id)
	if err != nil {
		return Session{}, fmt.Errorf("rename session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf("rename session: %w", err)
	}
	return renamed, nil
}

// applySessionRenamed is the projection write for a room's retitling. It
// updates title alone: created_at and last_active_at are untouched, so a
// rename can never be mistaken for the activity that a message or an open
// records. It targets a row that must already exist — Rebuild replays every
// event in journal order, and a rename can only follow the open or the
// message that minted its room — so a missing row here is a corrupt journal
// rather than a case to tolerate quietly.
func applySessionRenamed(tx *sql.Tx, payload sessionRenamedPayload) error {
	id := strings.TrimSpace(payload.SessionID)
	if id == "" {
		return fmt.Errorf("session renamed without an id")
	}
	result, err := tx.Exec(`UPDATE sessions SET title = ? WHERE id = ?`,
		bounded(payload.Title, MaxSessionTitleBytes), id)
	if err != nil {
		return err
	}
	// The tags are a second statement rather than a second column in the one
	// above, because a rename that says nothing about the subject must not
	// write over what the naming pass filed the room under. Normalized again on
	// the way in: replay reads a payload written by an older build, and the
	// row a rebuild lands must be the row the live write landed.
	if tags := normalizeSessionTags(payload.Tags); len(tags) > 0 {
		encoded, err := json.Marshal(tags)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE sessions SET tags = ? WHERE id = ?`, string(encoded), id); err != nil {
			return err
		}
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("session renamed but %q has no row", id)
	}
	return nil
}

// EnsureSession mints the row for a session the first time it is seen and
// raises its activity mark, and is safe to call on every message. An empty id
// is not a session: messages the machine posts to no room in particular carry
// one, and minting a nameless row for them would put a phantom thread in every
// thread list.
//
// Surface is first-writer-wins. The message path knows the session but not the
// lens it is being typed into, so it ensures with an empty surface and a lens
// that names itself later fills it in without overwriting an earlier answer.
func (s *Store) EnsureSession(id, surface string) (Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Session{}, fmt.Errorf("ensure session: %w: empty id", ErrInvalid)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Session{}, fmt.Errorf("ensure session: %w", err)
	}
	defer tx.Rollback()
	if err := ensureSessionTx(tx, id, surface, time.Now()); err != nil {
		return Session{}, fmt.Errorf("ensure session: %w", err)
	}
	session, err := readSessionTx(tx, id)
	if err != nil {
		return Session{}, fmt.Errorf("ensure session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf("ensure session: %w", err)
	}
	return session, nil
}

// TouchSession raises one session's activity mark, minting the row if this is
// the first the store has heard of it. A zero time means now.
func (s *Store) TouchSession(id string, at time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("touch session: %w: empty id", ErrInvalid)
	}
	if at.IsZero() {
		at = time.Now()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	defer tx.Rollback()
	if err := ensureSessionTx(tx, id, "", at); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// Sessions lists every known thread, most recently active first.
func (s *Store) Sessions() ([]Session, error) {
	rows, err := s.db.Query(`
		SELECT id, title, tags, surface, created_at, last_active_at
		FROM sessions ORDER BY last_active_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	sessions := make([]Session, 0)
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

// Session reads one thread's row. The boolean is false when nothing has ever
// been said in that session, which is not an error: a caller holding a session
// id from an older build, or from a lens that has not spoken yet, asks this.
func (s *Store) Session(id string) (Session, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Session{}, false, nil
	}
	session, err := readSession(s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("read session: %w", err)
	}
	return session, true, nil
}

// SessionMessageCursors returns, for every session that has ever been spoken
// in, the sequence of its newest non-user message — the same resume rule
// LastNonUserMessageSeq states for the whole journal, asked one room at a time.
// A session that has only ever heard from the user reports zero.
//
// It is one query rather than a read per session because the head asks it once
// at startup and must not pay a round trip per room to do it. Sessions are
// keyed by their raw id, the empty one included: messages posted to no room in
// particular still have a resume point, and leaving them out of the map would
// silently give them everyone else's.
func (s *Store) SessionMessageCursors() (map[string]int64, error) {
	rows, err := s.db.Query(`
		SELECT session_id, COALESCE(MAX(CASE WHEN role <> ? THEN seq END), 0)
		FROM messages GROUP BY session_id`, string(RoleUser))
	if err != nil {
		return nil, fmt.Errorf("read session cursors: %w", err)
	}
	defer rows.Close()
	cursors := make(map[string]int64)
	for rows.Next() {
		var sessionID string
		var seq int64
		if err := rows.Scan(&sessionID, &seq); err != nil {
			return nil, fmt.Errorf("read session cursors: %w", err)
		}
		cursors[sessionID] = seq
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read session cursors: %w", err)
	}
	return cursors, nil
}

// SessionLastNonUserMessageSeq is LastNonUserMessageSeq scoped to one room.
func (s *Store) SessionLastNonUserMessageSeq(sessionID string) (int64, error) {
	var seq int64
	if err := s.db.QueryRow(
		`SELECT COALESCE(MAX(seq), 0) FROM messages WHERE session_id = ? AND role <> ?`,
		sessionID, string(RoleUser)).Scan(&seq); err != nil {
		return 0, fmt.Errorf("read session resume point: %w", err)
	}
	return seq, nil
}

// backfillSessions gives every session named by an existing message a row.
// Projections are rebuildable from the journal, so an existing database needs
// no upgrade ceremony — it needs this one statement, which is idempotent by
// construction: the anti-join leaves it a no-op once every session has its row,
// and it runs at open rather than on any read path.
func backfillSessions(db *sql.DB) error {
	_, err := db.Exec(`
		INSERT INTO sessions (id, title, tags, surface, created_at, last_active_at)
		SELECT m.session_id, '', '[]', '', MIN(m.ts), MAX(m.ts)
		FROM messages m
		WHERE m.session_id <> ''
		  AND NOT EXISTS (SELECT 1 FROM sessions s WHERE s.id = m.session_id)
		GROUP BY m.session_id`)
	return err
}

// ensureSessionTx is the projection write every message goes through. It mints
// the row on first sight and never lowers what is already there: messages
// replay in journal order during a rebuild, but a live store can commit two of
// them in either order, and an activity mark that could move backwards would
// reorder the thread list under the reader.
func ensureSessionTx(tx *sql.Tx, id, surface string, at time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	stamp := formatTime(at)
	_, err := tx.Exec(`
		INSERT INTO sessions (id, title, surface, created_at, last_active_at)
		VALUES (?, '', ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			last_active_at = MAX(sessions.last_active_at, excluded.last_active_at),
			created_at     = MIN(sessions.created_at, excluded.created_at),
			surface        = CASE WHEN sessions.surface = '' THEN excluded.surface ELSE sessions.surface END`,
		id, strings.TrimSpace(surface), stamp, stamp)
	return err
}

func readSession(db *sql.DB, id string) (Session, error) {
	return scanSession(db.QueryRow(`
		SELECT id, title, tags, surface, created_at, last_active_at FROM sessions WHERE id = ?`, id))
}

func readSessionTx(tx *sql.Tx, id string) (Session, error) {
	return scanSession(tx.QueryRow(`
		SELECT id, title, tags, surface, created_at, last_active_at FROM sessions WHERE id = ?`, id))
}

// scanRow is what a *sql.Row and a *sql.Rows have in common, so one scanner
// serves the single read and the listing.
type scanRow interface {
	Scan(dest ...any) error
}

func scanSession(row scanRow) (Session, error) {
	var session Session
	var created, lastActive, tags string
	if err := row.Scan(&session.ID, &session.Title, &tags, &session.Surface, &created, &lastActive); err != nil {
		return Session{}, err
	}
	// A tag list that will not decode is a room with no tags, not a read that
	// fails. Tags are a finding aid over a working conversation; refusing to
	// hand back the room because its label list is malformed would take the
	// conversation away over an ornament.
	if tags != "" && tags != "[]" && tags != "null" {
		var decoded []string
		if err := json.Unmarshal([]byte(tags), &decoded); err == nil {
			session.Tags = normalizeSessionTags(decoded)
		}
	}
	at, err := parseTime(created)
	if err != nil {
		return Session{}, fmt.Errorf("parse created time: %w", err)
	}
	session.Created = at
	at, err = parseTime(lastActive)
	if err != nil {
		return Session{}, fmt.Errorf("parse last active time: %w", err)
	}
	session.LastActive = at
	return session, nil
}
