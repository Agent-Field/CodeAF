package workstream

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

type Options struct {
	Now   func() time.Time
	NewID func() (string, error)
}
type Store struct {
	workspace *workspace.Store
	now       func() time.Time
	newID     func() (string, error)
}

func New(w *workspace.Store, opts Options) *Store {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.NewID == nil {
		opts.NewID = func() (string, error) {
			var b [16]byte
			if _, err := rand.Read(b[:]); err != nil {
				return "", err
			}
			return hex.EncodeToString(b[:]), nil
		}
	}
	return &Store{workspace: w, now: opts.Now, newID: opts.NewID}
}

func instant(ms int64) time.Time { return time.UnixMilli(ms).UTC() }
func optionalInstant(ms sql.NullInt64) time.Time {
	if !ms.Valid {
		return time.Time{}
	}
	return instant(ms.Int64)
}
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
func invalid(message string) error { return fmt.Errorf("%w: %s", ErrInvalid, message) }
func textWithin(s string, limit int) bool {
	return len(s) <= limit && utf8.ValidString(s) &&
		strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) < 0
}
func lineWithin(s string, limit int) bool {
	return len(s) <= limit && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}
func actor(by string) (string, error) {
	if by == "" {
		by = "you"
	}
	if by != "you" && by != "agent" {
		return "", invalid("actor must be you or agent")
	}
	return by, nil
}
func validTitle(title string) error {
	if strings.TrimSpace(title) == "" || !lineWithin(title, 200) {
		return invalid("title must be 1–200 bytes")
	}
	return nil
}
func sourceJSON(source *Source) (any, error) {
	if source == nil {
		return nil, nil
	}
	if source.ID == "" || !textWithin(source.ID, 4096) || !lineWithin(source.Label, 200) {
		return nil, invalid("source needs an id and a label of at most 200 bytes")
	}
	switch source.Kind {
	case "chat", "page", "url":
	case "message":
		if source.ChatID == "" {
			return nil, invalid("message source needs chat_id")
		}
	case "factory_job":
		if err := (workspace.Ref{Kind: workspace.FactoryJobKind, ID: source.ID, Factory: source.Factory}).Validate(); err != nil {
			return nil, err
		}
	default:
		return nil, invalid("unknown source kind")
	}
	b, err := json.Marshal(source)
	return string(b), err
}
func readSource(raw sql.NullString) (*Source, error) {
	if !raw.Valid {
		return nil, nil
	}
	var s Source
	err := json.Unmarshal([]byte(raw.String), &s)
	return &s, err
}
func found(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func touch(ctx context.Context, tx *sql.Tx, id string, at int64) error {
	_, err := tx.ExecContext(ctx, "UPDATE workstreams SET updated_at=max(updated_at+1,?) WHERE id=?", at, id)
	return err
}
func changed(id string, part ChangedPart) Change {
	c := Change{ID: id, Parts: []ChangedPart{PartWorkstream}}
	if part != PartWorkstream {
		c.Parts = append(c.Parts, part)
	}
	return c
}

const workstreamSelect = `SELECT w.id,c.name,w.why,w.origin_chat_id,w.agent_id,w.autonomy,w.wake_morning,w.wake_related_issues,
 w.budget_usd,w.created_at,w.updated_at,w.started_at,w.done_at,w.how_it_went,w.client_id,
 (SELECT count(*) FROM workstream_records r WHERE r.workstream_id=w.id AND r.state='proposed')
 FROM workstreams w JOIN collections c ON c.id=w.id WHERE w.id=?`

func getIn(ctx context.Context, tx *sql.Tx, id string) (Workstream, error) {
	var w Workstream
	var created, updated int64
	var start, done sql.NullInt64
	var budget sql.NullFloat64
	err := tx.QueryRowContext(ctx, workstreamSelect, id).Scan(&w.ID, &w.Name, &w.Why, &w.OriginChatID, &w.AgentID, &w.Autonomy, &w.Wake.Morning, &w.Wake.RelatedIssues, &budget, &created, &updated, &start, &done, &w.HowItWent, &w.ClientID, &w.Proposed)
	if err != nil {
		return w, found(err)
	}
	w.CreatedAt, w.UpdatedAt, w.StartedAt, w.DoneAt = instant(created), instant(updated), optionalInstant(start), optionalInstant(done)
	if budget.Valid {
		w.BudgetUSD = &budget.Float64
	}
	w.Stage = Stage(w.DoneAt, w.StartedAt, nil)
	rows, err := tx.QueryContext(ctx, "SELECT kind,ref_id,session_id,factory FROM memberships WHERE collection_id=? ORDER BY seq", id)
	if err != nil {
		return w, err
	}
	defer rows.Close()
	w.Members = make([]workspace.Ref, 0)
	for rows.Next() {
		var ref workspace.Ref
		if err := rows.Scan(&ref.Kind, &ref.ID, &ref.SessionID, &ref.Factory); err != nil {
			return w, err
		}
		w.Members = append(w.Members, ref)
		if ref.Kind == workspace.FactoryJobKind {
			w.Total++
		}
	}
	return w, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (Workstream, error) {
	var out Workstream
	err := s.workspace.Read(ctx, func(tx *sql.Tx, version int) error {
		if version < 3 {
			return ErrNotFound
		}
		var err error
		out, err = getIn(ctx, tx, id)
		return err
	})
	return out, err
}

// ByClientID lets an adapter recognize a retry before creating its agent.
// Create checks it again inside the writer transaction to settle races.
func (s *Store) ByClientID(ctx context.Context, clientID string) (Workstream, error) {
	var out Workstream
	if clientID == "" {
		return out, ErrNotFound
	}
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < 3 {
			return ErrNotFound
		}
		var id string
		if err := tx.QueryRowContext(ctx, "SELECT id FROM workstreams WHERE client_id=?", clientID).Scan(&id); err != nil {
			return found(err)
		}
		var err error
		out, err = getIn(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Store) List(ctx context.Context) ([]Workstream, error) {
	out := make([]Workstream, 0)
	err := s.workspace.Read(ctx, func(tx *sql.Tx, version int) error {
		if version < 3 {
			return nil
		}
		rows, err := tx.QueryContext(ctx, "SELECT id FROM workstreams ORDER BY updated_at DESC,id")
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, id := range ids {
			w, err := getIn(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, w)
		}
		return nil
	})
	return out, err
}

func (s *Store) Create(ctx context.Context, p Create) (Workstream, Change, error) {
	p.Name = strings.TrimSpace(p.Name)
	if err := workspace.ValidateName(p.Name); err != nil {
		return Workstream{}, Change{}, err
	}
	if !textWithin(p.Why, 4000) || !lineWithin(p.ClientID, 128) || len(p.Refs) > 200 {
		return Workstream{}, Change{}, invalid("create exceeds workstream bounds")
	}
	refs := append([]workspace.Ref(nil), p.Refs...)
	refs = append(refs, workspace.Ref{Kind: workspace.ConversationKind, ID: p.AgentID})
	if p.OriginChatID != "" {
		refs = append(refs, workspace.Ref{Kind: workspace.ConversationKind, ID: p.OriginChatID})
	}
	additions := make([]workspace.Addition, 0, len(refs))
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return Workstream{}, Change{}, err
		}
		additions = append(additions, workspace.Addition{Ref: ref})
	}
	var out Workstream
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		if p.ClientID != "" {
			var id string
			err := tx.QueryRowContext(ctx, "SELECT id FROM workstreams WHERE client_id=?", p.ClientID).Scan(&id)
			if err == nil {
				out, err = getIn(ctx, tx, id)
				return err
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		id, err := s.newID()
		if err != nil {
			return err
		}
		at := s.now().UnixMilli()
		if err := workspace.CreateIn(ctx, tx, id, p.Name, at); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workstreams(id,why,origin_chat_id,agent_id,created_at,updated_at,client_id) VALUES (?,?,?,?,?,?,?)`, id, p.Why, p.OriginChatID, p.AgentID, at, at, p.ClientID); err != nil {
			return err
		}
		if _, _, err := workspace.AddRefsIn(ctx, tx, id, additions, at); err != nil {
			return err
		}
		pageID, err := s.newID()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workstream_pages(id,workstream_id,title,owner,position,version,updated_at,updated_by,doc,overview) VALUES (?,?,'Overview','you',0,1,?,'you',?,1)`, pageID, id, at, emptyDoc); err != nil {
			return err
		}
		if err := saveVersion(ctx, tx, pageID); err != nil {
			return err
		}
		out, err = getIn(ctx, tx, id)
		c = Change{ID: id, Parts: []ChangedPart{PartWorkstream, PartPages, PartJobs}}
		return err
	})
	if err != nil {
		return Workstream{}, Change{}, err
	}
	return out, c, nil
}

