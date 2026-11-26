package data

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// APPS ARE FOR DOING WORK. Beside the records, the space keeps four tables
// that belong to no one app:
//
//   - work items: something someone (the person, or an agent) is to do, about
//     a record or not, with a due date, an SLA, the form that completes it
//     and its outcome. One "My work" list reads them across every app.
//   - timers: a moment a record is waiting for (a follow-up in three days, an
//     SLA breach, a monthly close), fired into the outbox when it comes, and
//     caught up when the store opens after being closed.
//   - sign-offs: a statement signed against one version of a record. A change
//     to the record makes a new version the sign-off does not cover. Only the
//     person signs; an agent's review is a comment, never a signature.
//   - comments: notes on a record or on a node of an app's screen, with the
//     people they mention.
//
// Every write to them is one transaction with its outbox event, and is told
// to live queries on the table as a delta, like a record.

// The tables a live query may watch beside a record type.
const (
	TableWorkItems = "work_items"
	TableTimers    = "timers"
	TableSignoffs  = "signoffs"
	TableComments  = "comments"
)

// The states of a work item.
const (
	WorkOpen      = "open"
	WorkDone      = "done"
	WorkCancelled = "cancelled"
	// AgentAssigneePrefix is the stored prefix for an agent's assignment.
	AgentAssigneePrefix = "agent:"
)

// The bounds of the work tables.
const (
	titleMax     = 500
	bodyMax      = 10_000
	statementMax = 2000
	mentionsMax  = 50
	ListMax      = 500
)

// ItemChange is one work item, timer, sign-off or comment a [Change] made or
// moved, as its table's row.
type ItemChange struct {
	sourceType string
	Table      string
	ID         string
	Type       string
	Record     string
	App        string
	Row        map[string]any
}

// itemChange starts (or joins) the change w reports for a table write.
func (w *writeTx) itemChange(sc Scope, op string) {
	if w.change == nil {
		w.change = &Change{App: sc.App, Actor: w.schema.actor(sc), ByKind: sc.kind(op), Cause: sc.Cause, CauseChain: sc.CauseChain, Op: op, At: w.now}
	}
}

// item adds one table row to w's change and writes its outbox event.
func (w *writeTx) item(table, event string, t *Type, record, app, id string, row map[string]any) error {
	typeID, typ := "", ""
	if t != nil {
		typeID, typ = t.ID, t.Name
	}
	w.change.Items = append(w.change.Items, ItemChange{Table: table, ID: id, Type: typ, Record: record, App: app, Row: row})
	return w.emit(outboxRow{event: event, typeID: typeID, record: record, payload: map[string]string{"id": id}})
}

// subject checks a record a work item, timer, sign-off or comment is about:
// sc may read (or, with write, change) its type, and it is a live record.
func (w *writeTx) subject(sc Scope, typ, id string, write bool) (*Type, *record, error) {
	if typ == "" && id == "" {
		return nil, nil, nil
	}
	t, err := w.schema.allow(sc, typ, write)
	if err != nil {
		return nil, nil, err
	}
	if !validID(id) {
		return nil, nil, errorf(CodeInvalid, "%q is not a record id", id)
	}
	r, err := w.live(t, id)
	if err != nil {
		return nil, nil, err
	}
	return t, r, nil
}

// seen says whether sc may see a table row: the person sees every one; an app
// sees its own and those about records it may read.
func (s *schema) seen(sc Scope, app, typeID string) bool {
	if sc.App == "" || app == sc.App {
		return true
	}
	t := s.byID[typeID]
	if t == nil {
		return false
	}
	_, err := s.allow(sc, t.Name, false)
	return err == nil
}

// person is a person value as a work table keeps it: "me" is the person's id,
// an agent is "agent:<name>".
func (s *schema) personValue(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == Me || v == "$me" {
		return s.person, nil
	}
	if len(v) > 200 || strings.ContainsAny(v, "\r\n\x00") {
		return "", errorf(CodeInvalid, "%q is not a person or an agent", v)
	}
	return v, nil
}

// when reads a due date, an SLA or a timer's moment: a date, a time, or a
// span from now (+3d), answered in the store's one spelling.
func when(v string, now time.Time) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	at, err := queryTime(v, now)
	if err != nil {
		return "", errorf(CodeInvalid, "%q is not a date, a time, or a span like +3d", v)
	}
	if len(v) == len(dateLayout) {
		return at.Format(dateLayout), nil
	}
	return at.UTC().Format(stamp), nil
}

// ---- work items ----

// WorkItem is one thing someone is to do.
type WorkItem struct {
	sourceType string
	ID         string
	Title      string
	Type       string
	Record     string
	Assignee   string
	Due        string
	SLAAt      string
	Form       string
	Status     string
	Outcome    any
	App        string
	CreatedBy  string
	CreatedAt  string
	UpdatedAt  string
}

