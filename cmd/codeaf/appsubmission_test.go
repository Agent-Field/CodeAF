package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// composingConversation is the fake conversation with the door a message that
// carries pictures goes through, as an engine that says it takes them has.
type composingConversation struct {
	*fakeConversation
	known    bool
	refuse   error
	composed []session.Submission
}

func (c *composingConversation) ComposedKnown() bool { return c.known }

func (c *composingConversation) SubmitComposed(ctx context.Context, message session.Submission) (session.SubmissionReceipt, <-chan session.Event, error) {
	c.mu.Lock()
	refuse := c.refuse
	if refuse == nil {
		c.composed = append(c.composed, message)
	}
	c.mu.Unlock()
	if refuse != nil {
		return session.SubmissionReceipt{}, nil, refuse
	}
	events, err := c.fakeConversation.Submit(ctx, message.Text)
	if len(message.Media) == 0 {
		return session.SubmissionReceipt{MessageID: "msg_0123456789abcdef01234567", Delivery: session.DeliveryText}, events, err
	}
	width, height := 40, 30
	return session.SubmissionReceipt{
		MessageID: "msg_0123456789abcdef01234567", Delivery: session.DeliveryDescription, SeenBy: "vendor/seer",
		Media: []session.MessageMedia{
			{ID: "at1", Path: message.Media[0].Path, MIME: "image/png", Bytes: 99, SHA256: "ab", Width: width, Height: height, Role: session.MediaRoleOriginal},
			{ID: "at2", Path: message.Media[1].Path, MIME: "image/png", Bytes: 77, SHA256: "cd", Width: width, Height: height, Role: session.MediaRoleMarked},
		},
	}, events, err
}

func (c *composingConversation) sent() []session.Submission {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]session.Submission(nil), c.composed...)
}

func composingBackend(t *testing.T, conv appConversation) (*appBackend, *appRecorder, appwire.Agent, string, []string) {
	t.Helper()
	backend, rec, agent := markBackend(t, conv)
	for _, name := range []string{"a.png", "b.png"} {
		if err := os.WriteFile(filepath.Join(agent.Workspace, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var ids []string
	for _, name := range []string{"a.png", "b.png"} {
		chip, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachFile, Path: name}, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, chip.ID)
	}
	return backend, rec, agent, agent.Workspace, ids
}

// submissionFor is the wire's submission for two staged chips: the original and
// its marked copy, and three marks.
func submissionFor(ids []string) *appwire.MessageSubmission {
	area := 6.0
	return &appwire.MessageSubmission{
		Media: []appwire.SubmissionMedia{{ID: ids[0], Source: &appwire.MediaReference{SourceID: "src_1"}}, {ID: ids[1]}},
		Annotation: &appwire.Annotation{
			Original: ids[0], Marked: ids[1], MarksPath: "/w/a.marks.json", ChangeOnlyMarked: true,
			Marks: []appwire.AnnotationMark{
				{Kind: "pin", Note: "warmer", At: &appwire.Point{X: 24, Y: 18}},
				{Kind: "box", From: &appwire.Point{X: 70, Y: 56}, To: &appwire.Point{X: 82, Y: 76}},
				{Kind: "shade", Shade: &appwire.Shade{Points: []appwire.Point{{X: 1, Y: 2}}, BrushWidth: 4, AreaPercent: &area}},
			},
		},
	}
}

// ONE SEND IS ONE MESSAGE: the engine is handed the pictures — resolved from
// what the person staged, never from a path the wire carried — and the marks
// together with the words, the entry shows the engine's id, delivery and
// measurements, and the chips go only once the message was kept.
func TestAppSendComposedHandsTheEngineOneMessageAndShowsWhatItKept(t *testing.T) {
	conv := &composingConversation{fakeConversation: &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}, known: true}
	backend, rec, agent, workspace, ids := composingBackend(t, conv)

	said := "make the sky warmer"
	receipt, err := backend.SendComposed(agent.ID, appwire.AgentMessage{
		Text: "redo this", Said: &said, ClientID: "m-1", Attachments: ids, Submission: submissionFor(ids),
	})
	if err != nil {
		t.Fatalf("SendComposed: %v", err)
	}
	if receipt.MessageID != "msg_0123456789abcdef01234567" || receipt.Delivery != appwire.DeliveryDescription || receipt.SeenBy != "vendor/seer" || receipt.Turn == 0 {
		t.Fatalf("receipt = %+v", receipt)
	}
	sent := conv.sent()
	if len(sent) != 1 || len(conv.submissions) != 0 {
		t.Fatalf("the engine took %d composed and %d plain messages, want exactly one composed", len(sent), len(conv.submissions))
	}
	got := sent[0]
	if got.Media[0].Path != filepath.Join(workspace, "a.png") || got.Media[1].Path != filepath.Join(workspace, "b.png") || got.Media[0].Source == nil || got.Media[0].Source.SourceID != "src_1" {
		t.Fatalf("media = %+v", got.Media)
	}
	if got.Said == nil || *got.Said != said || len(got.Attachments) != 2 || got.Annotation == nil || len(got.Annotation.Marks) != 3 ||
		got.Annotation.Marks[2].Shade.BrushWidth != 4 || got.Annotation.Marks[1].To.X != 82 {
		t.Fatalf("submission = %+v", got)
	}

	user := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryUser })
	if user.MessageID != receipt.MessageID || user.Delivery != appwire.DeliveryDescription || user.SeenBy != "vendor/seer" ||
		user.Annotation == nil || len(user.Annotation.Marks) != 3 || user.Text != said || len(user.Attachments) != 2 {
		t.Fatalf("entry = %+v", user)
	}
	media := user.Attachments[0].Media
	if media == nil || media.Mime != "image/png" || *media.Width != 40 || *media.Height != 30 || media.Role != "original" || user.Attachments[1].Media.Role != "marked" {
		t.Fatalf("attachment media = %+v", user.Attachments)
	}
	for _, id := range ids {
		if staged(backend, agent.ID, id) {
			t.Fatalf("chip %s was kept staged after the message was accepted", id)
		}
	}
}

