package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F4, THE TORN TAIL. A process that died mid-line leaves a fragment with no
// newline. The replay skipped it, as it should — but the next line the journal
// wrote landed glued to the fragment and was skipped with it, so a sound line
// was lost to a broken one. The fragment is set aside now, and the next line
// is its own.
func TestATornJournalTailDoesNotSwallowTheNextLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	journal, _, err := openSessionFile(path, "/lab", "test/model", "")
	if err != nil {
		t.Fatal(err)
	}
	journal.appendMessage(textMessage("user", "before the tear"))
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	// The process dies halfway through writing the model's answer.
	const fragment = `{"type":"message","message":{"role":"assistant","content":[{"type":"te`
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(fragment); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	// The next life opens it and says something.
	journal, _, err = openSessionFile(path, "/lab", "test/model", "")
	if err != nil {
		t.Fatal(err)
	}
	journal.appendMessage(textMessage("user", "after the tear"))
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	// And the life after that reads both sound lines.
	journal, replayed, err := openSessionFile(path, "/lab", "test/model", "")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	var said []string
	for _, message := range replayed.messages {
		said = append(said, messageContentText(message))
	}
	if strings.Join(said, "|") != "before the tear|after the tear" {
		t.Fatalf("the journal replays %q, want both sound lines", said)
	}
	if torn, err := os.ReadFile(path + tornSuffix); err != nil || strings.TrimSpace(string(torn)) != fragment {
		t.Fatalf("the fragment was not kept aside: %q (%v)", torn, err)
	}
}
