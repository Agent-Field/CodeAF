package prompt

import (
	_ "embed"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/draft"
)

// appBuilderMarkdown is the teaching page for a Builder task that builds an
// app through `codeaf app` verbs (docs/apps/builder-loop/DESIGN.md section 10).
//
//go:embed appbuilder.md
var appBuilderMarkdown string

const crmScriptMarker = "{{crm-script}}"

var appBuilderOnce = sync.OnceValue(func() string {
	return strings.Replace(appBuilderMarkdown, crmScriptMarker, strings.TrimSpace(draft.CRMScript()), 1)
})

// AppBuilderPage is the page that goes into the task brief. The worked example
// is the draft package's own CRM script, the one its tests run against the
// golden CRM, so the page cannot drift from the verbs.
func AppBuilderPage() string { return appBuilderOnce() }
