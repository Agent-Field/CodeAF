package factory

import "reflect"

// UntouchedIssue is the reconciliation gate, checked on the row under its lock.
func UntouchedIssue(it Item) bool {
	return IsIssueJob(it) && it.Origin == OriginForge && it.Num > 0 && !it.PersonTouched &&
		(it.State == StateNew || it.State == StateNeedsYou) && it.DoneHow == "" && !it.PipelineDecided && JobUnworked(it)
}

// TouchIssue records personal ownership even when the person later undoes it.
func TouchIssue(it *Item) {
	it.PersonTouched = true
	it.SourceClosed = false
}

// RecordIssueTouch runs under the store lock. Forge refreshes and automatic
// close/reopen change none of these person-owned choices.
func RecordIssueTouch(before Item, after *Item) {
	edited := before.Cap != after.Cap || before.Model != after.Model || before.Effort != after.Effort ||
		before.Autonomy != after.Autonomy || before.Manual != after.Manual || before.Assignee != after.Assignee ||
		before.Marked != after.Marked || before.SetupHeld != after.SetupHeld ||
		!reflect.DeepEqual(before.Followers, after.Followers) || !reflect.DeepEqual(before.BriefSeen, after.BriefSeen) ||
		(before.SourceClosed && after.SourceClosed && before.DoneHow != after.DoneHow) || before.StopAfterStep != after.StopAfterStep ||
		!reflect.DeepEqual(before.Stages, after.Stages) || !reflect.DeepEqual(before.Priority, after.Priority) ||
		!reflect.DeepEqual(before.AutomaticModels, after.AutomaticModels)
	if edited || !JobUnworked(*after) || after.State == StateRunning || after.State == StateQueued {
		TouchIssue(after)
	}
}
