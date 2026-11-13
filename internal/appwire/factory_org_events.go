package appwire

import "reflect"

// EventFactoryOrgChanged says a team or an organization workstream changed on
// an organization's server, so a window showing that organization reads it
// again now instead of polling. Its data is [FactoryOrgChanged].
//
// IT CARRIES WHERE, NOT WHAT, the same as [EventFactoryJobsChanged]: the
// connection, the server's own kind word and the id. The team or workstream is
// still read with factory.teams or factory.org_workstream, so nothing here can
// drift from the read, and a window looking at another context ignores it.
//
// The engine follows a connection's event stream (GET /v1/events) once that
// connection has answered an organization method, and relays exactly the
// three kinds the server publishes for teams and workstreams; the server has
// already left out a workstream this person cannot see. The stream ends when
// the connection is detached, its sign-in changes or is refused, or the engine
// stops; the next organization method follows it again.
const EventFactoryOrgChanged = "factory.org_changed"

// FactoryOrgChanged is one change on an organization's server. Connection is
// the factory connection id the org methods take. Kind is the server's word:
// "team.changed" (a team was made, renamed, deleted or its members changed;
// read factory.teams), "workstream.changed" (made, changed, moved owner or
// shares; read factory.org_workstream {id}, which may now answer not_found
// when it is no longer shared with you) or "workstream.removed" (archived or
// purged; drop it), "canvas.changed" (a canvas was made, written, shared or
// renamed; Seq is the seq it reached and By the handle who wrote it, so a
// window already at Seq reads nothing, else factory.org_canvas_ops since what
// it has) or "canvas.removed" (deleted, or no longer shared with you; drop
// it). An empty ID means the stream dropped and the server could
// not replay what was missed: every team, workstream or canvas of that kind may have
// changed, so read the whole list again.
type FactoryOrgChanged struct {
	Connection string `json:"connection"`
	Kind       string `json:"kind" enum:"team.changed,workstream.changed,workstream.removed,canvas.changed,canvas.removed"`
	ID         string `json:"id"`
	// Part, PartID, By, Rev and Deleted say which part of a workstream changed
	// ("page", "versions", "plan", "wakes", "suggestions", "jobs", "spend" or
	// "workstream"), which one, who changed it, its rev after, and whether it
	// was removed. By is the person the server stamped; all are empty on a resync,
	// and Part, PartID, Rev and Deleted are absent from a team's change and from
	// an older server, and a window that ignores them reads the whole workstream again.
	Part    string `json:"part,omitempty"`
	PartID  string `json:"part_id,omitempty"`
	By      string `json:"by,omitempty"`
	Rev     int    `json:"rev,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
	// Seq is the seq a canvas reached (canvas.changed only).
	Seq int64 `json:"seq,omitempty"`
}

func init() {
	eventShapes[EventFactoryOrgChanged] = reflect.TypeFor[FactoryOrgChanged]()
}
