package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A CONTEXT RECORD IS INFORMATION, NEVER AN INSTRUCTION. It says that a finding
// or a decision was written down, where it came from, and where its author
// explicitly said it applies. It grants no authority, adopts no goal, claims no
// acceptance by the person, and does not make its targets read it. A surface or
// a prompt assembler decides what to do with one; storage only keeps it honest.
//
// Its identity is stable across revisions so the same record can be reached from
// several places without becoming inconsistent copies. Revisions are immutable:
// a revision that has been written is never edited, and the record's current
// revision is a pointer that moves. That is what lets a reader load a revision
// in two queries and still see one consistent record while a peer revises it.
type ContextRecord struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Source    Ref    `json:"source"`
	Revision  int    `json:"revision"`
	Withdrawn bool   `json:"withdrawn"`
	Targets   []Ref  `json:"targets"`
}

// ContextSelection is one page of records together with the two things a caller
// cannot work out from the page itself: whether the store had more to give, and
// whether this place USED to have context that no longer applies to it. The
// second is not a record and never becomes one — it is the difference between
// "nothing was ever written here" and "what was written here was withdrawn or
// pointed somewhere else", which are the same empty page to a reader who is only
// shown the records.
type ContextSelection struct {
	Records           []ContextRecord `json:"records"`
	More              bool            `json:"more"`
	PreviouslyApplied bool            `json:"previously_applied"`
}

// THE BOUNDS ARE HERE AND NOWHERE ELSE, because a limit that is written twice is
// a limit that drifts. The text bound is a prompt-cost bound as much as a storage
// one: a later assembler must be able to reason about the worst case it can be
// handed, so a finding is a paragraph or a page, not a transcript.
const (
	maxContextTitle   = 256
	maxContextText    = 65536
	maxContextTargets = 64

	// MaxContextTargets is the target bound spelled for callers who have to stay
	// inside it. It is the same number, not a second one.
	MaxContextTargets = maxContextTargets
	// MaxContextPage is the most records one page carries. A caller asking for
	// more is given this many rather than refused, so a surface that guesses too
	// high still gets an answer it can draw.
	MaxContextPage = 50
)

// ErrConflict reports that the record moved under an editor who was working from
// an older revision. The write is refused whole; nothing is merged and nothing
// is overwritten, so the caller can re-read and decide.
var ErrConflict = errors.New("context was revised by someone else")

// querier is the read surface shared by the database handle and a transaction,
// so a load looks the same inside and outside a write.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// CreateContext records a new finding or decision. The source is where it came
// from; the targets are the only places it applies. Neither is a claim that the
// record has been accepted, and an empty target list is a legitimate record that
// simply is not reachable by applicability yet.
func (s *Store) CreateContext(ctx context.Context, title, text string, source Ref, targets []Ref) (ContextRecord, error) {
	targets, err := validateContext(title, text, source, targets)
	if err != nil {
		return ContextRecord{}, err
	}
	id, err := newID()
	if err != nil {
		return ContextRecord{}, err
	}
	record := ContextRecord{ID: id, Title: title, Text: text, Source: source, Revision: 1, Targets: targets}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ContextRecord{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO contexts(id,revision) VALUES (?,?)", record.ID, record.Revision); err != nil {
		return ContextRecord{}, err
	}
	if err := writeRevision(ctx, tx, record); err != nil {
		return ContextRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ContextRecord{}, err
	}
	return record, nil
}

// ReviseContext replaces the wording, the source and the whole target set in one
// atomic step, refusing an editor whose expected revision is no longer current.
// Revising a withdrawn record makes it current again; that is the only way back,
// and it is deliberately an explicit edit rather than a silent reinstatement.
func (s *Store) ReviseContext(ctx context.Context, id string, expectedRevision int, title, text string, source Ref, targets []Ref) (ContextRecord, error) {
	targets, err := validateContext(title, text, source, targets)
	if err != nil {
		return ContextRecord{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ContextRecord{}, err
	}
	defer tx.Rollback()
	current, err := currentRevision(ctx, tx, id)
	if err != nil {
		return ContextRecord{}, err
	}
	if current != expectedRevision {
		return ContextRecord{}, fmt.Errorf("%w: expected revision %d, current revision is %d", ErrConflict, expectedRevision, current)
	}
	record := ContextRecord{ID: id, Title: title, Text: text, Source: source, Revision: current + 1, Targets: targets}
	if err := writeRevision(ctx, tx, record); err != nil {
		return ContextRecord{}, err
	}
	if err := movePointer(ctx, tx, record.ID, record.Revision); err != nil {
		return ContextRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ContextRecord{}, err
	}
	return record, nil
}

// WithdrawContext keeps the record and its history and stops presenting it as
// current. The withdrawal is itself a revision carrying the wording it withdrew,
// so a reader of the history can still see what was once said. Withdrawing an
// already withdrawn record changes nothing rather than piling up empty revisions.
func (s *Store) WithdrawContext(ctx context.Context, id string, expectedRevision int) (ContextRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ContextRecord{}, err
	}
	defer tx.Rollback()
	current, err := currentContext(ctx, tx, id)
	if err != nil {
		return ContextRecord{}, err
	}
	if current.Revision != expectedRevision {
		return ContextRecord{}, fmt.Errorf("%w: expected revision %d, current revision is %d", ErrConflict, expectedRevision, current.Revision)
	}
	if current.Withdrawn {
		return current, nil
	}
	record := current
	record.Revision = current.Revision + 1
	record.Withdrawn = true
	if err := writeRevision(ctx, tx, record); err != nil {
		return ContextRecord{}, err
	}
	if err := movePointer(ctx, tx, record.ID, record.Revision); err != nil {
		return ContextRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ContextRecord{}, err
	}
	return record, nil
}

