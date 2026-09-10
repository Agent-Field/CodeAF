// LANE C STUB — deleted at merge; lane K owns the real type.
//
// The quick-task kernel (`quick/kernel`) lands `quickTaskSpec` in
// `internal/session/task_quick.go` with the fields below and the `done` slice
// the `items` tool ticks. This branch has to compile without it, so the shape
// the ceiling road writes is declared here and nowhere else. The integrator
// deletes this file when K is merged; nothing outside it names the type except
// the one field on [taskSpec] and checkpoint_quick.go.

package session

// quickTaskSpec is what a quick node is started on: one line saying what to do,
// an ordered list of items it works through, and the paths it claims.
type quickTaskSpec struct {
	line  string
	items []string
	files []string
}
