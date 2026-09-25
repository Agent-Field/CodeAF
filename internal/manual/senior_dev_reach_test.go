package manual

import (
	"strings"
	"testing"
)

func TestSeniorDevInternetQuestionReachesItsOwnAnswer(t *testing.T) {
	for _, section := range Chat().Search("can senior-dev use the internet?", DefaultResults) {
		if section.Page != "senior-dev" || !strings.Contains(strings.ToLower(section.Title), "internet") {
			continue
		}
		for _, fact := range []string{"webfetch", "SENIOR_DEV_ENABLE_EXA", "SENIOR_DEV_ENABLE_PARALLEL", "models.dev"} {
			if !strings.Contains(section.Body, fact) {
				t.Errorf("internet answer missing %q", fact)
			}
		}
		return
	}
	t.Fatal("the internet question did not reach senior-dev's internet answer")
}