// A refusal — a description that does not fit, a file the engine will not carry,
// an engine that cannot take pictures — leaves every chip staged and the engine
// holding nothing, and the same ids then send once.
func TestAppRefusedPicturesLeaveEveryChipStaged(t *testing.T) {
	conv := &composingConversation{fakeConversation: &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}, known: true}
	backend, _, agent, _, ids := composingBackend(t, conv)
	sendWith := func(mutate func(*appwire.MessageSubmission)) error {
		sub := submissionFor(ids)
		mutate(sub)
		_, err := backend.SendComposed(agent.ID, appwire.AgentMessage{Text: "x", Attachments: ids, Submission: sub})
		return err
	}
	for name, mutate := range map[string]func(*appwire.MessageSubmission){
		"a coordinate over 100": func(s *appwire.MessageSubmission) { s.Annotation.Marks[0].At.X = 101 },
		"an inverted box":       func(s *appwire.MessageSubmission) { s.Annotation.Marks[1].To.Y = 1 },
		"an unknown kind":       func(s *appwire.MessageSubmission) { s.Annotation.Marks[0].Kind = "arrow" },
		"an unstaged picture":   func(s *appwire.MessageSubmission) { s.Media[1].ID = "at99" },
	} {
		if err := sendWith(mutate); wireCode(err) != appwire.CodeBadParams {
			t.Errorf("%s answered %v, want bad_params", name, err)
		}
	}
	if len(conv.sent()) != 0 || conv.submits != 0 {
		t.Fatal("a refused message reached the engine")
	}

	// The engine itself refuses a file it cannot carry.
	conv.mu.Lock()
	conv.refuse = errors.New("session: the message's description does not fit: b.png is not an image this surface can send")
	conv.mu.Unlock()
	if err := sendWith(func(*appwire.MessageSubmission) {}); err == nil {
		t.Fatal("an engine refusal was swallowed")
	}
	for _, id := range ids {
		if !staged(backend, agent.ID, id) {
			t.Fatalf("chip %s was consumed by a refused message", id)
		}
	}

	// Once the engine accepts, the same chips send once and are gone.
	conv.mu.Lock()
	conv.refuse = nil
	conv.mu.Unlock()
	if err := sendWith(func(*appwire.MessageSubmission) {}); err != nil {
		t.Fatalf("the retry: %v", err)
	}
	if len(conv.sent()) != 1 {
		t.Fatalf("the engine took %d messages, want one", len(conv.sent()))
	}
	for _, id := range ids {
		if staged(backend, agent.ID, id) {
			t.Fatalf("chip %s stayed staged after it was sent", id)
		}
	}
}

