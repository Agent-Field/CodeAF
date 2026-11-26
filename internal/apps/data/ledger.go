package data

import "fmt"

// A LEDGER IS APPEND-ONLY. A type declared "ledger": true (stock moves,
// payments, time entries) refuses every update, delete and undo of its
// records: what was booked stays booked, and the history of the books is the
// books. A mistake is corrected by [Store.Reverse], which books a new entry
// with every number and amount negated, links the two, and leaves the
// original exactly as it was apart from the link.

// Reverse books the reversing entry of one ledger record. An entry is
// reversed once; a reversal is itself an entry and is never reversed.
func (s *Store) Reverse(sc Scope, typ, id, reason string) (*WriteResult, error) {
	if !validID(id) {
		return nil, errorf(CodeInvalid, "%q is not a record id", id)
	}
	if len(reason) > statementMax {
		return nil, errorf(CodeInvalid, "a reason is at most %d characters", statementMax)
	}
	var t *Type
	var made *record
	var batch string
	err := s.writeAs(sc, func(w *writeTx) error {
		made, batch = nil, ""
		var err error
		if t, err = w.schema.allow(sc, typ, true); err != nil {
			return err
		}
		if !t.Ledger {
			return &Error{Code: CodeLedger, Message: fmt.Sprintf("%s records are not a ledger; change or delete them instead", t.Name)}
		}
		orig, err := w.liveFor(sc, t, id)
		if err != nil {
			return err
		}
		if orig.Meta.ReversedBy != "" {
			return &Error{Code: CodeStale, Message: fmt.Sprintf("this %s entry was already reversed", t.Name)}
		}
		if orig.Meta.Reverses != "" {
			return &Error{Code: CodeLedger, Message: "a reversal is not reversed again; book a new entry"}
		}
		if sc.Cause == "" {
			sc.Cause = "reverse:" + id
		}
		if batch, err = w.newBatch(sc, OpReverse, t); err != nil {
			return err
		}
		at := w.now.UTC().Format(stamp)
		rev := &record{ID: w.s.ids.next(w.now), Type: t.Name, Rev: 1, Data: map[string]any{}, Meta: recordMeta{By: map[string]string{}, Reverses: id}, CreatedAt: at, UpdatedAt: at}
		kind := sc.kind(OpReverse)
		for name, v := range orig.Data {
			f := t.Fields[name]
			if f == nil || f.Computed != "" || f.Sequence != nil {
				continue
			}
			if n, ok := v.(float64); ok && (f.Type == TypeNumber || f.Type == TypeMoney) {
				v = -n
			}
			rev.Data[name] = v
			rev.Meta.By[name] = kind
		}
		if field := t.LedgerReversal; field != "" && t.Fields[field] != nil {
			rev.Data[field] = id
			rev.Meta.By[field] = kind
		}
		for _, name := range t.Order {
			if f := t.field(name); f != nil && f.Sequence != nil {
				n, err := w.nextNumber(t, f, rev.ID)
				if err != nil {
					return err
				}
				rev.Data[name] = n
			}
		}
		compute(t, rev, sortedKeys(rev.Data), w.schema.person)
		if err := w.checkRecord(t, rev, uniqueFields(t)); err != nil {
			return err
		}
		if err := w.save(t, rev, true, sc); err != nil {
			return err
		}
		if err := w.history(t, sc, OpReverse, reason, nil, rev); err != nil {
			return err
		}
		if err := w.assign(sc, t, rev, ""); err != nil {
			return err
		}
		linked := orig.clone()
		linked.Meta.ReversedBy = rev.ID
		linked.Rev++
		linked.UpdatedAt = at
		if err := w.save(t, linked, false, sc); err != nil {
			return err
		}
		if err := w.history(t, sc, OpReverse, reason, orig, linked); err != nil {
			return err
		}
		made = rev
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &WriteResult{Batch: batch, Rows: []Row{s.row(t, made, sc, false)}, Held: []Held{}}, nil
}
