package pipeline

import "github.com/Agent-Field/codeaf/internal/factory"

// pipelineNameSuggestion leaves valid custom names alone except when they
// are likely spellings of a built-in name. Normalization still decides which
// pipeline a spelling loads into.
func pipelineNameSuggestion(written string, kind factory.Kind) string {
	var name factory.Kind
	var jobs string
	switch {
	case kind == factory.KindIssue && written != string(kind):
		return "This spelling is read as `issue`, the pipeline issue jobs run. Did you mean `issue`?"
	case kind == factory.KindPR+"s":
		name, jobs = factory.KindPR, "pull request"
	case kind == factory.KindCI+"s":
		name, jobs = factory.KindCI, "CI"
	case oneNameEdit(string(kind), string(factory.KindIssue)):
		name, jobs = factory.KindIssue, "issue"
	default:
		return ""
	}
	return "This is read as a pipeline of your own and leaves the default steps for " + jobs + " jobs alone. Did you mean `" + string(name) + "`?"
}

// oneNameEdit checks for exactly one insertion, deletion or substitution.
// Pipeline names have already been checked to contain only ASCII.
func oneNameEdit(a, b string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b)-len(a) > 1 {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] == b[i] {
			continue
		}
		if len(a) == len(b) {
			return a[i+1:] == b[i+1:]
		}
		return a[i:] == b[i+1:]
	}
	return len(a) < len(b)
}