func (wi *WorkItem) Row() map[string]any {
	return map[string]any{
		"id": wi.ID, "title": wi.Title, "type": wi.Type, "record": wi.Record, "assignee": wi.Assignee,
		"due": wi.Due, "sla_at": wi.SLAAt, "form": wi.Form, "status": wi.Status, "outcome": wi.Outcome,
		"app": wi.App, "created_by": wi.CreatedBy, "created_at": wi.CreatedAt, "updated_at": wi.UpdatedAt,
	}
}

// WorkPatch is a change to a work item; a nil field is left as it is.
type WorkPatch struct {
	Title    *string
	Assignee *string
	Due      *string
	SLAAt    *string
	Form     *string
	Status   *string
	// Outcome is what doing the item produced (a form's answers); it is set
	// with the status that closes it.
	Outcome any
}

// CreateWorkItem makes a work item. A transition's effect, a run's ask step,
// a person and an agent all make them the same way.
func (s *Store) CreateWorkItem(sc Scope, in WorkItem) (*WorkItem, error) {
	var out *WorkItem
	err := s.write(func(w *writeTx) error {
		t, _, err := w.subject(sc, in.Type, in.Record, false)
		if err != nil {
			return err
		}
		out, err = w.createWorkItem(sc, t, in)
		return err
	})
	return out, err
}

// createWorkItem lets the writer commit a work item with its record.
func (w *writeTx) createWorkItem(sc Scope, t *Type, in WorkItem) (*WorkItem, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > titleMax {
		return nil, errorf(CodeInvalid, "a work item has a title of at most %d characters", titleMax)
	}
	item := WorkItem{ID: w.s.ids.next(w.now), Title: in.Title, Record: in.Record, Form: strings.TrimSpace(in.Form), Status: WorkOpen,
		App: sc.App, CreatedBy: w.schema.actor(sc), CreatedAt: w.now.UTC().Format(stamp), sourceType: in.sourceType}
	item.UpdatedAt = item.CreatedAt
	if t != nil {
		item.Type = t.Name
	}
	var err error
	if item.Assignee, err = w.schema.personValue(orMe(in.Assignee)); err != nil {
		return nil, err
	}
	if item.Due, err = when(in.Due, w.now); err != nil {
		return nil, err
	}
	if item.SLAAt, err = when(in.SLAAt, w.now); err != nil {
		return nil, err
	}
	if len(item.Form) > 200 {
		return nil, errorf(CodeInvalid, "a form reference is at most 200 characters")
	}
	typeID := ""
	if t != nil {
		typeID = t.ID
	}
	if _, err := w.tx.Exec(`INSERT INTO work_items(id, title, type_id, record, assignee, due, sla_at, form_ref, status, outcome_json, app, created_by, created_at, updated_at, source_type_id)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, 'null', ?, ?, ?, ?, ?)`,
		item.ID, item.Title, typeID, item.Record, item.Assignee, item.Due, item.SLAAt, item.Form, item.Status, item.App, item.CreatedBy, item.CreatedAt, item.UpdatedAt, item.sourceType); err != nil {
		return nil, err
	}
	w.itemChange(sc, "work_item")
	return &item, w.workItemEvent(EventWorkItemCreated, t, &item)
}

func (w *writeTx) workItemEvent(event string, t *Type, item *WorkItem) error {
	if err := w.item(TableWorkItems, event, t, item.Record, item.App, item.ID, item.Row()); err != nil {
		return err
	}
	w.change.Items[len(w.change.Items)-1].sourceType = item.sourceType
	return nil
}

// orMe is the person when no one is named.
func orMe(v string) string {
	if strings.TrimSpace(v) == "" {
		return Me
	}
	return v
}

// UpdateWorkItem changes a work item: reassigns it, moves its dates, or
// closes it with its outcome. A closed item may be opened again.
func (s *Store) UpdateWorkItem(sc Scope, id string, p WorkPatch) (*WorkItem, error) {
	return s.updateWorkItem(sc, id, p, "")
}

// ClaimWorkItem assigns open, unassigned work to the caller.
func (s *Store) ClaimWorkItem(sc Scope, id string) (*WorkItem, error) {
	return s.updateWorkItem(sc, id, WorkPatch{}, "claim")
}

// CompleteWorkItem closes an open work item with the caller's answers.
func (s *Store) CompleteWorkItem(sc Scope, id string, values map[string]any) (*WorkItem, error) {
	status := WorkDone
	return s.updateWorkItem(sc, id, WorkPatch{Status: &status, Outcome: values}, "complete")
}

// ReassignWorkItem changes the assignee of open work. Empty releases it.
func (s *Store) ReassignWorkItem(sc Scope, id, assignee string) (*WorkItem, error) {
	return s.updateWorkItem(sc, id, WorkPatch{Assignee: &assignee}, "reassign")
}