func (s *Store) Update(ctx context.Context, id string, p Patch) (Workstream, Change, error) {
	if p.Name != nil {
		name := strings.TrimSpace(*p.Name)
		p.Name = &name
		if err := workspace.ValidateName(name); err != nil {
			return Workstream{}, Change{}, err
		}
	}
	if p.Why != nil && !textWithin(*p.Why, 4000) {
		return Workstream{}, Change{}, invalid("why exceeds 4000 bytes")
	}
	if p.Autonomy != nil && *p.Autonomy != "propose" && *p.Autonomy != "small" && *p.Autonomy != "off" {
		return Workstream{}, Change{}, invalid("unknown autonomy")
	}
	if p.BudgetUSD != nil && (math.IsNaN(*p.BudgetUSD) || math.IsInf(*p.BudgetUSD, 0) || *p.BudgetUSD < 0) {
		return Workstream{}, Change{}, invalid("budget must be finite and non-negative")
	}
	var out Workstream
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		w, err := getIn(ctx, tx, id)
		if err != nil {
			return err
		}
		before := w
		if p.Name != nil {
			w.Name = *p.Name
		}
		if p.Why != nil {
			w.Why = *p.Why
		}
		if p.Autonomy != nil {
			w.Autonomy = *p.Autonomy
		}
		if p.Wake != nil {
			w.Wake = *p.Wake
		}
		if p.BudgetUSD != nil {
			w.BudgetUSD = p.BudgetUSD
			if *p.BudgetUSD == 0 {
				w.BudgetUSD = nil
			}
		}
		sameBudget := (w.BudgetUSD == nil && before.BudgetUSD == nil) || (w.BudgetUSD != nil && before.BudgetUSD != nil && *w.BudgetUSD == *before.BudgetUSD)
		if w.Name == before.Name && w.Why == before.Why && w.Autonomy == before.Autonomy && w.Wake == before.Wake && sameBudget {
			out = w
			return nil
		}
		if w.Name != before.Name {
			if err := workspace.RenameIn(ctx, tx, id, w.Name); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workstreams SET why=?,autonomy=?,wake_morning=?,wake_related_issues=?,budget_usd=?,updated_at=max(updated_at+1,?) WHERE id=?`, w.Why, w.Autonomy, w.Wake.Morning, w.Wake.RelatedIssues, w.BudgetUSD, s.now().UnixMilli(), id); err != nil {
			return err
		}
		out, err = getIn(ctx, tx, id)
		c = changed(id, PartWorkstream)
		return err
	})
	if err != nil {
		return Workstream{}, Change{}, err
	}
	return out, c, nil
}

// Stage follows the wire rule; dropped and unknown states do not start a workstream.
func Stage(done, started time.Time, states []string) string {
	if !done.IsZero() {
		return "done"
	}
	if !started.IsZero() {
		return "building"
	}
	for _, state := range states {
		switch state {
		case "queued", "running", "needs_you", "done":
			return "building"
		}
	}
	return "shaping"
}

// Jobs returns direct factory job members in collection order.
func (w Workstream) Jobs() []Job {
	out := make([]Job, 0, w.Total)
	for _, ref := range w.Members {
		if ref.Kind == workspace.FactoryJobKind {
			n, _ := strconv.Atoi(ref.ID)
			out = append(out, Job{Factory: ref.Factory, Number: n})
		}
	}
	return out
}
func (s *Store) ObserveJobStates(ctx context.Context, id string, states map[Job]string) (Change, error) {
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		w, err := getIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if !w.StartedAt.IsZero() {
			return nil
		}
		memberStates := make([]string, 0, w.Total)
		for _, job := range w.Jobs() {
			memberStates = append(memberStates, states[job])
		}
		if Stage(time.Time{}, time.Time{}, memberStates) != "building" {
			return nil
		}
		at := s.now().UnixMilli()
		_, err = tx.ExecContext(ctx, "UPDATE workstreams SET started_at=?,updated_at=max(updated_at+1,?) WHERE id=?", at, at, id)
		if err == nil {
			c = changed(id, PartWorkstream)
		}
		return err
	})
	if err != nil {
		return Change{}, err
	}
	return c, nil
}
func (s *Store) MarkDone(ctx context.Context, id, how string) (Workstream, Change, error) {
	if !textWithin(how, 4000) {
		return Workstream{}, Change{}, invalid("how_it_went exceeds 4000 bytes")
	}
	return s.setDone(ctx, id, how, true)
}
func (s *Store) Reopen(ctx context.Context, id string) (Workstream, Change, error) {
	return s.setDone(ctx, id, "", false)
}
func (s *Store) setDone(ctx context.Context, id, how string, done bool) (Workstream, Change, error) {
	var out Workstream
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		w, err := getIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if done == !w.DoneAt.IsZero() {
			out = w
			return nil
		}
		at := s.now().UnixMilli()
		if done {
			if _, err := tx.ExecContext(ctx, "UPDATE workstreams SET done_at=?,how_it_went=?,updated_at=max(updated_at+1,?) WHERE id=?", at, how, at, id); err != nil {
				return err
			}
			result, err := tx.ExecContext(ctx, `UPDATE workstream_goaheads SET state='ended',
 end_reason=CASE WHEN ends_at<=? THEN 'expired' ELSE 'workstream_done' END,
 ended_at=CASE WHEN ends_at<=? THEN ends_at ELSE ? END
 WHERE workstream_id=? AND state='active'`, at, at, at, id)
			if err != nil {
				return err
			}
			c = changed(id, PartWorkstream)
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n > 0 {
				c.Parts = append(c.Parts, PartGoAheads)
			}
		} else {
			if _, err := tx.ExecContext(ctx, "UPDATE workstreams SET done_at=NULL,started_at=coalesce(started_at,?),updated_at=max(updated_at+1,?) WHERE id=?", at, at, id); err != nil {
				return err
			}
			c = changed(id, PartWorkstream)
		}
		out, err = getIn(ctx, tx, id)
		return err
	})
	if err != nil {
		return Workstream{}, Change{}, err
	}
	return out, c, nil
}

// CountNumbers retains measured zeroes and withholds any total needing unknown
// facts. Job state must be known even for spend, as required by the contract.
func CountNumbers(jobs []Job, facts map[Job]JobFacts, agentSpend *float64, proposed int) Numbers {
	out := Numbers{Total: len(jobs)}
	merged, needs, spend := 0, proposed, 0.0
	mergedKnown, statesKnown, costKnown := true, true, true
	for _, job := range jobs {
		f, ok := facts[job]
		known := ok && (f.State == "backlog" || f.State == "queued" || f.State == "running" || f.State == "needs_you" || f.State == "done" || f.State == "dropped")
		if !known {
			statesKnown = false
		}
		if !known || f.Merged == nil {
			mergedKnown = false
		} else if *f.Merged {
			merged++
		}
		if f.State == "needs_you" {
			needs++
		}
		if !known || f.CostUSD == nil || math.IsNaN(*f.CostUSD) || math.IsInf(*f.CostUSD, 0) || *f.CostUSD < 0 {
			costKnown = false
		} else {
			spend += *f.CostUSD
		}
	}
	if mergedKnown {
		out.Merged = &merged
	}
	if statesKnown {
		out.NeedsYou = &needs
	}
	if costKnown && agentSpend != nil && !math.IsNaN(*agentSpend) && !math.IsInf(*agentSpend, 0) && *agentSpend >= 0 {
		spend += *agentSpend
		if !math.IsInf(spend, 0) {
			out.SpendUSD = &spend
		}
	}
	return out
}