// Context returns the current revision, withdrawn or not. A caller asking for a
// record by its identity is entitled to learn that it was withdrawn.
func (s *Store) Context(ctx context.Context, id string) (ContextRecord, error) {
	return currentContext(ctx, s.db, id)
}

// ContextHistory returns every revision, oldest first, each with the source that
// revision named. Provenance is per revision because a revision can correct
// where a finding actually came from.
func (s *Store) ContextHistory(ctx context.Context, id string) ([]ContextRecord, error) {
	matches, err := s.matchingRevisions(ctx, historyQuery, []any{id})
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, ErrNotFound
	}
	return s.loadAll(ctx, matches)
}

// ContextHistoryPage returns one page of the same history, still oldest first, so
// a long-lived record can be read without loading every wording it ever had. THE
// PAGE IS TAKEN IN SQL BEFORE ANY REVISION IS LOADED, so asking for the last ten
// of a hundred revisions reads ten. An identity that does not exist is
// ErrNotFound; an offset past the end of a record that does exist is an empty
// page, because "there is no such record" and "you have read them all" are
// different answers.
func (s *Store) ContextHistoryPage(ctx context.Context, id string, offset, limit int) (ContextSelection, error) {
	offset, limit, err := boundPage(offset, limit)
	if err != nil {
		return ContextSelection{}, err
	}
	// The existence question is asked separately and first, because an empty page
	// is the correct answer to a large offset and must not be reported as absence.
	if _, err := currentRevision(ctx, s.db, id); err != nil {
		return ContextSelection{}, err
	}
	matches, err := s.matchingRevisions(ctx, historyQuery+" LIMIT ? OFFSET ?", []any{id, limit + 1, offset})
	if err != nil {
		return ContextSelection{}, err
	}
	return s.selection(ctx, matches, limit)
}

// ContextAt returns one exact revision without reading the ones around it, which
// is what a reader following a citation needs: the wording as it stood, not the
// wording as it stands. Revision 0 means the current revision, withdrawn or not.
func (s *Store) ContextAt(ctx context.Context, id string, revision int) (ContextRecord, error) {
	if revision < 0 {
		return ContextRecord{}, fmt.Errorf("%w: a revision number is positive, or 0 for the current revision", ErrInvalid)
	}
	if revision == 0 {
		return currentContext(ctx, s.db, id)
	}
	return loadContext(ctx, s.db, id, revision)
}

const historyQuery = "SELECT context_id,revision FROM context_revisions WHERE context_id=? ORDER BY revision"

// ContextFor answers with the records whose current revision names one of these
// targets EXPLICITLY AND DIRECTLY. There is no folder inheritance, no similarity,
// no transitive reach through a parent collection, and no authorization: asking
// about a collection does not return the context of the chats inside it. Records
// are deduplicated by identity and ordered by when they were created, so the
// order does not depend on which target matched or on the order asked about.
func (s *Store) ContextFor(ctx context.Context, targets []Ref) ([]ContextRecord, error) {
	targets, err := askedRefs(targets)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return make([]ContextRecord, 0), nil
	}
	scope, args := contextScope("d", "c.id", "c.revision", targets, false)
	matches, err := s.matchingRevisions(ctx, currentQuery(scope), args)
	if err != nil {
		return nil, err
	}
	return s.loadAll(ctx, matches)
}

