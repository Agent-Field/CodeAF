package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

func TestTeamsCardModelsFollowLiveAndSavedConversations(t *testing.T) {
	a, id, older, _ := trafficApp(t)
	a.pal = newPalette(tokens.TrueColor, false)
	team := mustTeam(t, a, id)
	a.model = "deepseek/deepseek-v4-flash-latest"
	older.SetModel("vendor/held-before")
	var held teamMember
	for _, member := range team.Members {
		if agent, live := a.wallAgentFor(member.Key); live && agent == older {
			held = member
		}
	}
	if held.Key == "" {
		t.Fatal("fixture has no held member")
	}
	paint := func() string {
		t.Helper()
		return plain(strings.Join(a.teamsMemberCards(&teamsDraw{a: a}, team, 180, 0), "\n"))
	}
	if text := paint(); !strings.Contains(text, "~"+a.model) || !strings.Contains(text, "~vendor/held-before") {
		t.Fatal(text)
	}
	older.SetModel("vendor/held-after")
	a.model = "vendor/front-after"
	if text := paint(); !strings.Contains(text, "~vendor/front-after") || !strings.Contains(text, "~vendor/held-after") || strings.Contains(text, "held-before") {
		t.Fatal(text)
	}
	delete(a.behind, held.Key)
	a.tp.world = map[string]session.SessionRow{held.File: {Model: "vendor/saved-before"}}
	if text := paint(); !strings.Contains(text, "~vendor/saved-before") {
		t.Fatal(text)
	}
	got := teamsGot{worldAsked: true, worldKnown: true, world: map[string]session.SessionRow{held.File: {Model: "vendor/saved-after"}}}
	a.teamsFold(got)
	if text := paint(); !strings.Contains(text, "~vendor/saved-after") || strings.Contains(text, "saved-before") {
		t.Fatal(text)
	}
	label := a.teamsConversationLabel("@picker", held.Key, held.File, 25)
	if plain(label) != "@picker · ~vendor/save..." || ansi.StringWidth(label) > 25 {
		t.Fatalf("narrow label = %q (%d cells)", plain(label), ansi.StringWidth(label))
	}
	if !strings.HasPrefix(label, a.pal.ink("@picker")) || !strings.Contains(label, a.pal.dim(" · ~vendor/save...")) {
		t.Fatalf("alias or model has the wrong ink: %q", label)
	}
}
