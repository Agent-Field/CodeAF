package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const skillDoorWords = "how should I lint this repo?"
const skillDoorLead = "Skills suited to this message:"

// Person-facing words omit only this message's recorded injection. Nil indexes,
// pasted blocks and a changed suffix preserve every word of the model's copy.
func TestPersonWordsRequiresTheMessagesOwnSkillsMark(t *testing.T) {
	type personWordReader interface {
		personWords(ai.Message) string
	}
	var absent *presentationIndex
	if _, ok := any(absent).(personWordReader); !ok {
		t.Fatal("presentationIndex has no shared personWords projection")
	}
	block := "\n\nSkills suited to this message:\n- lint\nEarlier-listed skills win when two skills conflict."
	marked := func(text, injection string) (ai.Message, *presentationIndex) {
		message := textMessage("user", text)
		index := &presentationIndex{}
		index.remember(message, &messagePresentation{SkillsBlock: injection})
		return message, index
	}
	injected, injectionIndex := marked(skillDoorWords+block, block)
	pasted, pastedIndex := marked(skillDoorWords+block+block, block)
	changed, changedIndex := marked(skillDoorWords+block+"\nmore words", block)
	unmarked, unmarkedIndex := marked(skillDoorWords+block, "")
	for _, tc := range []struct {
		name    string
		message ai.Message
		index   *presentationIndex
		want    string
	}{
		{"nil index keeps pasted bytes", textMessage("user", skillDoorWords+block), nil, skillDoorWords + block},
		{"empty message is safe", ai.Message{Role: "user"}, &presentationIndex{}, ""},
		{"unknown occurrence keeps pasted bytes", textMessage("user", skillDoorWords+block), injectionIndex, skillDoorWords + block},
		{"empty mark keeps pasted bytes", unmarked, unmarkedIndex, skillDoorWords + block},
		{"recorded injection comes off", injected, injectionIndex, skillDoorWords},
		{"pasted copy stays", pasted, pastedIndex, skillDoorWords + block},
		{"recorded bytes must still be the suffix", changed, changedIndex, skillDoorWords + block + "\nmore words"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := messageContentText(tc.message)
			if got := any(tc.index).(personWordReader).personWords(tc.message); got != tc.want {
				t.Fatalf("person-facing words = %q, want %q", got, tc.want)
			}
			if messageContentText(tc.message) != before {
				t.Fatal("projecting the person's words changed the model's copy")
			}
		})
	}
}

func skillDoorAgent(t *testing.T, journal string, replies ...step) (*Agent, *scriptedCompleter) {
	t.Helper()
	brain := openTestBrain(t)
	activeSkill(t, brain, "tool:lint", "checks the lint rules for this repo", "/shelf/lint")
	completer := &scriptedCompleter{steps: replies}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Memory = brain
		config.SessionFile = journal
	})
	return agent, completer
}

func skillDoorReply(context.Context, []ai.Message) (*ai.Response, error) {
	return textResponse("run the lint check"), nil
}

func skillDoorSubmit(t *testing.T, agent *Agent, words string) {
	t.Helper()
	events, err := agent.Submit(context.Background(), words)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	for _, event := range collect(t, events) {
		if event.Kind == EventError {
			t.Fatalf("turn: %v", event.Err)
		}
	}
}

func skillDoorRequest(t *testing.T, completer *scriptedCompleter, words string) string {
	t.Helper()
	// Memory errands share the completer, so the person's message identifies
	// the main request without assuming an auxiliary call's position.
	for index := completer.requests() - 1; index >= 0; index-- {
		for _, message := range completer.request(index) {
			text := messageContentText(message)
			if message.Role == "user" && strings.HasPrefix(text, words) {
				return text
			}
		}
	}
	t.Fatalf("no provider request with the person's words %q", words)
	return ""
}

func skillDoorDisplayed(t *testing.T, entries []DisplayEntry) {
	t.Helper()
	found := false
	for _, entry := range entries {
		if entry.Role != "user" {
			continue
		}
		if strings.Contains(entry.Text, skillDoorLead) {
			t.Errorf("the person's message contains the injected block: %q", entry.Text)
		}
		found = found || entry.Text == skillDoorWords
	}
	if !found {
		t.Errorf("the transcript does not show exactly the person's words %q", skillDoorWords)
	}
}

