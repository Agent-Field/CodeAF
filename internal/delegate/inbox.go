package delegate

// The inbox: the one road from codeaf into a program while it works.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/env"
)

// A PROGRAM THAT LISTENS IS HANDED WORDS WHILE IT WORKS, AND SAYS WHEN IT HAS
// THEM.
//
// A program runs as a process of its own with its stdin closed, so everything
// codeaf told it used to be its brief, and a person watching it chase the wrong
// thing for an hour could only stop it. A program whose declaration says it
// listens ([Delegate.Listens]) is started with EnvInbox naming a file in its
// task's own record folder. codeaf appends one JSON line per message — the
// person's words from the task's page, or the conversation's `say` — and the
// program reads the lines it has not read yet at the points in its own work
// where a word can be taken in (senior-dev: before each call to its model).
//
// A FILE, NOT A PIPE, because the words are already on disk as the task's notes
// and a line appended to a file is there for a program that is busy for ten
// minutes in a test run: nothing blocks, nothing is lost to a full buffer, and
// the record folder keeps what was sent beside what was said.
//
// NOTHING IS DELIVERED UNTIL THE PROGRAM SAYS SO. The program writes a `heard`
// record naming the messages it put before its model, and only then does codeaf
// mark the note had. A program that stops reading — senior-dev once it has
// handed in and its tree is frozen — writes `inbox` with open false and why,
// and codeaf refuses further words with that reason instead of queueing them
// for nobody.

// EnvInbox names the inbox file in a listening program's environment.
const EnvInbox = "CODEAF_INBOX"

// InboxName is the inbox file's name in a task's record folder.
const InboxName = "delegate-inbox.jsonl"

// Who a message is from, as a program is told it.
const (
	FromPerson       = "person"
	FromConversation = "conversation"
	FromWorker       = "worker"
)

// inboxLineMax leaves room for a store's 32 KiB note to expand sixfold during
// JSON encoding, so every accepted note fits the reader's line bound.
const inboxLineMax = 1 << 20

// ErrInboxMessage distinguishes words the inbox cannot carry from an I/O
// failure, so one bad message does not block every later message.
var ErrInboxMessage = errors.New("the inbox cannot carry this message")

// Message is one line of the inbox.
type Message struct {
	ID   string `json:"id"`
	From string `json:"from"`
	Text string `json:"text"`
}

// AppendInbox adds one message to the inbox at path, whole, on one line.
func AppendInbox(path string, message Message) error {
	message.Text = strings.TrimSpace(message.Text)
	if message.ID == "" || message.Text == "" {
		return fmt.Errorf("%w: a message needs an id and words", ErrInboxMessage)
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(message); err != nil {
		return fmt.Errorf("%w: %v", ErrInboxMessage, err)
	}
	line := encoded.Bytes()
	if len(line) > inboxLineMax {
		return fmt.Errorf("%w: encoded line exceeds %d bytes", ErrInboxMessage, inboxLineMax)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(line); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// Listener is what a Host that can hand its program messages also is. A
// program asks for it with a type assertion on its Host, and a host that is not
// one — a test's, or a run started with no inbox — is a program nobody talks to.
type Listener interface {
	// Messages answers the messages not yet read, oldest first, and never
	// blocks: none is an empty answer.
	Messages() []Message
	// Heard tells codeaf the program put these messages before its model.
	Heard(ids []string)
	// CloseInbox tells codeaf the program reads no more messages, and why.
	CloseInbox(reason string)
}

// inbox reads a listening program's inbox from where it last stopped.
type inbox struct {
	mu     sync.Mutex
	path   string
	offset int64
	closed bool
}

// Messages reads every complete line past the last one read. A line still
// being written — no newline yet — is left for the next read.
func (in *inbox) Messages() []Message {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.closed {
		return nil
	}
	file, err := os.Open(in.path)
	if err != nil {
		return nil
	}
	defer file.Close()
	if _, err := file.Seek(in.offset, io.SeekStart); err != nil {
		return nil
	}
	reader := bufio.NewReaderSize(file, 4096)
	var messages []Message
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			// A LINE WITHOUT ITS NEWLINE IS STILL BEING WRITTEN, and nothing
			// past it has been read: the offset stays at its start.
			break
		}
		in.offset += int64(len(line))
		if len(line) > inboxLineMax {
			continue
		}
		var message Message
		if json.Unmarshal(line, &message) != nil || message.ID == "" || strings.TrimSpace(message.Text) == "" {
			continue
		}
		messages = append(messages, message)
	}
	return messages
}

// close stops reading and reports whether this call was the one that did.
func (in *inbox) close() bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	was := in.closed
	in.closed = true
	return !was
}

// inboxFromEnv is the inbox codeaf started this program with, nil for none.
func inboxFromEnv() *inbox {
	path := strings.TrimSpace(env.Get(EnvInbox))
	if path == "" {
		return nil
	}
	return &inbox{path: path}
}
