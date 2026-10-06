package tui3

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

func TestTeamsPreviewPairsLatestPromptWithItsReplyAcrossManyChunks(t *testing.T) {
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	var saved strings.Builder
	say := func(role, text string) {
		fmt.Fprintf(&saved, "{\"type\":\"message\",\"role\":%q,\"content\":%q}\n", role, text)
	}
	say("user", "Old prompt")
	say("assistant", "Old reply")
	say("user", "Review this layout")
	for i := 0; i < 12; i++ {
		say("assistant", fmt.Sprintf("Reply section %d", i))
	}
	if err := os.WriteFile(file, []byte(saved.String()), 0600); err != nil {
		t.Fatal(err)
	}
	p := teamsReadPreview(file, teamsPreview{})
	if p.count != 2 || p.messages[0].text != "Review this layout" || !strings.HasPrefix(p.messages[1].text, "Reply section 0") || !strings.Contains(p.messages[1].text, "Reply section 11") || strings.Contains(p.messages[1].text, "Old reply") {
		t.Fatalf("wrong saved exchange: %+v", p)
	}
	if err := os.WriteFile(file, []byte(saved.String()+"{\"type\":\"message\",\"role\":\"user\",\"content\":\"New unanswered prompt\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p = teamsReadPreview(file, p)
	if p.count != 1 || p.messages[0].text != "New unanswered prompt" {
		t.Fatalf("stale reply paired with new prompt: %+v", p)
	}
}

func TestTeamsPreviewUsesChatColoursAndQuestionBorderWithoutChangingCardSize(t *testing.T) {
	a, id, _ := teamsHostedLab(t)
	a.pal = newPalette(tokens.TrueColor, false)
	a.width, a.height = 160, 48
	team := mustTeam(t, a, id)
	manager := team.Manager
	a.entries = []entry{{kind: entryUser, text: "Which layout?"}, {kind: entryAssistant, text: "Start with the wide layout.\n\n" + strings.Repeat("Later explanation.\n\n", 40)}}
	r := teamsCrewRow{key: manager, file: a.file, handle: "coordinator", title: "The Tab Bar's Counts", manager: true, word: "idle"}
	rows := a.teamsManagerCard(&teamsDraw{a: a}, team, r, 140, 0)
	text := strings.Join(rows, "\n")
	for _, want := range []string{a.pal.ink("@coordinator"), a.pal.dim(" (The Tab Bar's Counts)"), a.pal.accent(a.pal.youGlyph()) + a.pal.muted("Which layout?")} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing chat or metadata styling %q: %q", want, text)
		}
	}
	if !strings.Contains(plain(text), "Start with the wide layout.") || !strings.Contains(plain(text), "...") || strings.Contains(plain(text), "Recent messages") || strings.Contains(plain(text), "idle") {
		t.Fatal(plain(text))
	}
	if len(rows) != a.teamsManagerHeight() {
		t.Fatal("manager card resized")
	}
	r.asking = true
	a.frontWaits = true
	a.questions = []questionShown{{question: session.Question{Head: "Which layout needs approval?"}}}
	for _, width := range []int{12, 24, 40, 140} {
		alert := a.teamsManagerCard(&teamsDraw{a: a}, team, r, width, 0)
		if len(alert) != len(rows) || !strings.Contains(alert[0], a.pal.ask("╭─")) || !strings.Contains(plain(alert[0]), "?") || !strings.Contains(alert[len(alert)-1], a.pal.ask("╰"+strings.Repeat("─", width-2)+"╯")) {
			t.Fatalf("missing alert border at %d: %q", width, alert)
		}
		for _, row := range alert {
			if ansi.StringWidth(row) > width {
				t.Fatalf("alert exceeds %d: %q", width, row)
			}
		}
	}
	r.asking = false
	a.frontWaits, a.questions = false, nil
	resolved := a.teamsManagerCard(&teamsDraw{a: a}, team, r, 140, 0)
	if strings.Contains(resolved[0], a.pal.ask("╭─")) {
		t.Fatal("resolved card retained yellow frame")
	}
	// Escalated team questions need the same visible alert even when the
	// manager's own agent is at rest rather than on a permission question.
	a.tp.packets = []teamstore.Packet{{Origin: id, Team: teamstore.Person, State: teamstore.PacketOpen, Question: "Choose a layout"}}
	alert := a.teamsManagerCard(&teamsDraw{a: a}, team, r, 140, 0)
	if !strings.Contains(plain(alert[0]), "? Choose a layout") || !strings.Contains(alert[0], a.pal.ask("╭─")) {
		t.Fatal("team decision did not highlight its manager")
	}
}

