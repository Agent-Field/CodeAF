package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestDirectOnlyPickerKeepsTheUnkeyedDefaultHeadingAndManual(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	server := sourcestub.New("fake-small")
	defer server.Close()
	if err := config.WriteSources(dir, []config.PersistedSource{{ID: "ollama", Written: "ollama", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	sources := config.ResolveSources(dir, "", server.URL())
	services := sources.All()
	if len(services) != 2 || services[0].Key != "" || services[1].Source.ID != "ollama" {
		t.Fatalf("direct-only registry lost the unkeyed default: %+v", services)
	}
	// The registry supplies Ollama's fixed local address. Only its HTTP peer is
	// replaced here, so the real catalog can answer without an installed model.
	services[1].Address, services[1].Source.Address = server.URL(), server.URL()
	sources = modelsource.NewSet(services...)
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: "ollama/fake-small", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	a := modelServiceTestAppWithAgent(t, dir, "ollama/fake-small", sources, []Model{{ID: config.DefaultModel}}, agent)
	installModelServiceShelf(a, dir)
	if _, err := a.serviceModelRefresh(t.Context(), services[1], nil); err != nil {
		t.Fatal(err)
	}
	a.openPicker()
	frame := plain(strings.Join(a.pick.rows(100, a.pick.height(100), a.pal, -1, func(string) string { return "" }), "\n"))
	defaultAt, directAt := strings.Index(frame, "openrouter"), strings.Index(frame, "ollama")
	if defaultAt < 0 || directAt <= defaultAt || !strings.Contains(frame, "fake-small") {
		t.Fatalf("direct-only picker lost its default-first headings:\n%s", frame)
	}
	page, ok := manual.Chat().Page("commands")
	if !ok || !strings.Contains(page, "even without its key") || !strings.Contains(page, "Ollama") {
		t.Fatal("manual does not explain why a direct-only picker retains both headings")
	}
}
