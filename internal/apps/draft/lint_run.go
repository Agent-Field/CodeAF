package draft

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// lintRun returns the findings of one decoded run against the draft's
// records. Connectors, screens and the agent are not looked at here: they
// may be made later, and check and done judge them.
func lintRun(st State, id string, run *spec.Run) []string {
	l := &issueSink{file: "runs/" + id + ".yaml"}
	for _, name := range sortedKeys(run.Params) {
		p := run.Params[name]
		if (p.Type == "ref" || p.Type == "refs") && st.Records[p.To] == nil {
			l.add("/params/"+escapePointer(name)+"/to", "%q is not a record of this app (records: %s)", p.To, ids(sortedKeys(st.Records)))
		}
	}
	lintSteps(l, st, "/steps", run.Steps)
	return l.lines
}

func lintSteps(l *issueSink, st State, path string, steps []*spec.Step) {
	for i, step := range steps {
		at := fmt.Sprintf("%s/%d", path, i)
		for _, w := range []struct {
			key  string
			step *spec.RecordsStep
		}{{"records.query", step.RecordsQuery}, {"records.create", step.RecordsCreate}, {"records.update", step.RecordsUpdate}, {"records.delete", step.RecordsDelete}} {
			if w.step != nil {
				lintRecordStep(l, st, at+"/"+w.key, w.key, w.step)
			}
		}
		lintSteps(l, st, at+"/steps", step.Steps)
	}
}

func lintRecordStep(l *issueSink, st State, path, key string, rs *spec.RecordsStep) {
	if rs.Type == "" || strings.HasPrefix(rs.Type, "$") || strings.Contains(rs.Type, "{{") {
		return
	}
	if key == "records.create" || key == "records.update" {
		checkRecordWrite(l, st, rs.Type, rs.Set, path)
	} else if st.Records[rs.Type] == nil {
		l.add(path+"/type", "unknown record type %q (records: %s)", rs.Type, ids(sortedKeys(st.Records)))
		return
	}
	if key == "records.query" {
		if err := query.Problem(st.Records[rs.Type], rs.Query, ownerID); err != nil {
			l.add(path+"/query", "%v (fields of %s: %s) — %s", err, rs.Type, fieldList(sortedKeys(st.Records[rs.Type].Fields)), spec.RunQueryRule)
		}
	}
}
