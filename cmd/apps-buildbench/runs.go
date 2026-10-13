package main

import "github.com/Agent-Field/codeaf/internal/apps/spec"

func (c *checker) checkRun(id string, run *spec.Run) {
	c.checkSteps("runs/"+id+".yaml", run.Steps)
}

func (c *checker) checkSteps(file string, steps []*spec.Step) {
	for _, step := range steps {
		for _, rs := range []*spec.RecordsStep{step.RecordsQuery, step.RecordsCreate, step.RecordsUpdate, step.RecordsDelete} {
			c.checkRecordsStep(file, step.ID, rs)
		}
		c.checkSteps(file, step.Steps)
	}
}

func (c *checker) checkRecordsStep(file, id string, rs *spec.RecordsStep) {
	if rs == nil {
		return
	}
	if !c.hasType(rs.Type) {
		c.fail(file, "step %s: unknown record type %q", id, rs.Type)
		return
	}
	for _, name := range sortedKeys(rs.Set) {
		if !c.hasField(rs.Type, name) {
			c.fail(file, "step %s: %s has no field %q", id, rs.Type, name)
		}
	}
}
