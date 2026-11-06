package session

// request_secret asks the person for a credential through a masked box, and the
// model never holds the value:
//
//   ? Repository-scoped runner credential
//     Create a fine-grained GitHub token for codeaf-desk only …
//     [ •••••••••••••••••••••••• 93 ]   Save securely   Decline
//     Kept out of chat, in a private file this agent's commands can read for 24 hours
//
// WHAT THE MODEL GETS BACK IS A PATH. The typed value is written to a file only
// its owner can read, in codeaf's own state folder and never in the workspace (a
// workspace is committed, synced and shared; the state folder is none of those),
// and the call returns the path, the length and the hour it stops being
// readable. A command reads it as "$(cat <path>)", so the key reaches the
// program that needs it and never the transcript, the journal, the record, a
// `--host` frame or the provider.
//
// AND IF A COMMAND PRINTS IT ANYWAY, IT IS TAKEN OUT. The value is registered
// with internal/redact the moment it is kept, and every tool result passes
// through redact.Secrets before any copy of it is made (loop.go's
// [Agent.finishToolResult]), so `echo "$(cat …)"` hands the model `[redacted]`.
// A restart forgets that registration, which is why every file still inside its
// day is registered again when this engine builds its first belt.
//
// IT IS THE `ask` LANE, NOT A SECOND ONE. The question is an ordinary
// clarification with [InputShape.Secret] set, so every surface that already
// masks a secret box (tui3's secretDraftBlock, the app's dock) draws it with
// nothing new to learn, and `ask` with `input.secret` gets the same file rather
// than the key in its answer — which is what it got before this file existed.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// RequestSecretToolName is the verb, spelled once: the belt, the shelf, the
// page and the manual gate all read it from here.
const RequestSecretToolName = "request_secret"

// SecretKeepFor is how long a kept credential stays readable. It is a day
// because the work that asks for a token is the work in front of the person
// now; a credential that has to outlive that belongs in the tool that owns it
// (a keychain, `gh auth`, a provider's own settings), not in a file an agent
// can read.
const SecretKeepFor = 24 * time.Hour

// SecretDeclineKey is the one answer a secret question offers besides the box.
// It is a digit like every other answer key, so a hand that learned the keys on
// another card is right here too.
const SecretDeclineKey = "1"

// SecretDeclineLabel is that answer's word, as the person reads it.
const SecretDeclineLabel = "Decline"

// SecretsDirName is the folder under codeaf's state root that holds kept
// credentials, each in a file of its own.
const SecretsDirName = "secrets"

// secretRegisterFloor is the shortest value registered for redaction. A
// two-character "secret" would take every `ab` out of every tool result for the
// rest of the process, which is a broken transcript and not a protected key.
const secretRegisterFloor = 6

const requestSecretDescription = "Ask the person for a credential (token, password, API key) through a masked box. " +
	"The value never enters this conversation: you get a private file path your commands can read for 24 hours. " +
	"Never ask for a credential in chat or with `ask`."

const requestSecretSchema = `{"type":"object","properties":{` +
	`"label":{"type":"string","description":"What it is, as a heading"},` +
	`"why":{"type":"string","description":"Exactly how to create it (owner, scope, permissions) and what you will use it for"},` +
	`"placeholder":{"type":"string","description":"Words inside the empty box"}` +
	`},"required":["label","why"],"additionalProperties":false}`

// KeptSecret is where a credential went: the file, when it stops being
// readable, and how many characters arrived — the one fact a person pasting a
// key wants confirmed. Failed is why it could not be kept, and is "" when it
// was.
type KeptSecret struct {
	File       string    `json:"file,omitempty"`
	Until      time.Time `json:"until,omitzero"`
	Characters int       `json:"characters,omitempty"`
	Failed     string    `json:"failed,omitempty"`
}