func (s *Store) updateWorkItem(sc Scope, id string, p WorkPatch, action string) (*WorkItem, error) {
	if !validID(id) {
		return nil, errorf(CodeInvalid, "%q is not a work item", id)
	}
	var out *WorkItem
	err := s.write(func(w *writeTx) error {
		item, typeID, err := loadWorkItem(w.tx, w.schema, id)
		if err != nil {
			return err
		}
		if item == nil || !w.schema.seen(sc, item.App, typeID) || !w.schema.canReadSource(sc, item.sourceType) {
			return errorf(CodeNotFound, "there is no work item %s", id)
		}
		if sc.App != "" && item.App != sc.App {
			if _, err := w.schema.allow(sc, item.Type, true); err != nil {
				return err
			}
		}
		if action != "" {
			if item.Status != WorkOpen {
				return errorf(CodeStale, "this work item is %s", item.Status)
			}
			actor := workActor(w.schema, sc)
			switch action {
			case "claim":
				if item.Assignee == actor {
					out = item
					return nil
				}
				if item.Assignee != "" {
					return errorf(CodeNotAllowed, "this work item is already assigned")
				}
				item.Assignee = actor
			case "complete", "reassign":
				if item.Assignee != actor && (sc.below() || sc.Flow || (item.Assignee != "" && !strings.HasPrefix(item.Assignee, AgentAssigneePrefix))) {
					return errorf(CodeNotAllowed, "this work item belongs to another assignee")
				}
			}
		}
		if p.Title != nil {
			title := strings.TrimSpace(*p.Title)
			if title == "" || len(title) > titleMax {
				return errorf(CodeInvalid, "a work item has a title of at most %d characters", titleMax)
			}
			item.Title = title
		}
		if p.Assignee != nil {
			value := *p.Assignee
			if action == "" {
				value = orMe(value)
			}
			if item.Assignee, err = w.schema.personValue(value); err != nil {
				return err
			}
		}
		if p.Due != nil {
			if item.Due, err = when(*p.Due, w.now); err != nil {
				return err
			}
		}
		if p.SLAAt != nil {
			if item.SLAAt, err = when(*p.SLAAt, w.now); err != nil {
				return err
			}
		}
		if p.Form != nil {
			item.Form = strings.TrimSpace(*p.Form)
		}
		if p.Status != nil {
			switch *p.Status {
			case WorkOpen, WorkDone, WorkCancelled:
				item.Status = *p.Status
			default:
				return errorf(CodeInvalid, "a work item is open, done or cancelled")
			}
		}
		if p.Outcome != nil {
			if item.Status == WorkOpen {
				return errorf(CodeInvalid, "an outcome comes with done or cancelled")
			}
			item.Outcome = p.Outcome
		}
		item.UpdatedAt = w.now.UTC().Format(stamp)
		if _, err := w.tx.Exec(`UPDATE work_items SET title = ?, assignee = ?, due = ?, sla_at = ?, form_ref = ?, status = ?, outcome_json = ?, updated_at = ? WHERE id = ?`,
			item.Title, item.Assignee, item.Due, item.SLAAt, item.Form, item.Status, canonicalJSON(item.Outcome), item.UpdatedAt, id); err != nil {
			return err
		}
		w.itemChange(sc, "work_item")
		out = item
		return w.workItemEvent(EventWorkItemUpdated, w.schema.byID[typeID], item)
	})
	return out, err
}

const workColumns = `id, title, type_id, record, assignee, due, sla_at, form_ref, status, outcome_json, app, created_by, created_at, updated_at, source_type_id`

func scanWorkItem(sch *schema, scan func(...any) error) (*WorkItem, string, error) {
	var wi WorkItem
	var typeID, outcome string
	if err := scan(&wi.ID, &wi.Title, &typeID, &wi.Record, &wi.Assignee, &wi.Due, &wi.SLAAt, &wi.Form, &wi.Status, &outcome, &wi.App, &wi.CreatedBy, &wi.CreatedAt, &wi.UpdatedAt, &wi.sourceType); err != nil {
		return nil, "", err
	}
	_ = json.Unmarshal([]byte(outcome), &wi.Outcome)
	if t := sch.byID[typeID]; t != nil {
		wi.Type = t.Name
	}
	return &wi, typeID, nil
}

