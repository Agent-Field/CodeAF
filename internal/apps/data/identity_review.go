package data

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

const (
	maxIdentityReviews = 10
	identityReviewForm = "identity-review"
)

// IdentityCandidate points to a work item and the two revisions it compares.
type IdentityCandidate struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Left     string        `json:"left"`
	Right    string        `json:"right"`
	LeftRev  int64         `json:"left_revision"`
	RightRev int64         `json:"right_revision"`
	Evidence MatchEvidence `json:"evidence"`

	typeID string
	epoch  int64
}

const matchColumns = `id, type_id, left_id, right_id, left_rev, right_rev, evidence, mapping_epoch`

func scanIdentityCandidate(sch *schema, scan func(...any) error) (*IdentityCandidate, error) {
	var c IdentityCandidate
	var raw string
	if err := scan(&c.ID, &c.typeID, &c.Left, &c.Right, &c.LeftRev, &c.RightRev, &raw, &c.epoch); err != nil {
		return nil, err
	}
	if t := sch.byID[c.typeID]; t != nil {
		c.Type = t.Name
	}
	return &c, json.Unmarshal([]byte(raw), &c.Evidence)
}

// IdentityCandidates lists outstanding duplicate reviews for the space owner.
func (s *Store) IdentityCandidates(sc Scope) ([]IdentityCandidate, error) {
	if err := allowIdentity(sc, s.draft != ""); err != nil {
		return nil, err
	}
	var out = []IdentityCandidate{}
	sch := s.schema.Load()
	err := eachRow(s.rdb, `SELECT `+matchColumns+` FROM identity_matches
		WHERE decision = '' ORDER BY id LIMIT ?`, []any{ListMax}, func(scan func(...any) error) error {
		c, err := scanIdentityCandidate(sch, scan)
		if err == nil {
			out = append(out, *c)
		}
		return err
	})
	return out, err
}

// ScanIdentity looks for duplicates of a native Person or Organization.
// A scan proposes decisions for owner review, including strong matches.
func (s *Store) ScanIdentity(sc Scope, typ, id string, fields IdentityFields) error {
	if err := allowIdentity(sc, s.draft != ""); err != nil {
		return err
	}
	if !validID(id) {
		return errorf(CodeInvalid, "choose a record ID")
	}
	return s.write(func(w *writeTx) error {
		t, err := w.schema.allow(sc, typ, false)
		if err != nil {
			return err
		}
		if !t.identifiable() {
			return errorf(CodeInvalid, "choose a type bound to Person or Organization")
		}
		if fields == (IdentityFields{}) {
			fields = defaultIdentityFields(t)
		}
		if err := fields.bind(t); err != nil {
			return err
		}
		anchor, err := w.live(t, id)
		if err != nil {
			return err
		}
		_, err = w.matchIdentity(sc, t, anchor, fields, false)
		return err
	})
}

func defaultIdentityFields(t *Type) IdentityFields {
	field := func(names ...string) string {
		for _, name := range names {
			if f := t.field(name); f != nil && !f.Hidden {
				return name
			}
		}
		return ""
	}
	return IdentityFields{Name: field("name", "full_name"), Email: field("email"), Organization: field("organization", "company"),
		Phone: field("phone"), Domain: field("domain", "website")}
}

func (w *writeTx) matchingSubject(t *Type, r *record, fields IdentityFields) (MatchSubject, error) {
	subject := fields.subject(t, r)
	if subject.Organization != "" && validID(subject.Organization) {
		root, err := identityRoot(w.tx, subject.Organization)
		if err != nil {
			return subject, err
		}
		subject.Organization = root
	}
	err := w.tx.QueryRow(`SELECT
		EXISTS(SELECT 1 FROM identity_attestations WHERE record = ?1 AND rev = ?2 AND field_id = ?3 AND role = 'work_email' AND context = ?4),
		COALESCE((SELECT context FROM identity_attestations WHERE record = ?1 AND rev = ?2 AND field_id = ?5 AND role = 'external'), '')`,
		r.ID, r.Rev, fields.Email, fields.Organization, fields.ExternalID).Scan(&subject.VerifiedWorkEmail, &subject.Namespace)
	return subject, err
}

type candidateScore struct {
	record   *record
	evidence MatchEvidence
}

