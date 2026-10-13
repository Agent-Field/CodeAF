// Package check validates app definitions before Preview. It implements svc.Check
// without changing the shared wire types. Service.Check also reads a definition
// directory and returns substep statuses and separate repair hints.
//
// Schema validation includes required files, plan targets, record relations and
// exactly one action per run step. The owner's wave-1 priority limits kit lint
// to known parts, renderable props and binding targets; labels, navigation count
// and the remaining visual laws are deferred. Render checks use svc.Sample,
// exercise every sample row and empty, single and multiple selections, resolve
// references and templates, and evaluate boolean CEL with bounded cost.
//
// Sample generation must work for a check to pass. Behaviour uses sample mode
// through svc.Runs and counts only completed fake runs. A stub or an asynchronous
// run is reported as skipped: the current seam has no bounded dry-run or stop
// operation, so starting a run cannot establish that its behaviour passed.
// Build substeps stream through BuildEvent.Log on app/<id>/build; the Builder
// retains ownership of the round's final status and spend.
package check