// ContextPage answers the same question one page at a time, and can widen the
// reach by ONE EXPLICIT HOP: with includeCollections, a record filed against a
// folder is also returned for a ref that is a member of that folder right now.
// That hop is not inheritance and not authority. It does not recurse — a record
// on a grandparent folder does not reach a chat two levels down — it consults
// membership as it stands rather than as it stood, and it still grants nothing.
// A caller who wants a task's own conversation considered says so by supplying
// that conversation as another ref; this door does not invent refs for anyone.
//
// The paging is done in SQL over identities and revision numbers, so a page of
// ten out of a thousand records reads ten bodies. More says the store had at
// least one more; PreviouslyApplied says something used to apply in this same
// scope and no longer does.
func (s *Store) ContextPage(ctx context.Context, targets []Ref, includeCollections bool, offset, limit int) (ContextSelection, error) {
	targets, err := askedRefs(targets)
	if err != nil {
		return ContextSelection{}, err
	}
	if offset, limit, err = boundPage(offset, limit); err != nil {
		return ContextSelection{}, err
	}
	if len(targets) == 0 {
		return ContextSelection{Records: make([]ContextRecord, 0)}, nil
	}
	scope, args := contextScope("d", "c.id", "c.revision", targets, includeCollections)
	matches, err := s.matchingRevisions(ctx, currentQuery(scope)+" LIMIT ? OFFSET ?", append(args, limit+1, offset))
	if err != nil {
		return ContextSelection{}, err
	}
	page, err := s.selection(ctx, matches, limit)
	if err != nil {
		return ContextSelection{}, err
	}
	if page.PreviouslyApplied, err = s.previouslyApplied(ctx, targets, includeCollections); err != nil {
		return ContextSelection{}, err
	}
	return page, nil
}

// ContextApplies reports whether this exact revision is current, not withdrawn,
// and applicable in the requested scope. An identity read can still return an
// older or unrelated record; existence alone must never imply applicability.
func (s *Store) ContextApplies(ctx context.Context, id string, revision int, targets []Ref, includeCollections bool) (bool, error) {
	targets, err := askedRefs(targets)
	if err != nil {
		return false, err
	}
	if len(targets) == 0 {
		return false, nil
	}
	scope, args := contextScope("d", "c.id", "c.revision", targets, includeCollections)
	var applies bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM contexts c
 JOIN context_revisions r ON r.context_id=c.id AND r.revision=c.revision
 WHERE c.id=? AND c.revision=? AND r.withdrawn=0 AND `+scope+`)`, append([]any{id, revision}, args...)...).Scan(&applies)
	return applies, err
}

// previouslyApplied reports that some record once applied in this scope and does
// not now. It is deliberately narrow: a record that still applies here does not
// count merely because it has older revisions, or every revised record would
// look like a loss. What it catches is the withdrawal and the retarget-away.
func (s *Store) previouslyApplied(ctx context.Context, targets []Ref, includeCollections bool) (bool, error) {
	past, pastArgs := contextScope("h", "r.context_id", "r.revision", targets, includeCollections)
	present, presentArgs := contextScope("d", "c.id", "c.revision", targets, includeCollections)
	var applied bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM context_revisions r WHERE `+past+`
 AND NOT EXISTS (SELECT 1 FROM contexts c JOIN context_revisions n ON n.context_id=c.id AND n.revision=c.revision
  WHERE c.id=r.context_id AND n.withdrawn=0 AND `+present+`))`, append(pastArgs, presentArgs...)...).Scan(&applied)
	return applied, err
}

// currentQuery selects the identity and current revision of every record that is
// current, not withdrawn, and inside the given scope, oldest first. Ordering by
// creation keeps the answer independent of which target matched and of the order
// the caller asked in, which is also what makes an offset mean the same thing
// twice.
func currentQuery(scope string) string {
	return `SELECT c.id,c.revision FROM contexts c
 JOIN context_revisions r ON r.context_id=c.id AND r.revision=c.revision
 WHERE r.withdrawn=0 AND ` + scope + ` ORDER BY c.seq`
}

