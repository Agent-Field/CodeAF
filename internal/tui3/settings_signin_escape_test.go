package tui3

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestSettingsCodexRowEscapeCancelsItsBrowserFlow(t *testing.T) {
	a := modelServiceTestApp(t, t.TempDir(), "~deepseek/deepseek-v4-flash-latest", modelsource.NewSet(testDefaultService("fixture")), nil)
	flow := &panelCodexFlow{url: "https://auth.example/authorize?state=settings"}
	a.codexConnect = func(context.Context) (CodexFlow, error) { return flow, nil }
	previous := processOpener
	processOpener = func(string) error { return nil }
	t.Cleanup(func() { processOpener = previous })
	a.conns = &fakeConnections{}
	a.openSettings()
	toConnections(t, a)
	found := false
	for at, item := range a.sheet.items {
		if item.conn != nil && item.conn.service == modelConnectionID("codex") {
			a.sheet.cursor, found = at, true
			break
		}
	}
	if !found {
		t.Fatal("settings has no Codex service row")
	}
	_, begin := a.Update(key("enter"))
	if begin == nil {
		t.Fatal("Codex row did not start sign-in")
	}
	_, wait := a.Update(begin())
	if wait == nil || a.codexFlow != flow || !a.at(pageSettings) {
		t.Fatal("settings sign-in was not pending on its page")
	}
	a.Update(key("esc"))
	if !flow.cancelled || a.codexFlow != nil {
		t.Fatal("settings Escape left its browser flow alive")
	}
	if !a.at(pageSettings) {
		t.Fatal("cancelling the settings flow unexpectedly closed its page")
	}
}
