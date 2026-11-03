package run

import (
 "context"

 "github.com/Agent-Field/codeaf/internal/factory"
)

// SetStepBegin binds a leased job's synchronous step admission report.
// A refusal prevents the executor from starting. Nil removes the binding.
func (r *Runner) SetStepBegin(id int, begin func(context.Context, factory.StageReport) error) {
 if begin == nil {
  r.stepBegins.Delete(id)
  return
 }
 r.stepBegins.Store(id, begin)
}