func (a *Agent) requestSecretTool() bare.Tool {
	rememberLiveSecrets()
	return bare.Tool{Name: RequestSecretToolName, Description: requestSecretDescription, Schema: json.RawMessage(requestSecretSchema), Execute: a.executeRequestSecret}
}

func (a *Agent) executeRequestSecret(ctx context.Context, raw json.RawMessage) (string, bool, error) {
	var in struct {
		Label       string `json:"label"`
		Why         string `json:"why"`
		Placeholder string `json:"placeholder"`
	}
	if err := decodeToolArguments(raw, &in); err != nil {
		return rejectToolArguments(ctx, "Invalid arguments: "+err.Error()), true, nil
	}
	label, why := strings.TrimSpace(in.Label), strings.TrimSpace(in.Why)
	if label == "" || why == "" {
		return askRefusedLead + "a secret request needs a label and a why: say what it is, how to create it and what it is for", true, nil
	}
	return a.raiseAsk(ctx, askArguments{
		Head: label, Kind: AskClarification, Reason: why, Stakes: StakesReversible,
		Options: []AnswerOption{{Key: SecretDeclineKey, Label: SecretDeclineLabel, Safe: true, Consequence: "Nothing is saved"}},
		Input:   InputShape{Kind: InputText, Prompt: strings.TrimSpace(in.Placeholder), Secret: true},
	})
}

// keepSecretAnswer is one answer to a secret question as the model's lane may
// carry it: what was typed is written to a file and gone from the answer, and
// the answer says where it went.
//
// EVERY TYPED FIELD IS READ AND EVERY TYPED FIELD IS EMPTIED, on
// [withoutSecretWords]'s terms: a person pasting a key has no idea which box
// the engine reads it from. AND NOTHING TYPED INTO A SECRET BOX IS ASKING BACK:
// a surface that offers "ask back" beside a masked box would send the key as a
// question to the model, so typed words always end the question here.
func keepSecretAnswer(q Question, answer Answer) Answer {
	typed := secretTyped(answer)
	answer.Change, answer.Reframe, answer.Why = "", "", ""
	answer.Comments, answer.Blanks, answer.AskedBack = nil, nil, nil
	answer.Clarify = false
	if typed == "" {
		return answer
	}
	answer.Key, answer.Picked, answer.Labels = "", nil, nil
	kept, err := keepSecret(q.Head, typed, time.Now())
	if err != nil {
		kept = KeptSecret{Failed: err.Error()}
	}
	answer.Kept = &kept
	return answer
}

// secretTyped is the first non-empty thing the person typed, in the order a
// surface is likeliest to have put it.
func secretTyped(answer Answer) string {
	for _, text := range []string{answer.Change, answer.Reframe, answer.Why} {
		if text = strings.TrimSpace(text); text != "" {
			return text
		}
	}
	labels := make([]string, 0, len(answer.Blanks))
	for label := range answer.Blanks {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		if text := strings.TrimSpace(answer.Blanks[label]); text != "" {
			return text
		}
	}
	for _, exchange := range answer.AskedBack {
		if text := strings.TrimSpace(exchange.Asked); text != "" {
			return text
		}
	}
	return ""
}

// secretAnswerText is what the model is told, and it is the WHOLE of what it
// is told: no answer JSON follows, because the JSON is where typed words live.
func secretAnswerText(q Question, answer Answer) string {
	head := strings.TrimSpace(q.Head)
	switch {
	case answer.Kept != nil && answer.Kept.Failed != "":
		return fmt.Sprintf("not saved · %s · %s · they typed it, but it could not be kept: %s. Say so and ask again only if they want to.",
			askQuestionWord(q), head, answer.Kept.Failed)
	case answer.Kept != nil:
		k := answer.Kept
		return fmt.Sprintf("saved · %s · %s · %s · %d characters, readable until %s.\n"+
			"Use it only inside a command, as \"$(cat %s)\". Never print, echo or log it, never copy it into the workspace, a commit or a message, and never ask for it in chat. "+
			"A command's output that contains it comes back to you as [redacted].",
			askQuestionWord(q), head, k.File, k.Characters, k.Until.Local().Format("Mon 15:04 MST"), k.File)
	default:
		return fmt.Sprintf("declined · %s · %s · they chose not to give it. Carry on without it, and ask again only if they bring it up.",
			askQuestionWord(q), head)
	}
}

