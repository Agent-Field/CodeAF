package draft

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

var templateRef = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

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
		if err := queryProblem(st.Records[rs.Type], rs.Query); err != nil {
			l.add(path+"/query", "%v (fields of %s: %s) — %s", err, rs.Type, fieldList(sortedKeys(st.Records[rs.Type].Fields)), spec.RunQueryRule)
		}
	}
}

// queryProblem parses a literal query against a record. A query that holds
// templates is parsed with each template replaced by a stand-in word and every
// field typed as text, because only the field names are known before it runs.
func queryProblem(rec *spec.RecordType, text string) error {
	if rec == nil || strings.HasPrefix(text, "$") {
		return nil
	}
	if strings.Contains(text, "{{") {
		rec = textOnly(rec)
		text = templateRef.ReplaceAllString(text, "sample")
	}
	_, err := query.Parse(rec, text, ownerID)
	return err
}

func textOnly(rec *spec.RecordType) *spec.RecordType {
	out := &spec.RecordType{Fields: map[string]*spec.Field{}}
	for name := range rec.Fields {
		out.Fields[name] = &spec.Field{Type: spec.FieldText}
	}
	return out
}