func loadWorkItem(q querier, sch *schema, id string) (*WorkItem, string, error) {
	wi, typeID, err := scanWorkItem(sch, q.QueryRow(`SELECT `+workColumns+` FROM work_items WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	return wi, typeID, err
}

// WorkFilter narrows a list of work items; empty fields do not narrow.
// Assignee "me" is the person.
type WorkFilter struct {
	Assignee string
	Status   string
	Type     string
	Record   string
}

// WorkItems lists work items, the soonest due first (undated last), at most
// [ListMax]: the person's "My work" across every app, or one app's queue.
func (s *Store) WorkItems(sc Scope, f WorkFilter) ([]WorkItem, error) {
	return workItemsOn(s.rdb, s.schema.Load(), sc, f, false)
}

// MyWork lists open items assigned to the person or their agents. An agent
// calling it sees only its own assignments.
func (s *Store) MyWork(sc Scope) ([]WorkItem, error) {
	var out []WorkItem
	err := s.write(func(w *writeTx) error {
		f := WorkFilter{Status: WorkOpen}
		mine := !sc.below()
		if !mine {
			f.Assignee = workActor(w.schema, sc)
		}
		var err error
		out, err = workItemsOn(w.tx, w.schema, sc, f, mine)
		return err
	})
	return out, err
}

func workActor(sch *schema, sc Scope) string {
	if sc.below() {
		name := sc.Actor
		if name == "" {
			name = sc.App
		}
		return AgentAssigneePrefix + strings.TrimPrefix(name, AgentAssigneePrefix)
	}
	return sch.person
}

func workItemsOn(q querier, sch *schema, sc Scope, f WorkFilter, mine bool) ([]WorkItem, error) {
	where := []string{"1"}
	var args []any
	if mine {
		where = append(where, "(assignee = ? OR substr(assignee, 1, ?) = ?)")
		args = append(args, sch.person, len(AgentAssigneePrefix), AgentAssigneePrefix)
	}
	if f.Assignee != "" {
		a, err := sch.personValue(f.Assignee)
		if err != nil {
			return nil, err
		}
		where, args = append(where, "assignee = ?"), append(args, a)
	}
	if f.Status != "" {
		where, args = append(where, "status = ?"), append(args, f.Status)
	}
	if f.Type != "" {
		t, err := sch.allow(sc, f.Type, false)
		if err != nil {
			return nil, err
		}
		where, args = append(where, "type_id = ?"), append(args, t.ID)
	}
	if f.Record != "" {
		where, args = append(where, "record = ?"), append(args, f.Record)
	}
	out := []WorkItem{}
	err := eachRow(q, `SELECT `+workColumns+` FROM work_items WHERE `+strings.Join(where, " AND ")+
		` ORDER BY due = '', due, created_at, id`, args, func(scan func(...any) error) error {
		wi, typeID, err := scanWorkItem(sch, scan)
		if err != nil {
			return err
		}
		if sch.seen(sc, wi.App, typeID) && sch.canReadSource(sc, wi.sourceType) && len(out) < ListMax {
			out = append(out, *wi)
		}
		return nil
	})
	return out, err
}

// ---- timers ----

// Timer is one moment a record (or an app) is waiting for. Every, when set,
// makes it recurring: once fired, the next one is set Every later, skipping
// the moments that passed while the store was closed.
type Timer struct {
	ID          string
	Type        string
	Record      string
	FireAt      string
	Kind        string
	Payload     any
	Every       string
	FiredAt     string
	CancelledAt string
	App         string
	CreatedBy   string
	CreatedAt   string
}

func (tm *Timer) Row() map[string]any {
	return map[string]any{
		"id": tm.ID, "type": tm.Type, "record": tm.Record, "fire_at": tm.FireAt, "kind": tm.Kind, "payload": tm.Payload,
		"every": tm.Every, "fired_at": tm.FiredAt, "cancelled_at": tm.CancelledAt, "app": tm.App,
		"created_by": tm.CreatedBy, "created_at": tm.CreatedAt,
	}
}

// timersPerFire is the most timers one firing takes, so a long-closed store
// catches up in short writes.
const timersPerFire = 200

// CreateTimer sets a timer. FireAt is a time or a span from now (+3d).
func (s *Store) CreateTimer(sc Scope, in Timer) (*Timer, error) {
	in.Kind = strings.TrimSpace(in.Kind)
	if in.Kind == "" || len(in.Kind) > 64 {
		return nil, errorf(CodeInvalid, "a timer has a kind (follow_up, sla, due…) of at most 64 characters")
	}
	if in.Every != "" {
		if span, err := parseSpan(in.Every); err != nil || span < time.Minute {
			return nil, errorf(CodeInvalid, "every is a span of a minute or more, like 1d or 2w")
		}
	}
	if raw := canonicalJSON(in.Payload); len(raw) > 16<<10 {
		return nil, errorf(CodeInvalid, "a timer's payload is at most 16 KB")
	}
	var out *Timer
	err := s.write(func(w *writeTx) error {
		t, _, err := w.subject(sc, in.Type, in.Record, true)
		if err != nil {
			return err
		}
		at, err := when(in.FireAt, w.now)
		if err != nil {
			return err
		}
		if at == "" {
			return errorf(CodeInvalid, "a timer needs fire_at")
		}
		if len(at) == len(dateLayout) {
			day, _ := time.ParseInLocation(dateLayout, at, time.Local)
			at = day.UTC().Format(stamp)
		}
		tm := Timer{ID: w.s.ids.next(w.now), Record: in.Record, FireAt: at, Kind: in.Kind, Payload: in.Payload, Every: in.Every,
			App: sc.App, CreatedBy: w.schema.actor(sc), CreatedAt: w.now.UTC().Format(stamp)}
		typeID := ""
		if t != nil {
			tm.Type, typeID = t.Name, t.ID
		}
		if err := insertTimer(w, &tm, typeID); err != nil {
			return err
		}
		w.itemChange(sc, "timer")
		out = &tm
		return w.item(TableTimers, EventTimerCreated, t, tm.Record, tm.App, tm.ID, tm.Row())
	})
	return out, err
}

func insertTimer(w *writeTx, tm *Timer, typeID string) error {
	_, err := w.tx.Exec(`INSERT INTO timers(id, type_id, record, fire_at, kind, payload_json, every, app, created_by, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tm.ID, typeID, tm.Record, tm.FireAt, tm.Kind, canonicalJSON(tm.Payload), tm.Every, tm.App, tm.CreatedBy, tm.CreatedAt)
	return err
}

// CancelTimer stops a timer that has not fired.
func (s *Store) CancelTimer(sc Scope, id string) error {
	if !validID(id) {
		return errorf(CodeInvalid, "%q is not a timer", id)
	}
	return s.write(func(w *writeTx) error {
		tm, typeID, err := loadTimer(w.tx, w.schema, id)
		if err != nil {
			return err
		}
		if tm == nil || !w.schema.seen(sc, tm.App, typeID) {
			return errorf(CodeNotFound, "there is no timer %s", id)
		}
		if tm.FiredAt != "" || tm.CancelledAt != "" {
			return errorf(CodeStale, "that timer has already fired or been cancelled")
		}
		tm.CancelledAt = w.now.UTC().Format(stamp)
		if _, err := w.tx.Exec(`UPDATE timers SET cancelled_at = ? WHERE id = ?`, tm.CancelledAt, id); err != nil {
			return err
		}
		w.itemChange(sc, "timer")
		return w.item(TableTimers, EventTimerCancelled, w.schema.byID[typeID], tm.Record, tm.App, tm.ID, tm.Row())
	})
}

const timerColumns = `id, type_id, record, fire_at, kind, payload_json, every, COALESCE(fired_at, ''), COALESCE(cancelled_at, ''), app, created_by, created_at`

func scanTimer(sch *schema, scan func(...any) error) (*Timer, string, error) {
	var tm Timer
	var typeID, payload string
	if err := scan(&tm.ID, &typeID, &tm.Record, &tm.FireAt, &tm.Kind, &payload, &tm.Every, &tm.FiredAt, &tm.CancelledAt, &tm.App, &tm.CreatedBy, &tm.CreatedAt); err != nil {
		return nil, "", err
	}
	_ = json.Unmarshal([]byte(payload), &tm.Payload)
	if t := sch.byID[typeID]; t != nil {
		tm.Type = t.Name
	}
	return &tm, typeID, nil
}

func loadTimer(q querier, sch *schema, id string) (*Timer, string, error) {
	tm, typeID, err := scanTimer(sch, q.QueryRow(`SELECT `+timerColumns+` FROM timers WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	return tm, typeID, err
}

// Timers lists timers, soonest first, at most [ListMax]: one record's, or
// (Pending) every one still to fire.
func (s *Store) Timers(sc Scope, record string, pending bool) ([]Timer, error) {
	return timersOn(s.rdb, s.schema.Load(), sc, record, pending)
}

func timersOn(q querier, sch *schema, sc Scope, record string, pending bool) ([]Timer, error) {
	where := []string{"1"}
	var args []any
	if record != "" {
		where, args = append(where, "record = ?"), append(args, record)
	}
	if pending {
		where = append(where, "fired_at IS NULL AND cancelled_at IS NULL")
	}
	out := []Timer{}
	err := eachRow(q, `SELECT `+timerColumns+` FROM timers WHERE `+strings.Join(where, " AND ")+` ORDER BY fire_at, id`, args,
		func(scan func(...any) error) error {
			tm, typeID, err := scanTimer(sch, scan)
			if err != nil {
				return err
			}
			if sch.seen(sc, tm.App, typeID) && len(out) < ListMax {
				out = append(out, *tm)
			}
			return nil
		})
	return out, err
}

// FireDue fires every timer whose moment has come, each with its outbox
// event, and answers how many fired. It is what the store's clock runs; the
// first run after the store opens is the catch-up for the time it was closed.
func (s *Store) FireDue() (int, error) {
	fired := 0
	for {
		n := 0
		err := s.write(func(w *writeTx) error {
			n = 0
			now := w.now.UTC().Format(stamp)
			var due []*Timer
			var typeIDs []string
			err := eachRow(w.tx, `SELECT `+timerColumns+` FROM timers WHERE fired_at IS NULL AND cancelled_at IS NULL AND fire_at <= ? ORDER BY fire_at, id LIMIT ?`,
				[]any{now, timersPerFire}, func(scan func(...any) error) error {
					tm, typeID, err := scanTimer(w.schema, scan)
					if err == nil {
						due, typeIDs = append(due, tm), append(typeIDs, typeID)
					}
					return err
				})
			if err != nil || len(due) == 0 {
				return err
			}
			w.itemChange(Scope{Flow: true, Cause: "clock"}, "timer")
			for i, tm := range due {
				tm.FiredAt = now
				if _, err := w.tx.Exec(`UPDATE timers SET fired_at = ? WHERE id = ?`, now, tm.ID); err != nil {
					return err
				}
				t := w.schema.byID[typeIDs[i]]
				if err := w.item(TableTimers, EventTimerFired, t, tm.Record, tm.App, tm.ID, tm.Row()); err != nil {
					return err
				}
				if tm.Every != "" {
					span, err := parseSpan(tm.Every)
					if err != nil || span < time.Minute {
						continue
					}
					at, _ := time.Parse(stamp, tm.FireAt)
					for !at.After(w.now) {
						at = at.Add(span)
					}
					next := Timer{ID: w.s.ids.next(w.now), Type: tm.Type, Record: tm.Record, FireAt: at.UTC().Format(stamp), Kind: tm.Kind,
						Payload: tm.Payload, Every: tm.Every, App: tm.App, CreatedBy: tm.CreatedBy, CreatedAt: now}
					if err := insertTimer(w, &next, typeIDs[i]); err != nil {
						return err
					}
					if err := w.item(TableTimers, EventTimerCreated, t, next.Record, next.App, next.ID, next.Row()); err != nil {
						return err
					}
				}
				n++
			}
			return nil
		})
		fired += n
		if err != nil || n < timersPerFire {
			return fired, err
		}
	}
}

// ---- sign-offs ----

// Signoff is one signed statement bound to one version of a record. Current
// says the record is still that version.
type Signoff struct {
	ID          string
	Type        string
	Record      string
	VersionHash string
	Statement   string
	Signer      string
	App         string
	At          string
	Current     bool
}

func (so *Signoff) Row() map[string]any {
	return map[string]any{
		"id": so.ID, "type": so.Type, "record": so.Record, "version_hash": so.VersionHash, "statement": so.Statement,
		"signer": so.Signer, "app": so.App, "at": so.At, "current": so.Current,
	}
}

// Sign signs a statement against a record as it is now. Rev, when set, is the
// revision the person read; a record that moved since is refused as stale,
// so nobody signs a version they did not see. ONLY THE PERSON SIGNS: an agent
// or an automation is refused, and its review belongs in a comment.
func (s *Store) Sign(sc Scope, typ, id, statement string, rev int64) (*Signoff, error) {
	if sc.Agent || sc.AI || sc.Flow {
		return nil, &Error{Code: CodeNotAllowed, Need: "sign", Message: "only the person signs; an agent's review is a comment"}
	}
	statement = strings.TrimSpace(statement)
	if statement == "" || len(statement) > statementMax {
		return nil, errorf(CodeInvalid, "a sign-off states what is signed, in at most %d characters", statementMax)
	}
	var out *Signoff
	err := s.write(func(w *writeTx) error {
		t, r, err := w.subject(sc, typ, id, true)
		if err != nil {
			return err
		}
		if t == nil {
			return errorf(CodeInvalid, "a sign-off names its record")
		}
		if rev != 0 && r.Rev != rev {
			return &Error{Code: CodeStale, Message: fmt.Sprintf("this %s has changed since revision %d; read it again before signing", t.Name, rev)}
		}
		if t.rules.invalid != nil {
			return t.rules.invalid
		}
		rule := t.approvalFor(r)
		var step int
		if rule != nil {
			step, err = w.nextApproval(r, rule, statement)
			if err != nil {
				return err
			}
		}
		so := Signoff{ID: w.s.ids.next(w.now), Type: t.Name, Record: id, VersionHash: versionHash(t, r), Statement: statement,
			Signer: w.schema.actor(sc), App: sc.App, At: w.now.UTC().Format(stamp), Current: true}
		if rule != nil {
			so.Signer = w.schema.person
		}
		if _, err := w.tx.Exec(`INSERT INTO signoffs(id, type_id, record, version_hash, statement, signer, app, at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
			so.ID, t.ID, so.Record, so.VersionHash, so.Statement, so.Signer, so.App, so.At); err != nil {
			return err
		}
		if rule != nil {
			if _, err := w.tx.Exec(`INSERT INTO approval_receipts(record,revision,route,step,signoff) VALUES(?,?,?,?,?)`,
				r.ID, r.Rev, rule.key, step, so.ID); err != nil {
				return err
			}
			if err := w.ruleActivity(sc, t, r.ID, fmt.Sprintf("%s: signed step %d of %d (%s)", rule.id, step+1, len(rule.def.Roles), rule.def.Roles[step])); err != nil {
				return err
			}
		}
		w.itemChange(sc, "signoff")
		out = &so
		return w.item(TableSignoffs, EventSignoff, t, id, so.App, so.ID, so.Row())
	})
	return out, err
}

// Signoffs lists a record's sign-offs in the order they were signed, each
// saying whether it covers the record as it is now.
func (s *Store) Signoffs(sc Scope, typ, id string) ([]Signoff, error) {
	sch := s.schema.Load()
	t, err := sch.allow(sc, typ, false)
	if err != nil {
		return nil, err
	}
	r, err := loadRecord(s.rdb, sch, id)
	if err != nil {
		return nil, err
	}
	if r != nil && r.Type != t.Name {
		r = nil
	}
	return signoffsOn(s.rdb, t, id, r)
}

func signoffsOn(q querier, t *Type, id string, r *record) ([]Signoff, error) {
	var current, route string
	var revision int64
	if r != nil {
		current, revision = versionHash(t, r), r.Rev
		if rule := t.approvalFor(r); rule != nil {
			route = rule.key
		}
	}
	out := []Signoff{}
	err := eachRow(q, `SELECT s.id, s.record, s.version_hash, s.statement, s.signer, s.app, s.at,
		COALESCE(a.route,''), COALESCE(a.revision,0)
		FROM signoffs s LEFT JOIN approval_receipts a ON s.id = a.signoff
		WHERE s.record = ? AND s.type_id = ? ORDER BY s.at, s.id`,
		[]any{id, t.ID}, func(scan func(...any) error) error {
			so := Signoff{Type: t.Name}
			var signedRoute string
			var signedRev int64
			if err := scan(&so.ID, &so.Record, &so.VersionHash, &so.Statement, &so.Signer, &so.App, &so.At, &signedRoute, &signedRev); err != nil {
				return err
			}
			so.Current = so.VersionHash == current && route == signedRoute && (route == "" || revision == signedRev)
			out = append(out, so)
			return nil
		})
	return out, err
}

// ---- comments ----

// Comment is a note on a record, or on a node of an app's screen, with the
// people it mentions.
type Comment struct {
	ID       string
	Type     string
	Record   string
	Node     string
	Body     string
	Author   string
	Mentions []string
	App      string
	At       string
}

func (c *Comment) Row() map[string]any {
	return map[string]any{
		"id": c.ID, "type": c.Type, "record": c.Record, "node": c.Node, "body": c.Body, "author": c.Author,
		"mentions": c.Mentions, "app": c.App, "at": c.At,
	}
}

// AddComment adds a comment to a record (Type and Record) or to a node of the
// calling app's screen (Node). An agent may comment, and is named as one.
func (s *Store) AddComment(sc Scope, in Comment) (*Comment, error) {
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" || len(in.Body) > bodyMax {
		return nil, errorf(CodeInvalid, "a comment has a body of at most %d characters", bodyMax)
	}
	if (in.Record == "") == (in.Node == "") {
		return nil, errorf(CodeInvalid, "a comment is on a record or on a node, one of them")
	}
	if len(in.Mentions) > mentionsMax {
		return nil, errorf(CodeInvalid, "a comment mentions at most %d people", mentionsMax)
	}
	var out *Comment
	err := s.write(func(w *writeTx) error {
		c := Comment{ID: w.s.ids.next(w.now), Body: in.Body, Author: w.schema.actor(sc), App: sc.App, At: w.now.UTC().Format(stamp), Mentions: []string{}}
		var t *Type
		if in.Record != "" {
			var err error
			if t, _, err = w.subject(sc, in.Type, in.Record, false); err != nil {
				return err
			}
			c.Type, c.Record = t.Name, in.Record
		} else {
			if sc.App == "" && in.App == "" {
				return errorf(CodeInvalid, "a comment on a node names its app")
			}
			if sc.App == "" {
				c.App = in.App
			}
			c.Node = strings.TrimSpace(in.Node)
			if len(c.Node) > 200 {
				return errorf(CodeInvalid, "a node id is at most 200 characters")
			}
		}
		for _, m := range in.Mentions {
			p, err := w.schema.personValue(m)
			if err != nil {
				return err
			}
			c.Mentions = append(c.Mentions, p)
		}
		typeID := ""
		if t != nil {
			typeID = t.ID
		}
		if _, err := w.tx.Exec(`INSERT INTO comments(id, type_id, record, node, body, author, mentions_json, app, at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, typeID, c.Record, c.Node, c.Body, c.Author, canonicalJSON(c.Mentions), c.App, c.At); err != nil {
			return err
		}
		w.itemChange(sc, "comment")
		out = &c
		return w.item(TableComments, EventComment, t, c.Record, c.App, c.ID, c.Row())
	})
	return out, err
}

// Comments lists the comments on one record, or on one node of one app, in
// the order they were written, at most [ListMax].
func (s *Store) Comments(sc Scope, typ, record, app, node string) ([]Comment, error) {
	return commentsOn(s.rdb, s.schema.Load(), sc, typ, record, app, node)
}

func commentsOn(q querier, sch *schema, sc Scope, typ, record, app, node string) ([]Comment, error) {
	var stmt string
	var args []any
	switch {
	case record != "":
		t, err := sch.allow(sc, typ, false)
		if err != nil {
			return nil, err
		}
		stmt, args = `record = ? AND type_id = ?`, []any{record, t.ID}
	case node != "":
		if sc.App != "" {
			app = sc.App
		}
		stmt, args = `app = ? AND node = ?`, []any{app, node}
	default:
		return nil, errorf(CodeInvalid, "comments are listed for a record or a node")
	}
	out := []Comment{}
	err := eachRow(q, `SELECT id, type_id, record, node, body, author, mentions_json, app, at FROM comments WHERE `+stmt+` ORDER BY at, id LIMIT ?`,
		append(args, ListMax), func(scan func(...any) error) error {
			var c Comment
			var typeID, mentions string
			if err := scan(&c.ID, &typeID, &c.Record, &c.Node, &c.Body, &c.Author, &mentions, &c.App, &c.At); err != nil {
				return err
			}
			_ = json.Unmarshal([]byte(mentions), &c.Mentions)
			if t := sch.byID[typeID]; t != nil {
				c.Type = t.Name
			}
			out = append(out, c)
			return nil
		})
	return out, err
}

// ---- activity ----

// Activity is one line of a record's activity feed: a transition (who moved
// it, from where to where, and why), a comment, or a sign-off.
type Activity struct {
	Kind       string
	ID         string
	At         string
	Actor      string
	ByKind     string
	Transition string
	From       string
	To         string
	Reason     string
	Body       string
	Mentions   []string
}

// Row is the activity line as the wire carries it.
func (a *Activity) Row() map[string]any {
	out := map[string]any{"kind": a.Kind, "id": a.ID, "at": a.At, "actor": a.Actor}
	for k, v := range map[string]string{"by_kind": a.ByKind, "transition": a.Transition, "from": a.From, "to": a.To, "reason": a.Reason, "body": a.Body} {
		if v != "" {
			out[k] = v
		}
	}
	if len(a.Mentions) > 0 {
		out["mentions"] = a.Mentions
	}
	return out
}

// The kinds of activity.
const (
	ActivityTransition = "transition"
	ActivityComment    = "comment"
	ActivitySignoff    = "signoff"
	ActivityRule       = "rule"
)

// RecordActivity is a record's activity feed, oldest first: its transitions,
// its comments and its sign-offs, at most [ListMax] of each.
func (s *Store) RecordActivity(sc Scope, typ, id string) ([]Activity, error) {
	sch := s.schema.Load()
	t, err := sch.allow(sc, typ, false)
	if err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, errorf(CodeInvalid, "%q is not a record id", id)
	}
	signoffs, err := s.Signoffs(sc, typ, id)
	if err != nil {
		return nil, err
	}
	return activityOn(s.rdb, sch, sc, t, id, signoffs)
}

func activityOn(q querier, sch *schema, sc Scope, t *Type, id string, signoffs []Signoff) ([]Activity, error) {
	out := []Activity{}
	if lc := t.Lifecycle; lc != nil || len(t.checklists) != 0 {
		err := eachRow(q, `SELECT seq, op, actor, by_kind, note, before_json, after_json, at FROM history WHERE record = ? AND type_id = ? AND op IN (?, ?) ORDER BY seq DESC LIMIT ?`,
			[]any{id, t.ID, OpTransition, OpChecklist, ListMax}, func(scan func(...any) error) error {
				var seq int64
				var note string
				var before, after sql.NullString
				var a Activity
				if err := scan(&seq, &a.Kind, &a.Actor, &a.ByKind, &note, &before, &after, &a.At); err != nil {
					return err
				}
				a.ID = fmt.Sprint(seq)
				if a.Kind == OpChecklist {
					a.Reason = note
				} else if lc != nil {
					a.Transition, a.Reason, _ = strings.Cut(note, ": ")
					if before.Valid {
						a.From, _ = t.decodeState(before.String).Data[lc.Field].(string)
					}
					if after.Valid {
						a.To, _ = t.decodeState(after.String).Data[lc.Field].(string)
					}
				}
				out = append(out, a)
				return nil
			})
		if err != nil {
			return nil, err
		}
	}
	if err := eachRow(q, `SELECT * FROM (SELECT id, actor, by_kind, reason, at FROM rule_activity
		WHERE type_id = ? AND record = ? AND (? = '' OR source_type_id = '' OR EXISTS (
			SELECT 1 FROM types WHERE types.id = rule_activity.source_type_id AND types.name IN (
				SELECT value FROM json_each(?)
			)
		)) ORDER BY id DESC LIMIT ?) ORDER BY id`,
		[]any{t.ID, id, sc.App, canonicalJSON(sortedKeys(sch.caps[sc.App])), ListMax}, func(scan func(...any) error) error {
			a := Activity{Kind: ActivityRule}
			if err := scan(&a.ID, &a.Actor, &a.ByKind, &a.Reason, &a.At); err != nil {
				return err
			}
			out = append(out, a)
			return nil
		}); err != nil {
		return nil, err
	}
	comments, err := commentsOn(q, sch, sc, t.Name, id, "", "")
	if err != nil {
		return nil, err
	}
	for _, c := range comments {
		out = append(out, Activity{Kind: ActivityComment, ID: c.ID, At: c.At, Actor: c.Author, Body: c.Body, Mentions: c.Mentions})
	}
	for _, so := range signoffs {
		out = append(out, Activity{Kind: ActivitySignoff, ID: so.ID, At: so.At, Actor: so.Signer, ByKind: ByPerson, Body: so.Statement})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out, nil
}
