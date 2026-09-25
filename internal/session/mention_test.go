package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestMentionDigestIsAttachedBoundedAndDoesNotWake(t *testing.T) {
	fixture := newTeamFixture(t, true)
	lonerDir := filepath.Join(filepath.Dir(filepath.Dir(fixture.manager)), "loner")
	if err := os.MkdirAll(lonerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	loner := filepath.Join(lonerDir, placeTranscript)
	reply := strings.Repeat("the parser said this. ", 40)
	body := strings.Join([]string{
		`{"type":"title","title":"side chat"}`,
		`{"type":"message","role":"user","content":"how is the port","timestamp":"2026-09-01T10:00:01Z"}`,
		`{"type":"message","role":"assistant","content":"` + reply + `","timestamp":"2026-09-01T10:00:02Z"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(loner, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		appendTraffic(t, fixture, teams.Entry{
			Kind: teams.KindNote, From: "parser", To: teams.ToEveryone,
			Text: strings.Repeat("traffic line. ", 20),
		})
	}

	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("noted"), nil
		},
	}}
	agent := teamAgent(t, fixture, fixture.manager, completer, nil)
	quiet := []string{fixture.parser, fixture.web, loner, teams.TrafficPath(fixture.profile, fixture.teamID)}
	beforeQuiet := map[string]string{}
	for _, path := range quiet {
		beforeQuiet[path] = string(mustRead(t, path))
	}
	said := "see ●harbor and @parser and @side-chat"
	note := agent.mentionNote(said)
	if note == "" {
		t.Fatal("the mention produced no digest")
	}
	if !strings.Contains(note, "Team reference (harbor)") || !strings.Contains(note, "@parser") || !strings.Contains(note, "side chat") {
		t.Fatalf("the digest is missing a reference:\n%s", note)
	}
	if strings.Contains(note, reply) {
		t.Fatal("the digest carried the whole reply")
	}
	for _, block := range strings.Split(note, "\n\n") {
		budget := mentionChatBudget
		if strings.HasPrefix(block, "Team reference") {
			budget = mentionTeamBudget + len("Team reference (harbor):\n")
		}
		if utf8.RuneCountInString(block) > budget {
			t.Fatalf("a reference ran to %d runes:\n%s", utf8.RuneCountInString(block), block[:80])
		}
	}
	for _, path := range quiet {
		if got := string(mustRead(t, path)); got != beforeQuiet[path] {
			t.Fatalf("building the digest wrote %s", path)
		}
	}

	collect(t, mustSubmit(t, agent, said))
	var model string
	for _, message := range agent.messages {
		text := messageContentText(message)
		if strings.Contains(text, "Team reference") {
			model = text
		}
	}
	if model == "" || !strings.Contains(model, said) {
		t.Fatal("the model was not handed the words and the digest")
	}
	journal, err := os.ReadFile(fixture.manager)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journal), said) || strings.Contains(string(journal), "Team reference") {
		t.Fatal("the journal did not keep the words without the digest")
	}
	for _, path := range quiet {
		if got := string(mustRead(t, path)); got != beforeQuiet[path] {
			t.Fatalf("sending the mention wrote %s", path)
		}
	}

	cut := mentionCut(strings.Repeat("あ", mentionChatBudget+40), mentionChatBudget)
	if utf8.RuneCountInString(cut) != mentionChatBudget {
		t.Fatalf("the chat budget cut to %d", utf8.RuneCountInString(cut))
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