// matchIdentity considers every live record before applying a unique high
// score. Only a native source observation may automatically join records.
func (w *writeTx) matchIdentity(sc Scope, t *Type, anchor *record, fields IdentityFields, auto bool) (string, error) {
	if err := w.retireIdentityReviews(sc, t, anchor.ID, ""); err != nil {
		return "", err
	}
	subject, err := w.matchingSubject(t, anchor, fields)
	if !auto && subject.Namespace == "" {
		subject.Namespace = fields.Namespace
	}
	if err != nil {
		return "", err
	}
	var candidates []candidateScore
	err = eachRow(w.tx, `SELECT `+recordColumns+` FROM records WHERE type_id = ? AND deleted_at IS NULL AND id <> ? ORDER BY id`,
		[]any{t.ID, anchor.ID}, func(scan func(...any) error) error {
			r, err := scanRecord(w.schema, scan)
			if err != nil {
				return err
			}
			other, err := w.matchingSubject(t, r, fields)
			if !auto && other.Namespace == "" {
				other.Namespace = fields.Namespace
			}
			if err != nil {
				return err
			}
			evidence := ScoreIdentity(t.Common, subject, other)
			if evidence.Decision == MatchSeparate {
				return nil
			}
			rejected, err := rejectedIdentity(w.tx, anchor.ID, r.ID)
			if err != nil || rejected {
				return err
			}
			if personConflict(t, r, anchor) {
				evidence.Decision = MatchReview
				evidence.Signals = append(evidence.Signals, "person-entered conflict")
			}
			candidates = append(candidates, candidateScore{r, evidence})
			slices.SortStableFunc(candidates, func(a, b candidateScore) int { return b.evidence.Score - a.evidence.Score })
			if len(candidates) > maxIdentityReviews {
				candidates = candidates[:maxIdentityReviews]
			}
			return nil
		})
	if err != nil {
		return "", err
	}
	if len(candidates) == 1 && auto && candidates[0].evidence.Decision == MatchLink {
		original := w.change
		merge, err := w.mergeIdentity(sc, t, candidates[0].record, anchor, candidates[0].evidence)
		if err != nil {
			return "", err
		}
		// The committed survivor is the only record a subscriber should see
		// if the source row was created and absorbed in this transaction.
		if original != nil {
			if before := original.befores[anchor.ID]; before == nil {
				w.change.Records = slices.DeleteFunc(w.change.Records, func(r RecordChange) bool { return r.ID == anchor.ID })
				delete(w.change.befores, anchor.ID)
			} else if after := &w.change.Records[len(w.change.Records)-1]; after.ID == anchor.ID {
				after.Before = whole(t, before)
				after.Changed = changedFields(before, nil)
				w.change.befores[anchor.ID] = before
			}
			w.change.Items = append(original.Items, w.change.Items...)
		}
		return merge.Canonical, w.retireIdentityReviews(sc, t, merge.Canonical, merge.Alias)
	}
	epochs, err := readEpochs(w.tx, t.ID)
	if err != nil {
		return "", err
	}
	for _, candidate := range candidates {
		if err := w.queueIdentityReview(sc, t, anchor, candidate.record, candidate.evidence, epochs.Mapping); err != nil {
			return "", err
		}
	}
	return anchor.ID, nil
}