func TestAppPicturesAreNeverSentAsWordsAlone(t *testing.T) {
	for name, conv := range map[string]appConversation{
		"a conversation with no door":      &fakeConversation{},
		"an engine that has not said so":   &composingConversation{fakeConversation: &fakeConversation{}, known: false},
		"an engine that has not said so 2": &composingConversation{fakeConversation: &fakeConversation{}},
	} {
		t.Run(name, func(t *testing.T) {
			backend, _, agent, _, ids := composingBackend(t, conv)
			_, err := backend.SendComposed(agent.ID, appwire.AgentMessage{Text: "x", Attachments: ids, Submission: submissionFor(ids)})
			if wireCode(err) != appwire.CodeUnknownMethod {
				t.Fatalf("answered %v, want unknown_method", err)
			}
			for _, id := range ids {
				if !staged(backend, agent.ID, id) {
					t.Fatalf("chip %s was consumed", id)
				}
			}
			var texts []string
			switch c := conv.(type) {
			case *fakeConversation:
				texts = sentTexts(c)
			case *composingConversation:
				texts = sentTexts(c.fakeConversation)
			}
			if len(texts) != 0 {
				t.Fatalf("the engine was sent %q", texts)
			}
		})
	}
}

func TestAppOnlyAFileCanBeSentAsAPicture(t *testing.T) {
	conv := &composingConversation{fakeConversation: &fakeConversation{}, known: true}
	backend, _, agent, _, ids := composingBackend(t, conv)
	link, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachLink, URL: "https://example.com/a.png"}, "")
	if err != nil {
		t.Fatal(err)
	}
	all := append(append([]string(nil), ids...), link.ID)
	sub := &appwire.MessageSubmission{Media: []appwire.SubmissionMedia{{ID: link.ID}}}
	if _, err := backend.SendComposed(agent.ID, appwire.AgentMessage{Text: "x", Attachments: all, Submission: sub}); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a link sent as a picture answered %v", err)
	}
	if len(conv.sent()) != 0 {
		t.Fatal("the engine heard of a link as a picture")
	}
}

// REOPENED, THE ENTRY SAYS THE SAME THINGS: the engine's id, delivery, the
// vision model that looked, the marks, and the measurements on each chip.
func TestAppReopenedMessageShowsWhatTheEngineKept(t *testing.T) {
	width, height, bytes := 40, 30, int64(99)
	_ = width
	item := session.DisplayEntry{
		Role: "user", Text: "make the sky warmer",
		Person: &session.PersonMark{
			Authored:    true,
			Attachments: []session.MessageAttachment{{ID: "at1", Kind: "file", Name: "a.png", Path: "/w/a.png"}, {ID: "at2", Kind: "file", Name: "b.png", Path: "/w/b.png"}},
			Composed: &session.ComposedMark{
				MessageID: "msg_x", Delivery: &session.MessageDelivery{Mode: session.DeliveryDescription, Model: "vendor/seer"},
				Media:      []session.MessageMedia{{ID: "at1", MIME: "image/png", Bytes: bytes, Width: width, Height: height, Role: session.MediaRoleOriginal, SHA256: "ab"}},
				Annotation: &session.Annotation{Original: "at1", Marked: "at2", ChangeOnlyMarked: false, Marks: []session.AnnotationMark{{Kind: "pin", At: &session.Point{X: 1, Y: 2}}}},
			},
		},
	}
	entries := appEntriesOf("a1", []session.DisplayEntry{item}, time.Now(), nil)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	user := entries[0]
	if user.MessageID != "msg_x" || user.Delivery != appwire.DeliveryDescription || user.SeenBy != "vendor/seer" ||
		user.Annotation == nil || user.Annotation.ChangeOnlyMarked || user.Annotation.Marks[0].At.Y != 2 {
		t.Fatalf("reopened entry = %+v", user)
	}
	if m := user.Attachments[0].Media; m == nil || *m.Width != 40 || m.Role != "original" || m.SHA256 != "ab" || user.Attachments[1].Media != nil {
		t.Fatalf("reopened attachments = %+v", user.Attachments)
	}
}

// An explicit empty submission is a composed text message, not a nil presentation.
func TestAppComposedTextWithoutAttachmentsHasAReceipt(t *testing.T) {
	conv := &composingConversation{fakeConversation: &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}, known: true}
	backend, _, agent, _, _ := composingBackend(t, conv)
	receipt, err := backend.SendComposed(agent.ID, appwire.AgentMessage{Text: "draw a tree", Submission: &appwire.MessageSubmission{}})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.MessageID == "" || receipt.Delivery != session.DeliveryText {
		t.Fatalf("missing text receipt: %#v", receipt)
	}
	if sent := conv.sent(); len(sent) != 1 || sent[0].Text != "draw a tree" || len(sent[0].Media) != 0 {
		t.Fatalf("not one plain composed message: %#v", sent)
	}
}