// Rewinding hands back only the person's words. Resubmitting that draft keeps
// one block on the model's copy and only the typed words in the journal and store.
func TestRewindDraftKeepsThePersonsWordsWhenSkillsRide(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, completer := skillDoorAgent(t, journal, skillDoorReply, skillDoorReply)
	skillDoorSubmit(t, agent, skillDoorWords)
	first := skillDoorRequest(t, completer, skillDoorWords)
	if strings.Count(first, skillDoorLead) != 1 {
		t.Fatalf("the model's first copy must carry one block: %q", first)
	}
	var turn *RewindPoint
	for _, point := range agent.RewindPoints() {
		if point.Turn {
			point := point
			turn = &point
			break
		}
	}
	if turn == nil {
		t.Fatal("the submitted message has no rewind point")
	}
	if turn.Said != skillDoorWords {
		t.Errorf("rewind draft contains model context: %q", turn.Said)
	}
	removed, err := agent.RewindAt(turn.Index)
	if err != nil {
		t.Fatal(err)
	}
	skillDoorDisplayed(t, removed)
	skillDoorSubmit(t, agent, turn.Said)
	sent := skillDoorRequest(t, completer, skillDoorWords)
	if sent != first || strings.Count(sent, skillDoorLead) != 1 {
		t.Errorf("resubmitting the rewind draft changed the model's copy; blocks=%d: %q", strings.Count(sent, skillDoorLead), sent)
	}
	agent.chatlog.close()
	stored, err := agent.config.Memory.Messages(agent.threadID(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range stored {
		if message.Role == "user" {
			found = true
			if message.Body != skillDoorWords {
				t.Errorf("the store kept more than the person's words: %q", message.Body)
			}
		}
	}
	if !found {
		t.Error("the store has no record of the person's message")
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), skillDoorLead) {
		t.Error("resubmitting the rewind draft journaled the injected block")
	}
}

// Compaction re-journals only the person's words on both ordinary and steering
// paths. A fresh agent draws those words while the live model copy keeps its block.
func TestCompactedConversationKeepsThePersonsWordsWhenSkillsRide(t *testing.T) {
	for _, steered := range []bool{false, true} {
		name := "ordinary message"
		if steered {
			name = "steering message"
		}
		t.Run(name, func(t *testing.T) {
			journal := filepath.Join(t.TempDir(), "session.jsonl")
			agent, _ := skillDoorAgent(t, journal)
			agent.config.ContextWindow = 2000
			agent.contextWindow.Store(2000)
			user := userText(skillDoorWords)
			if steered {
				user.crossed = &SteerMark{Consumed: true, Landing: "stopped the reply here"}
			}
			agent.mu.Lock()
			agent.attachTurnSkillsLocked(&user)
			agent.recordUserLocked(user)
			if steered {
				// The window's original message carries the correction mark. The
				// projection must read it before replacing the content allocation.
				agent.file.mu.Lock()
				rememberSteer(agent.file.steers, user.message, *user.crossed)
				agent.file.mu.Unlock()
			}
			agent.recordLocked(textMessage("assistant", strings.Repeat("thinking about the parser. ", 100)+"oldest sweep"))
			agent.recordUserLocked(userText("the second question"))
			agent.recordLocked(textMessage("assistant", strings.Repeat("thinking about the parser. ", 100)+"newer sweep"))
			agent.recordUserLocked(userText("the third question"))
			agent.recordLocked(textMessage("assistant", "the short last word"))
			agent.mu.Unlock()
			changed, err := agent.compact(context.Background(), nil)
			if err != nil || !changed {
				t.Fatalf("real compaction did not run: changed=%v err=%v", changed, err)
			}
			skillDoorDisplayed(t, agent.Transcript())
			found := false
			for _, message := range agent.snapshot() {
				if message.Role == "user" && strings.HasPrefix(messageContentText(message), skillDoorWords) {
					found = true
					if strings.Count(messageContentText(message), skillDoorLead) != 1 {
						t.Fatal("compaction changed the live model copy's skills block")
					}
				}
			}
			if !found {
				t.Fatal("the compaction window did not keep the skill-carrying message")
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(journal)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), skillDoorLead) {
				t.Error("compaction journaled the model's skills block")
			}
			restored, _ := skillDoorAgent(t, journal)
			skillDoorDisplayed(t, restored.Transcript())
			if steered {
				for _, entry := range restored.Transcript() {
					if entry.Text == skillDoorWords && (entry.Steer == nil || !entry.Steer.Consumed || entry.Steer.Landing != user.crossed.Landing) {
						t.Error("compaction lost the original message's steering mark")
					}
				}
			}
		})
	}
}

// Why quotes only the person's opening words while the model's copy keeps the
// skills block that informed the turn.
func TestWhyQuotesThePersonsWordsWhenSkillsRide(t *testing.T) {
	agent, completer := skillDoorAgent(t, "", skillDoorReply)
	skillDoorSubmit(t, agent, skillDoorWords)
	if got := agent.Why(); !strings.HasPrefix(got, "> "+skillDoorWords+"\n\n") || strings.Contains(got, skillDoorLead) {
		t.Errorf("Why quotes more than the person's words: %q", got)
	}
	if strings.Count(skillDoorRequest(t, completer, skillDoorWords), skillDoorLead) != 1 {
		t.Fatal("the model's copy lost the skills block")
	}
}

// The conversation's title is drawn from the person's words, without skills
// context. The main model still receives its own copy with the block intact.
func TestTitleReadsThePersonsWordsWhenSkillsRide(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, completer := skillDoorAgent(t, journal, skillDoorReply)
	asks := make(chan string, 1)
	completer.aside = func(messages []ai.Message) (*ai.Response, bool) {
		if !isTitleCall(messages) {
			return nil, false
		}
		select {
		case asks <- userTextIn(messages):
		default:
		}
		return textResponse("repository lint rules and checks"), true
	}
	skillDoorSubmit(t, agent, skillDoorWords)
	select {
	case input := <-asks:
		if !strings.Contains(input, skillDoorWords) || strings.Contains(input, skillDoorLead) {
			t.Errorf("the title request carries more than the person's words: %q", input)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the title request never reached the provider")
	}
	if strings.Count(skillDoorRequest(t, completer, skillDoorWords), skillDoorLead) != 1 {
		t.Fatal("the main model's copy lost the skills block")
	}
}
