package factory

// UntouchedIssue is the reconciliation gate, checked on the row under its lock.
func UntouchedIssue(it Item) bool {
 return IsIssueJob(it) && it.Origin==OriginForge && it.Num>0 && !it.PersonTouched &&
  (it.State==StateNew || it.State==StateNeedsYou) && it.DoneHow=="" && !it.PipelineDecided && JobUnworked(it)
}
