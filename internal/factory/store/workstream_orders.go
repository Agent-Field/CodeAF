package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── AN ORGANIZATION WORKSTREAM'S STANDING ORDERS, KEPT ─────────────────────
//
// One table in WorkstreamsDB beside the tasks: workstream_orders, one row per
// order, the order as JSON with when it next runs beside it (milliseconds,
// zero while it is off), so the server's timer finds what is due without
// decoding every order. CREATE IF NOT EXISTS on every open is this database's
// migration; deleting a workstream deletes its orders with it. The rules are
// factory/workstream_order.go and serve's workstream_orders.go: this file
// keeps rows, and every change is ONE transaction ([Store.EditWorkstreamOrders]).

const workstreamOrdersSchema = `
CREATE TABLE IF NOT EXISTS workstream_orders (
	seq     INTEGER PRIMARY KEY AUTOINCREMENT,
	ws      TEXT NOT NULL,
	id      TEXT NOT NULL UNIQUE,
	next_ms INTEGER NOT NULL DEFAULT 0,
	doc     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS workstream_orders_ws ON workstream_orders (ws, seq);
CREATE INDEX IF NOT EXISTS workstream_orders_next ON workstream_orders (next_ms);
`

// ErrWorkstreamOrderGone is an order not kept in the workstream named.
var ErrWorkstreamOrderGone = errors.New("standing order not found")

// ErrWorkstreamOrderChanged is a write made over an order changed since it
// was read.
var ErrWorkstreamOrderChanged = errors.New("the standing order changed since it was read")

// WorkstreamOrders are ws's orders, oldest first.
func (st *Store) WorkstreamOrders(ws string) ([]factory.WorkstreamOrder, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return []factory.WorkstreamOrder{}, err
	}
	return ordersIn(db, `ws = ? ORDER BY seq`, ws)
}

// DueWorkstreamOrders are the orders switched on whose next run is at or
// before now, across every workstream, soonest first.
func (st *Store) DueWorkstreamOrders(now time.Time) ([]factory.WorkstreamOrder, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return nil, err
	}
	return ordersIn(db, `next_ms > 0 AND next_ms <= ? ORDER BY next_ms`, now.UnixMilli())
}

// NextWorkstreamOrder is when the soonest order switched on next runs, zero
// when none will.
func (st *Store) NextWorkstreamOrder() (time.Time, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return time.Time{}, err
	}
	var next sql.NullInt64
	if err := db.QueryRow(`SELECT min(next_ms) FROM workstream_orders WHERE next_ms > 0`).Scan(&next); err != nil || !next.Valid {
		return time.Time{}, err
	}
	return time.UnixMilli(next.Int64), nil
}

func ordersIn(q rowQuerier, where string, args ...any) ([]factory.WorkstreamOrder, error) {
	rows, err := q.Query(`SELECT doc FROM workstream_orders WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []factory.WorkstreamOrder{}
	for rows.Next() {
		var doc string
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		var o factory.WorkstreamOrder
		if err := json.Unmarshal([]byte(doc), &o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// OrderWrite is one transaction over the orders, handed to the edit of
// [Store.EditWorkstreamOrders].
type OrderWrite struct{ tx *sql.Tx }

// Orders are ws's orders as this transaction sees them.
func (w OrderWrite) Orders(ws string) ([]factory.WorkstreamOrder, error) {
	return ordersIn(w.tx, `ws = ? ORDER BY seq`, ws)
}

// Order is order id of ws as this transaction sees it.
func (w OrderWrite) Order(ws, id string) (factory.WorkstreamOrder, error) {
	all, err := ordersIn(w.tx, `ws = ? AND id = ?`, ws, id)
	if err != nil {
		return factory.WorkstreamOrder{}, err
	}
	if len(all) == 0 {
		return factory.WorkstreamOrder{}, ErrWorkstreamOrderGone
	}
	return all[0], nil
}

// Put keeps o: a new order when rev is 0, else in place of the order of its
// id kept at rev.
func (w OrderWrite) Put(o factory.WorkstreamOrder, rev int) error {
	doc, err := json.Marshal(o)
	if err != nil {
		return err
	}
	next := int64(0)
	if at := o.NextAt(); o.On && !at.IsZero() {
		next = at.UnixMilli()
	}
	if rev == 0 {
		_, err = w.tx.Exec(`INSERT INTO workstream_orders (ws, id, next_ms, doc) VALUES (?, ?, ?, ?)`, o.WS, o.ID, next, string(doc))
		return err
	}
	cur, err := w.Order(o.WS, o.ID)
	if err != nil {
		return err
	}
	if cur.Rev != rev {
		return ErrWorkstreamOrderChanged
	}
	_, err = w.tx.Exec(`UPDATE workstream_orders SET next_ms = ?, doc = ? WHERE ws = ? AND id = ?`, next, string(doc), o.WS, o.ID)
	return err
}

// Delete takes order id of ws away.
func (w OrderWrite) Delete(ws, id string) error {
	res, err := w.tx.Exec(`DELETE FROM workstream_orders WHERE ws = ? AND id = ?`, ws, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrWorkstreamOrderGone
	}
	return nil
}

// EditWorkstreamOrders runs edit in one transaction under the workstreams'
// cross-process lock. An error from edit writes nothing.
func (st *Store) EditWorkstreamOrders(edit func(w OrderWrite) error) error {
	return st.wsTx(func(tx *sql.Tx) error { return edit(OrderWrite{tx: tx}) })
}
