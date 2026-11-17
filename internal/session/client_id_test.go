package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// requestTexts is the text of the last user message of every request the
// completer saw, in order.
func requestTexts(c *scriptedCompleter) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var texts []string
	for _, seen := range c.seen {
		texts = append(texts, messageContentText(seen[len(seen)-1]))
	}
	return texts
}

func sendMarkedAndWait(t *testing.T, agent *Agent, message Submission) {
	t.Helper()
	events, err := agent.SubmitMessage(context.Background(), message)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
}

// A CLIENT ID IS ON THE ACCEPTED LINE, so a conversation reopened by a fresh
// session reads it back with no help from the process that sent it, while the
// model reads the same words as for a message that had none.
func TestAClientIDSurvivesARestartOnTheAcceptedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := answering()
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
	empty := ""
	sendMarkedAndWait(t, agent, Submission{Text: "run the tests", ClientID: "c-1"})
	sendMarkedAndWait(t, agent, Submission{Text: "legacy shaped"})
	sendMarkedAndWait(t, agent, Submission{Text: "attachments only", Said: &empty, ClientID: "c-3 ünï"})
	if got, want := requestTexts(completer), []string{"run the tests", "legacy shaped", "attachments only"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the model read %q, want %q", got, want)
	}
	check := func(where string, entries []DisplayEntry) {
		t.Helper()
		users := personEntries(entries)
		if len(users) != 3 {
			t.Fatalf("%s: %d person entries", where, len(users))
		}
		if p := users[0].Person; users[0].Text != "run the tests" || p == nil || p.ClientID != "c-1" || p.Authored || !ordinaryIdentity(withoutClientID(users[0]), false) {
			t.Fatalf("%s: first = %#v", where, users[0])
		}
		if users[1].Person != nil && users[1].Person.ClientID != "" {
			t.Fatalf("%s: a message with no id was given %q", where, users[1].Person.ClientID)
		}
		if p := users[2].Person; users[2].Text != "" || p == nil || p.ClientID != "c-3 ünï" || !p.Authored {
			t.Fatalf("%s: third = %#v", where, users[2])
		}
	}
	check("live", agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	check("at rest", ReadTranscript(path).Entries)
	reopened, _ := newTestAgent(t, answering(), func(c *Config) { c.SessionFile = path })
	check("reopened", reopened.Transcript())
	if marks := journalMarks(t, path); marks[0]["client_id"] != "c-1" || marks[1]["client_id"] != nil {
		t.Fatalf("the journal holds %v", marks)
	}
}

func withoutClientID(entry DisplayEntry) DisplayEntry {
	person := *entry.Person
	person.ClientID = ""
	entry.Person = &person
	return entry
}

// THE COMPOSED ROAD KEEPS THE SAME ID ON THE SAME KIND OF LINE.
func TestAClientIDRidesAComposedSubmissionToo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, answering(), func(c *Config) { c.SessionFile = path })
	receipt, events, err := agent.SubmitComposed(context.Background(), Submission{Text: "composed words", ClientID: "c-9"})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	users := personEntries(ReadTranscript(path).Entries)
	if len(users) != 1 || users[0].Text != "composed words" || users[0].Person == nil || users[0].Person.ClientID != "c-9" ||
		users[0].Person.Authored || users[0].Person.Composed == nil || users[0].Person.Composed.MessageID != receipt.MessageID {
		t.Fatalf("the composed line reads %#v (receipt %#v)", users, receipt)
	}
}

// A BAD ID REFUSES THE MESSAGE WHOLE, BEFORE IT IS ACCEPTED; a repeated one is
// no more than a repeat — both messages run.
func TestAClientIDThatDoesNotFitRecordsNothingAndARepeatIsNotARefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := answering()
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
	for name, id := range map[string]string{
		"too long": strings.Repeat("i", MaxClientIDBytes+1),
		"not utf8": "bad\xff",
		"nul":      "a\x00b",
		"newline":  "a\nb",
	} {
		if _, err := agent.SubmitMessage(context.Background(), Submission{Text: "x", ClientID: id}); !errors.Is(err, ErrMessageMark) {
			t.Fatalf("%s: SubmitMessage = %v", name, err)
		}
		if _, _, err := agent.SubmitComposed(context.Background(), Submission{Text: "x", ClientID: id}); !errors.Is(err, ErrMessageMark) {
			t.Fatalf("%s: SubmitComposed = %v", name, err)
		}
	}
	if asked := len(requestTexts(completer)); asked != 0 || len(personEntries(agent.Transcript())) != 0 {
		t.Fatalf("a refused id reached the model %d times or the record", asked)
	}
	exact := strings.Repeat("é", MaxClientIDBytes/2)
	sendMarkedAndWait(t, agent, Submission{Text: "one", ClientID: exact})
	sendMarkedAndWait(t, agent, Submission{Text: "two", ClientID: exact})
	users := personEntries(agent.Transcript())
	if len(users) != 2 || users[0].Person.ClientID != exact || users[1].Person.ClientID != exact {
		t.Fatalf("a repeated id was not taken as given: %#v", users)
	}
}

// THE ID IS PART OF WHAT A RECORD COSTS.
func TestAClientIDIsCountedInWhatARecordCosts(t *testing.T) {
	bare := []DisplayEntry{{Role: "user", Person: &PersonMark{}}}
	named := []DisplayEntry{{Role: "user", Person: &PersonMark{ClientID: strings.Repeat("i", 200)}}}
	if diff := entriesCost(named) - entriesCost(bare); diff != 200 {
		t.Fatalf("an id of 200 bytes costs %d", diff)
	}
	if got := cloneEntries(named)[0].Person.ClientID; got != named[0].Person.ClientID {
		t.Fatalf("a cloned entry lost its id: %q", got)
	}
}