// contextScope is THE ONE DEFINITION OF "applies here", used by identity-read applicability, the page, the
// previously-applied question and ContextFor, so these paths cannot come to
// disagree about what a scope means. It is a predicate over an already-chosen
// (context, revision) pair rather than a join, so a record is never returned
// twice for matching two ways.
//
// The membership half joins the supplied refs to the memberships table instead of
// reading a folder's members out and asking about each one: a chat can sit in a
// hundred folders and a folder can hold thousands of chats, and neither number
// may reach the caller's bound on how many places it asked about.
func contextScope(alias, idExpr, revisionExpr string, targets []Ref, includeCollections bool) (string, []any) {
	direct := make([]string, 0, len(targets))
	args := make([]any, 0, len(targets)*6)
	for _, target := range targets {
		direct = append(direct, "("+alias+".kind=? AND "+alias+".ref_id=? AND "+alias+".session_id=?)")
		args = append(args, string(target.Kind), target.ID, target.SessionID)
	}
	where := " WHERE " + alias + ".context_id=" + idExpr + " AND " + alias + ".revision=" + revisionExpr + " AND "
	scope := "(EXISTS (SELECT 1 FROM context_targets " + alias + where + "(" + strings.Join(direct, " OR ") + "))"
	if includeCollections {
		member := alias + "m"
		through := make([]string, 0, len(targets))
		for _, target := range targets {
			through = append(through, "("+member+".kind=? AND "+member+".ref_id=? AND "+member+".session_id=?)")
			args = append(args, string(target.Kind), target.ID, target.SessionID)
		}
		scope += " OR EXISTS (SELECT 1 FROM context_targets " + alias +
			" JOIN memberships " + member + " ON " + member + ".collection_id=" + alias + ".ref_id" +
			where + alias + ".kind='collection' AND (" + strings.Join(through, " OR ") + "))"
	}
	return scope + ")", args
}