// secretsDir is where kept credentials live.
func secretsDir() string { return filepath.Join(home.Dir(), SecretsDirName) }

var errSecretTooLong = errors.New("it is longer than 64 KiB, which no credential is")

// keepSecret writes one credential to a file of its own and registers it for
// redaction. The folder is 0700 and the file 0600, created exclusively, so
// nobody but this user reads it and no earlier file is ever overwritten.
func keepSecret(label, value string, now time.Time) (KeptSecret, error) {
	if len(value) > 64<<10 {
		return KeptSecret{}, errSecretTooLong
	}
	dir := secretsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return KeptSecret{}, err
	}
	// MkdirAll leaves an existing folder as it was, so a folder somebody made
	// readable is made private again before anything is put in it.
	if err := os.Chmod(dir, 0o700); err != nil {
		return KeptSecret{}, err
	}
	sweepSecrets(now)
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return KeptSecret{}, err
	}
	path := filepath.Join(dir, secretSlug(label)+"-"+hex.EncodeToString(random[:]))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return KeptSecret{}, err
	}
	if _, err := file.WriteString(value); err != nil {
		file.Close()
		os.Remove(path)
		return KeptSecret{}, err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return KeptSecret{}, err
	}
	registerSecret(value)
	// THE DAY IS KEPT BY THIS PROCESS AS WELL AS BY THE SWEEP. The sweep runs
	// when the next secret is kept or the next engine starts, which may be
	// never; the timer is what makes "for 24 hours" true of an engine that is
	// still running. A file removed by hand before then is nothing to remove.
	time.AfterFunc(SecretKeepFor, func() { os.Remove(path) })
	return KeptSecret{File: path, Until: now.Add(SecretKeepFor), Characters: len([]rune(value))}, nil
}

// secretSlug is the label as a file name: lower-case letters and digits joined
// by single dashes, at most forty characters, "secret" when nothing is left.
func secretSlug(label string) string {
	var out strings.Builder
	dash := false
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if dash && out.Len() > 0 {
				out.WriteByte('-')
			}
			dash = false
			out.WriteRune(r)
		default:
			dash = true
		}
		if out.Len() >= 40 {
			break
		}
	}
	if out.Len() == 0 {
		return "secret"
	}
	return strings.TrimRight(out.String(), "-")
}

// sweepSecrets removes every kept credential past its day. Every failure is
// silence: a file that would not go is tried again next time.
func sweepSecrets(now time.Time) {
	entries, err := os.ReadDir(secretsDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) >= SecretKeepFor {
			os.Remove(filepath.Join(secretsDir(), entry.Name()))
		}
	}
}

var rememberSecretsOnce sync.Once

// rememberLiveSecrets sweeps the folder once per process and registers every
// credential still inside its day, so a restart does not leave a kept key
// printable by the next `cat`.
func rememberLiveSecrets() {
	rememberSecretsOnce.Do(func() {
		now := time.Now()
		sweepSecrets(now)
		entries, err := os.ReadDir(secretsDir())
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(secretsDir(), entry.Name())
			if info, err := entry.Info(); err == nil {
				time.AfterFunc(SecretKeepFor-now.Sub(info.ModTime()), func() { os.Remove(path) })
			}
			if data, err := os.ReadFile(path); err == nil {
				registerSecret(string(data))
			}
		}
	})
}

func registerSecret(value string) {
	if value = strings.TrimSpace(value); len(value) >= secretRegisterFloor {
		redact.Register(value)
	}
}
