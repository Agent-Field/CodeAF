package data

import (
	"database/sql"
	"errors"
)

const aiReviewMigration = `
	CREATE TABLE ai_reviews (
		app TEXT NOT NULL,
		queue TEXT NOT NULL,
		proposal TEXT NOT NULL,
		decision TEXT NOT NULL,
		PRIMARY KEY (app, queue, proposal)
	);
`

// AIEffect is a proposed update prepared by an engine run.
type AIEffect struct {
	ID       string
	Record   string
	Revision int64
	Set      map[string]any
}

// AIReviews reads committed review decisions, including declined proposals.
func (s *Store) AIReviews(app, queue string) (map[string]string, error) {
	rows, err := s.rdb.Query("SELECT proposal, decision FROM ai_reviews WHERE app = ? AND queue = ?", app, queue)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reviews := map[string]string{}
	for rows.Next() {
		var id, decision string
		if err := rows.Scan(&id, &decision); err != nil {
			return nil, err
		}
		reviews[id] = decision
	}
	return reviews, rows.Err()
}

// ReviewAI commits the chosen effects and their receipts in the same writer
// transaction. A replay can see its receipt without repeating a record edit.
func (s *Store) ReviewAI(sc Scope, queue, typ, decision string, effects []AIEffect) error {
	if sc.below() || sc.Flow {
		return &Error{Code: CodeNotAllowed, Need: "review", Message: "only the person can apply proposed changes"}
	}
	if queue == "" || (decision != "accept" && decision != "reject" && decision != "accept_safe") {
		return errorf(CodeInvalid, "choose a queue and a review decision")
	}
	sc.AI = true
	sc.Cause = "review:" + queue
	return s.write(func(w *writeTx) error {
		t, err := w.schema.allow(sc, typ, decision != "reject")
		if err != nil {
			return err
		}
		if t.Ledger && decision != "reject" {
			return errorf(CodeLedger, "ledger entries cannot be updated")
		}
		seen := map[string]bool{}
		pending := make([]reviewedEffect, 0, len(effects))
		for _, effect := range effects {
			if effect.ID == "" || effect.Revision <= 0 || seen[effect.Record] {
				return errorf(CodeInvalid, "select each revision once")
			}
			seen[effect.Record] = true
			entry, err := w.prepareReview(sc, t, queue, decision, effect)
			if err != nil {
				return err
			}
			if entry.before != nil {
				pending = append(pending, entry)
			}
		}
		for _, effect := range pending {
			if decision != "reject" {
				if _, _, err := w.updateRecord(sc, t, effect.before, effect.set, nil, ""); err != nil {
					return err
				}
			}
			if _, err := w.tx.Exec("INSERT INTO ai_reviews(app,queue,proposal,decision) VALUES(?,?,?,?)", sc.App, queue, effect.id, decision); err != nil {
				return err
			}
		}
		return nil
	})
}

type reviewedEffect struct {
	id     string
	before *record
	set    map[string]any
}

func (w *writeTx) prepareReview(sc Scope, t *Type, queue, decision string, effect AIEffect) (reviewedEffect, error) {
	var previous string
	err := w.tx.QueryRow("SELECT decision FROM ai_reviews WHERE app = ? AND queue = ? AND proposal = ?", sc.App, queue, effect.ID).Scan(&previous)
	if err == nil {
		if previous == decision {
			return reviewedEffect{}, nil
		}
		return reviewedEffect{}, errorf(CodeStale, "this proposal already has a decision")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return reviewedEffect{}, err
	}
	before, err := w.live(t, effect.Record)
	if err != nil {
		return reviewedEffect{}, err
	}
	if decision == "accept_safe" && !safeAIReview(t, before, effect.Set) {
		return reviewedEffect{}, nil
	}
	if effect.Revision != before.Rev {
		return reviewedEffect{}, errorf(CodeStale, "a proposed record changed after the model read it")
	}
	if decision == "reject" {
		return reviewedEffect{id: effect.ID, before: before}, nil
	}
	values, err := w.prepareSet(t, effect.Set, prepared{})
	if err != nil {
		return reviewedEffect{}, err
	}
	return reviewedEffect{id: effect.ID, before: before, set: values}, nil
}

func safeAIReview(t *Type, row *record, set map[string]any) bool {
	if len(set) == 0 {
		return false
	}
	for name, value := range set {
		f := t.field(name)
		if f == nil || f.AI == "" || f.Masked || f.Unique || value == nil {
			return false
		}
		if row.Data[f.Name] != nil || row.Meta.By[f.Name] == ByPerson || row.Meta.By[f.Name] == ByImport {
			return false
		}
	}
	return true
}