// matchingRevisions reads the identities and revision numbers a query chose,
// and nothing else. THE ROWS ARE CLOSED BEFORE ANY BODY IS LOADED: this store
// keeps one connection, so loading inside the iteration would wait on the
// connection the iteration is holding.
func (s *Store) matchingRevisions(ctx context.Context, query string, args []any) ([]contextMatch, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var matches []contextMatch
	for rows.Next() {
		var match contextMatch
		if err := rows.Scan(&match.id, &match.revision); err != nil {
			rows.Close()
			return nil, err
		}
		matches = append(matches, match)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

type contextMatch struct {
	id       string
	revision int
}

func (s *Store) loadAll(ctx context.Context, matches []contextMatch) ([]ContextRecord, error) {
	records := make([]ContextRecord, 0, len(matches))
	for _, match := range matches {
		// The revision was chosen above and is loaded by number rather than through
		// the pointer, so a peer revising this record cannot tear the answer.
		record, err := loadContext(ctx, s.db, match.id, match.revision)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// selection turns one row more than was asked for into the honest "there is
// more" flag, and loads only the rows the caller asked for.
func (s *Store) selection(ctx context.Context, matches []contextMatch, limit int) (ContextSelection, error) {
	result := ContextSelection{}
	if len(matches) > limit {
		result.More = true
		matches = matches[:limit]
	}
	records, err := s.loadAll(ctx, matches)
	if err != nil {
		return ContextSelection{}, err
	}
	result.Records = records
	return result, nil
}

// boundPage keeps a page request inside the bound callers can rely on. A limit of
// zero means "whatever a page holds", and a larger one is brought down to it
// rather than refused, because a caller asking for more than a page is asking for
// a page. A negative offset or limit is a caller's arithmetic error and is said
// so rather than quietly reinterpreted.
func boundPage(offset, limit int) (int, int, error) {
	if offset < 0 || limit < 0 {
		return 0, 0, fmt.Errorf("%w: a page starts at a non-negative offset and asks for a non-negative count", ErrInvalid)
	}
	if limit == 0 || limit > MaxContextPage {
		limit = MaxContextPage
	}
	return offset, limit, nil
}

// askedRefs validates and deduplicates the places a caller is asking about, and
// applies the bound only afterwards. DEDUPLICATION COMES FIRST because a place
// named twice is one place, and letting a repeat spend the budget would refuse a
// legitimate question about far fewer places than the bound allows.
func askedRefs(targets []Ref) ([]Ref, error) {
	kept, err := dedupRefs(targets)
	if err != nil {
		return nil, err
	}
	if len(kept) > maxContextTargets {
		return nil, fmt.Errorf("%w: ask about at most %d targets at once", ErrInvalid, MaxContextTargets)
	}
	return kept, nil
}

// dedupRefs validates every reference and collapses repeats, keeping the place
// each one's first mention had.
func dedupRefs(refs []Ref) ([]Ref, error) {
	kept := make([]Ref, 0, len(refs))
	seen := make(map[Ref]bool, len(refs))
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return nil, err
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		kept = append(kept, ref)
	}
	return kept, nil
}

func writeRevision(ctx context.Context, tx *sql.Tx, record ContextRecord) error {
	withdrawn := 0
	if record.Withdrawn {
		withdrawn = 1
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO context_revisions(context_id,revision,title,text,source_kind,source_id,source_session,withdrawn)
 VALUES (?,?,?,?,?,?,?,?)`, record.ID, record.Revision, record.Title, record.Text,
		string(record.Source.Kind), record.Source.ID, record.Source.SessionID, withdrawn)
	if err != nil {
		return err
	}
	for position, target := range record.Targets {
		// A collection target must exist in this store, because this store owns
		// collections and can say so. Every other target belongs to another owner
		// and may be offline, archived or not yet created; an unavailable record
		// is still a legitimate thing to have written something about.
		if target.Kind == CollectionKind {
			if err := requireCollection(ctx, tx, target.ID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO context_targets(context_id,revision,position,kind,ref_id,session_id)
 VALUES (?,?,?,?,?,?)`, record.ID, record.Revision, position, string(target.Kind), target.ID, target.SessionID)
		if err != nil {
			return err
		}
	}
	return nil
}

func movePointer(ctx context.Context, tx *sql.Tx, id string, revision int) error {
	result, err := tx.ExecContext(ctx, "UPDATE contexts SET revision=? WHERE id=?", revision, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func currentRevision(ctx context.Context, q querier, id string) (int, error) {
	var revision int
	err := q.QueryRowContext(ctx, "SELECT revision FROM contexts WHERE id=?", id).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return revision, err
}

func currentContext(ctx context.Context, q querier, id string) (ContextRecord, error) {
	revision, err := currentRevision(ctx, q, id)
	if err != nil {
		return ContextRecord{}, err
	}
	return loadContext(ctx, q, id, revision)
}

func loadContext(ctx context.Context, q querier, id string, revision int) (ContextRecord, error) {
	record := ContextRecord{ID: id, Revision: revision}
	err := q.QueryRowContext(ctx, `SELECT title,text,source_kind,source_id,source_session,withdrawn
 FROM context_revisions WHERE context_id=? AND revision=?`, id, revision).
		Scan(&record.Title, &record.Text, &record.Source.Kind, &record.Source.ID, &record.Source.SessionID, &record.Withdrawn)
	if errors.Is(err, sql.ErrNoRows) {
		return ContextRecord{}, ErrNotFound
	}
	if err != nil {
		return ContextRecord{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT kind,ref_id,session_id FROM context_targets
 WHERE context_id=? AND revision=? ORDER BY position`, id, revision)
	if err != nil {
		return ContextRecord{}, err
	}
	defer rows.Close()
	record.Targets = make([]Ref, 0)
	for rows.Next() {
		var target Ref
		if err := rows.Scan(&target.Kind, &target.ID, &target.SessionID); err != nil {
			return ContextRecord{}, err
		}
		record.Targets = append(record.Targets, target)
	}
	if err := rows.Err(); err != nil {
		return ContextRecord{}, err
	}
	return record, nil
}

func validateContext(title, text string, source Ref, targets []Ref) ([]Ref, error) {
	if !validText(title, maxContextTitle) {
		return nil, fmt.Errorf("%w: a title is 1–%d bytes without surrounding whitespace or control characters", ErrInvalid, maxContextTitle)
	}
	if !validProse(text, maxContextText) {
		return nil, fmt.Errorf("%w: text is 1–%d bytes of valid UTF-8 without surrounding whitespace or control characters other than a line break or a tab", ErrInvalid, maxContextText)
	}
	if err := source.Validate(); err != nil {
		return nil, err
	}
	// A FOLDER AND A STANDING ITEM ARE NOT SOURCES. Organization is not provenance,
	// and an instruction that already has its own owner must not be laundered into
	// an informational record that claims to have come from it.
	switch source.Kind {
	case ConversationKind, TaskKind, ArtifactKind:
	default:
		return nil, fmt.Errorf("%w: a context comes from a conversation, a task or an artifact, not from a %s", ErrInvalid, source.Kind)
	}
	// A repeated target is the same applicability stated twice, so it collapses
	// rather than being refused; the first mention keeps its place in the order.
	// The bound is counted after that, on the places the record actually reaches.
	kept, err := dedupRefs(targets)
	if err != nil {
		return nil, err
	}
	if len(kept) > maxContextTargets {
		return nil, fmt.Errorf("%w: a context applies to at most %d targets", ErrInvalid, MaxContextTargets)
	}
	return kept, nil
}

// validProse accepts the body of a finding, which is prose and therefore keeps
// its line breaks and tabs. Every other control character is refused, so a
// record cannot carry terminal escapes into a surface that prints it.
func validProse(s string, limit int) bool {
	return s != "" && len(s) <= limit && utf8.ValidString(s) && strings.TrimSpace(s) == s &&
		strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) < 0
}