func TestTeamsMemberAndGlobalPreviewsUseTheSameLatestExchange(t *testing.T) {
	a, id, _ := teamsHostedLab(t)
	a.width, a.height = 160, 48
	team := mustTeam(t, a, id)
	key := team.Manager
	var member teamMember
	for _, m := range team.Members {
		if m.Key != key {
			member = m
			break
		}
	}
	if member.Key == "" {
		t.Fatal("no member in fixture")
	}
	a.entries = nil
	a.tp.previews[member.Key] = teamsPreview{count: 2, messages: [teamsPreviewMessages]teamsPreviewMessage{{role: "user", text: "Member prompt"}, {role: "assistant", text: "Member reply begins.\n\n" + strings.Repeat("Later words.\n\n", 20)}}}
	if a.tp.world == nil {
		a.tp.world = map[string]session.SessionRow{}
	}
	delete(a.behind, member.Key)
	a.tp.world[member.File] = session.SessionRow{Live: true, Presence: session.SessionPresence{State: session.PresenceWaiting, Reason: "Review member work"}}
	d := &teamsDraw{a: a}
	cards := a.teamsMemberCards(d, team, 160, 0)
	text := plain(strings.Join(cards, "\n"))
	for _, want := range []string{a.pal.youGlyph() + "Member prompt", "Member reply begins.", "? Review member work"} {
		if !strings.Contains(text, want) {
			t.Fatalf("member missing %q: %s", want, text)
		}
	}
	// Releasing the ordinary manager role lets the same conversation take
	// the optional global role through the real teams store's assignment law.
	var rootID string
	if err := a.teamEdit(func(f *teamstore.File) error {
		f.Teams[teamstore.Index(f.Teams, id)].Manager = ""
		rootID = f.MakeRoot(a.now())
		m, _ := team.Member(key)
		if err := f.AddMember(rootID, m); err != nil {
			return err
		}
		return f.SetManager(rootID, key)
	}); err != nil {
		t.Fatal(err)
	}
	a.tp.previews[key] = teamsPreview{}
	a.entries = []entry{{kind: entryUser, text: "Global prompt"}, {kind: entryAssistant, text: "Global reply begins.\n\n" + strings.Repeat("Later global words.\n\n", 30)}}
	a.frontWaits = true
	a.questions = []questionShown{{question: session.Question{Head: "Review global work"}}}
	d = &teamsDraw{a: a}
	rows := a.teamsGlobalManagerCard(d, 120, 3)
	text = plain(strings.Join(rows, "\n"))
	for _, want := range []string{a.pal.youGlyph() + "Global prompt", "Global reply begins.", "? Review global work", "+ Add member", "Settings"} {
		if !strings.Contains(text, want) {
			t.Fatalf("global missing %q: %s", want, text)
		}
	}
	for _, target := range d.targets {
		if target.act == teamsActDeleteGlobalManager && (target.y != 4 || target.x0 != 115) {
			t.Fatalf("delete target moved: %+v", target)
		}
	}
}