func (w *writeTx) queueIdentityReview(sc Scope, t *Type, a, b *record, evidence MatchEvidence, epoch int64) error {
	if b.ID < a.ID {
		a, b = b, a
	}
	var present bool
	if err := w.tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM identity_matches
		WHERE left_id = ? AND right_id = ? AND decision = '')`, a.ID, b.ID).Scan(&present); err != nil || present {
		return err
	}
	title := "Are these the same person?"
	if t.Common == CommonOrganization {
		title = "Are these the same organization?"
	}
	evidence.Decision = MatchReview
	review := &IdentityCandidate{Left: a.ID, Right: b.ID, LeftRev: a.Rev, RightRev: b.Rev, Evidence: evidence}
	item, err := w.createWorkItem(sc, t, WorkItem{Title: title, Record: a.ID, Form: identityReviewForm, Identity: review})
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(`INSERT INTO identity_matches(id,type_id,left_id,right_id,left_rev,right_rev,evidence,mapping_epoch)
		VALUES(?,?,?,?,?,?,?,?)`, item.ID, t.ID, a.ID, b.ID, a.Rev, b.Rev, canonicalJSON(evidence), epoch)
	return err
}

// DecideIdentityMatch completes the review and changes the graph in the same
// write. A repeated decision returns the original merge, if there was one.
func (s *Store) DecideIdentityMatch(sc Scope, id string, keepSeparate bool, survivor string) (*IdentityMerge, error) {
	if err := allowIdentity(sc, s.draft != ""); err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, errorf(CodeInvalid, "choose an identity review ID")
	}
	var result *IdentityMerge
	err := s.write(func(w *writeTx) error {
		var err error
		result, err = w.decideIdentityMatch(sc, id, keepSeparate, survivor)
		return err
	})
	return result, err
}

func (w *writeTx) decideIdentityMatch(sc Scope, id string, separate bool, survivor string) (*IdentityMerge, error) {
	var savedDecision, savedMerge string
	err := w.tx.QueryRow(`SELECT decision, merge_id FROM identity_matches WHERE id = ?`, id).Scan(&savedDecision, &savedMerge)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errorf(CodeNotFound, "there is no identity review with that ID")
	}
	if err != nil {
		return nil, err
	}
	decision := "merge"
	if separate {
		decision = "separate"
		if survivor != "" {
			return nil, errorf(CodeInvalid, "keeping records separate needs no survivor")
		}
	}
	if savedDecision != "" {
		if savedDecision != decision {
			return nil, errorf(CodeStale, "the identity review already has a decision")
		}
		if savedMerge == "" {
			return nil, nil
		}
		merged := &IdentityMerge{ID: savedMerge}
		err := w.tx.QueryRow(`SELECT canonical, alias, canonical_rev FROM identity_merges WHERE id = ?`, savedMerge).
			Scan(&merged.Canonical, &merged.Alias, &merged.Revision)
		if err == nil && survivor != "" && survivor != merged.Canonical {
			return nil, errorf(CodeStale, "the identity review already chose a different survivor")
		}
		return merged, err
	}
	item, _, err := loadWorkItem(w.tx, w.schema, id)
	if err != nil {
		return nil, err
	}
	c, err := scanIdentityCandidate(w.schema, w.tx.QueryRow(`SELECT `+matchColumns+` FROM identity_matches WHERE id = ?`, id).Scan)
	if err != nil {
		return nil, err
	}
	var result *IdentityMerge
	if !separate {
		t, a, b, err := w.identityPair(c.Left, c.Right, c.LeftRev, c.RightRev)
		if err != nil {
			return nil, err
		}
		epochs, err := readEpochs(w.tx, c.typeID)
		if err != nil {
			return nil, err
		}
		if epochs.Mapping != c.epoch {
			return nil, errorf(CodeStale, "the mapping changed after the identity review")
		}
		switch survivor {
		case "", a.ID:
		case b.ID:
			a, b = b, a
		default:
			return nil, errorf(CodeInvalid, "the survivor must be one of the two records")
		}
		result, err = w.mergeIdentity(sc, t, a, b, c.Evidence)
		if err != nil {
			return nil, err
		}
		savedMerge = result.ID
	}
	if _, err := w.tx.Exec(`UPDATE identity_matches SET decision = ?, merge_id = ? WHERE id = ?`, decision, savedMerge, id); err != nil {
		return nil, err
	}
	if err := w.finishIdentityWorkItem(sc, item, c.typeID, decision, savedMerge); err != nil {
		return nil, err
	}
	if result != nil {
		return result, w.retireIdentityReviews(sc, w.schema.byID[c.typeID], result.Canonical, result.Alias)
	}
	return nil, nil
}

func (w *writeTx) finishIdentityWorkItem(sc Scope, item *WorkItem, typeID, decision, merge string) error {
	item.Status, item.UpdatedAt = WorkDone, w.now.UTC().Format(stamp)
	if decision == "superseded" {
		item.Status = WorkCancelled
	}
	item.Outcome = map[string]any{"decision": decision}
	if merge != "" {
		item.Outcome.(map[string]any)["merge"] = merge
	}
	if _, err := w.tx.Exec(`UPDATE work_items SET status = ?, outcome_json = ?, updated_at = ? WHERE id = ?`,
		item.Status, canonicalJSON(item.Outcome), item.UpdatedAt, item.ID); err != nil {
		return err
	}
	w.itemChange(sc, "work_item")
	return w.workItemEvent(EventWorkItemUpdated, w.schema.byID[typeID], item)
}

func (w *writeTx) completeIdentityWorkItem(sc Scope, item *WorkItem, outcome any) (*WorkItem, error) {
	if item.Status != WorkOpen {
		return nil, errorf(CodeStale, "this work item is %s", item.Status)
	}
	values, _ := outcome.(map[string]any)
	decision, _ := values["decision"].(string)
	survivor, _ := values["survivor"].(string)
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "merge":
		_, err := w.decideIdentityMatch(sc, item.ID, false, survivor)
		if err != nil {
			return nil, err
		}
	case "separate", "keep separate":
		_, err := w.decideIdentityMatch(sc, item.ID, true, survivor)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errorf(CodeInvalid, "choose merge or keep separate")
	}
	out, _, err := loadWorkItem(w.tx, w.schema, item.ID)
	return out, err
}

// retireIdentityReviews closes changed pairs before a fresh scan creates a
// new work item. A client holding the old ID cannot accept new evidence.
func (w *writeTx) retireIdentityReviews(sc Scope, t *Type, aID, bID string) error {
	var ids []string
	err := eachRow(w.tx, `SELECT m.id FROM identity_matches m
		LEFT JOIN records a ON a.id = m.left_id LEFT JOIN records b ON b.id = m.right_id
		LEFT JOIN type_epochs e ON e.type_id = m.type_id
		WHERE m.type_id = ? AND m.decision = '' AND (m.left_id IN (?, ?) OR m.right_id IN (?, ?))
		AND (a.id IS NULL OR b.id IS NULL OR a.deleted_at IS NOT NULL OR b.deleted_at IS NOT NULL
			OR a.rev <> m.left_rev OR b.rev <> m.right_rev OR COALESCE(e.mapping, 0) <> m.mapping_epoch)`,
		[]any{t.ID, aID, bID, aID, bID}, func(scan func(...any) error) error {
			var id string
			if err := scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
			return nil
		})
	if err != nil {
		return err
	}
	for _, id := range ids {
		item, _, err := loadWorkItem(w.tx, w.schema, id)
		if err != nil {
			return err
		}
		if _, err := w.tx.Exec(`UPDATE identity_matches SET decision = 'superseded' WHERE id = ?`, id); err != nil {
			return err
		}
		if err := w.finishIdentityWorkItem(sc, item, t.ID, "superseded", ""); err != nil {
			return err
		}
	}
	return nil
}
