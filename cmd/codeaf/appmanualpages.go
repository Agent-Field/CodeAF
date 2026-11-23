package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// SetManualPages is manual.corpus.set: the app's pages replace every earlier
// set in this process's manual, which the conversations it holds read through
// their manual tool. It answers the names the pages are filed under.
func (b *appBackend) SetManualPages(pages []appwire.ManualPage) ([]string, error) {
	set := make([]manual.AppPage, 0, len(pages))
	for _, page := range pages {
		set = append(set, manual.AppPage{Name: page.Name, Markdown: page.Markdown})
	}
	names, err := manual.SetAppPages(set)
	if err != nil {
		return nil, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	return names, nil
}