func TestTeamsPreviewFindsPromptAndResponseBeginningBeyondOldTailBound(t *testing.T) {
	for _, chunks := range []int{1, 20} {
		t.Run(fmt.Sprint(chunks), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "transcript.jsonl")
			var data strings.Builder
			fmt.Fprintf(&data, "{\"type\":\"message\",\"role\":\"user\",\"content\":%q}\n", "Last human prompt")
			for i := 0; i < chunks; i++ {
				body := fmt.Sprintf("Response opening %d: ", i) + strings.Repeat("界\\\"quoted\n", teamsPreviewBytes/chunks)
				fmt.Fprintf(&data, "{\"type\":\"message\",\"role\":\"assistant\",\"content\":%q}\n", body)
			}
			if err := os.WriteFile(file, []byte(data.String()), 0600); err != nil {
				t.Fatal(err)
			}
			p := teamsReadPreview(file, teamsPreview{})
			if p.count != 2 || p.messages[0].text != "Last human prompt" || !strings.HasPrefix(p.messages[1].text, "Response opening 0:") || !p.messages[1].clipped || len(p.messages[0].text)+len(p.messages[1].text) > teamsPreviewBytes {
				t.Fatalf("long response lost its exchange or exceeded bound: count=%d prompt=%q response prefix=%q clipped=%v", p.count, p.messages[0].text, p.messages[1].text[:min(len(p.messages[1].text), 40)], p.messages[1].clipped)
			}
		})
	}
}

func TestTeamsPreviewHonoursReplayCutsAndLongNoteAuthorship(t *testing.T) {
	for _, scenario := range []struct{ name, journal, prompt, reply string }{
		{"rewind", "{\"type\":\"message\",\"role\":\"user\",\"content\":\"Prompt A\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"Reply A\"}\n{\"type\":\"message\",\"role\":\"user\",\"content\":\"Prompt B\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"Reply B\"}\n{\"type\":\"rewind\",\"dropped\":2}\n", "Prompt A", "Reply A"},
		{"compaction", "{\"type\":\"message\",\"role\":\"user\",\"content\":\"Discarded prompt\"}\n{\"type\":\"compaction\",\"window\":0}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"Autonomous update\"}\n", "", "Autonomous update"},
		{"note", "{\"type\":\"message\",\"role\":\"user\",\"content\":\"Actual human prompt\"}\n" + fmt.Sprintf("{\"type\":\"message\",\"role\":\"user\",\"content\":%q,\"note\":true}\n", strings.Repeat("Session wrote these words.\n", teamsPreviewBytes/8)) + "{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"Response\"}\n", "Actual human prompt", "Response"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "transcript.jsonl")
			if err := os.WriteFile(file, []byte(scenario.journal), 0600); err != nil {
				t.Fatal(err)
			}
			p := teamsReadPreview(file, teamsPreview{})
			if scenario.prompt != "" {
				if p.count != 2 || p.messages[0].text != scenario.prompt || p.messages[1].text != scenario.reply {
					t.Fatalf("wrong exchange: %+v", p)
				}
			} else if p.count != 1 || p.messages[0].role != "assistant" || p.messages[0].text != scenario.reply {
				t.Fatalf("crossed replay boundary: %+v", p)
			}
		})
	}
}

func TestTeamsCardFactsNeverDisplaceQualifiedModel(t *testing.T) {
	a, id, _ := teamsHostedLab(t)
	a.model = "vendor/model-with-a-long-qualified-name"
	key := a.frontTabKey()
	a.unreadChats = map[string]bool{key: true}
	r := teamsCrewRow{key: key, file: a.file, handle: "coordinator", title: "Conversation title", word: "working", independent: true}
	metadata := a.teamsCardMetadata(r, 32)
	if len(metadata) > 2 || !strings.Contains(plain(strings.Join(metadata, "\n")), "vendor/") {
		t.Fatalf("model disappeared: %q", metadata)
	}
	lines := []wallCardLine{{s: metadata[0]}, {s: metadata[1]}}
	rows := a.teamsConversationCard(&teamsDraw{a: a}, mustTeam(t, a, id), r, "", lines, 0, 0, 39)
	for _, word := range []string{"working", "independent", "unread"} {
		if !strings.Contains(plain(rows[0]), word) {
			t.Fatalf("fact vanished: %q", rows[0])
		}
	}
}
