package prompt

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/draft"
)

func TestAppBuilderPageInlinesTheScript(t *testing.T) {
	page := AppBuilderPage()
	if strings.Contains(page, crmScriptMarker) {
		t.Fatal("the CRM script marker was not replaced")
	}
	if !strings.Contains(page, "codeaf app done") || !strings.Contains(page, "<<'JSON'") {
		t.Fatal("the page does not hold the CRM script")
	}
}

// Every verb a worker may type is named on the page, so a new verb cannot ship
// without the worker being taught it.
func TestAppBuilderPageNamesEveryVerb(t *testing.T) {
	page := AppBuilderPage()
	for _, usage := range draft.Usages() {
		verb := "codeaf app " + strings.Fields(usage)[2]
		if !strings.Contains(page, verb) {
			t.Errorf("the page does not name %q", verb)
		}
	}
}

func TestAppBuilderPageQuotesTheVerbLimits(t *testing.T) {
	page := AppBuilderPage()
	if strings.Contains(page, "{{plan-line-max}}") || strings.Contains(page, "{{name-max}}") {
		t.Fatal("a limit placeholder was not replaced")
	}
	for _, want := range []string{
		"at most " + strconv.Itoa(draft.MaxPlanLine) + " characters",
		"name is at most " + strconv.Itoa(draft.MaxNameLen) + " characters",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
}
