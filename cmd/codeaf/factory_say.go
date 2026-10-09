package main

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/guard"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// ── SAY: THE ITEM PAGE'S BOX SENDS IN PLACE ─────────────────────────────────
//
// The story in the middle of the item page IS the manager's conversation (the
// owner's decision of 2026-10-08), so `enter` in its box sends the words to
// that conversation without leaving the page ([factory.Seam.Say]). The words
// go in as the person's own message, and the manager's turn on them streams
// into the conversation's transcript, which the page reads every second.
//
// THERE IS ONE CONVERSATION, NEVER TWO. Its journal takes one lock, so the
// door goes through whatever already holds it:
//
//   - open in this process (a window's conversation, in front or behind, or a
//     runner's shaping turn, [session.LiveAgentFor]): the words go to that
//     agent, a turn in flight taking them as a steer;
//   - opened by this door before and kept: the words go to it;
//   - otherwise it is opened THROUGH THE WINDOW'S OWN DOOR ([tui3.Options.Open]),
//     the way `T` would open it, and kept here. When the person then presses
//     `T`, the window asks that same door for the transcript and is handed
//     the conversation this door kept ([sayDoor.adopt]), turn in flight and
//     all, instead of opening a second copy that the lock would refuse.
//
// A launch whose window cannot open a conversation, or whose floor has no Talk
// door, has no Say door, and the box hands its words to the chat as before.

// sayTurn is what the door needs of a conversation: the person's words in.
// *session.Agent and every tui3.Agent answer it; a test hands in a fake.
type sayTurn interface {
	Submit(ctx context.Context, text string) (<-chan session.Event, error)
}

// sayDoor is one window's Say door and the conversations it keeps open.
type sayDoor struct {
	// talk is the floor's Talk door: the item's conversation, made when it has
	// none.
	talk func(ctx context.Context, id int) (string, error)
	// live is the conversation open in this process on a transcript.
	live func(chat string) (sayTurn, bool)
	// open is the window's own door onto a transcript, in its folder.
	open func(workspace, transcript string) (tui3.Conversation, error)
	// folder is the folder a conversation works in, from its session meta.
	folder func(chat string) string

	mu   sync.Mutex
	kept map[string]tui3.Conversation
}

// say is [factory.Seam.Say].
func (d *sayDoor) say(ctx context.Context, id int, words string) error {
	words = strings.TrimSpace(words)
	if words == "" {
		return errors.New("nothing to say")
	}
	chat, err := d.talk(ctx, id)
	if err != nil {
		return err
	}
	chat = strings.TrimSpace(chat)
	if chat == "" {
		return errors.New("the item has no conversation")
	}
	if d.live != nil {
		if turn, ok := d.live(chat); ok {
			return saySubmit(turn, words)
		}
	}
	key := sameFileKey(chat)
	conv, held := d.held(key)
	if held {
		if err := saySubmit(conv.Agent, words); err == nil {
			return nil
		}
		// A KEPT CONVERSATION THAT REFUSES has ended under the door (its
		// engine went away): it is let go and opened again, once.
		d.forget(key)
	}
	conv, err = d.open(d.folder(chat), chat)
	if err != nil {
		return err
	}
	if conv.Agent == nil {
		return errors.New("the item's conversation did not open")
	}
	d.keep(key, conv)
	return saySubmit(conv.Agent, words)
}

// held is the conversation this door keeps under key, when it keeps one.
func (d *sayDoor) held(key string) (tui3.Conversation, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	conv, ok := d.kept[key]
	return conv, ok
}

// keep records a conversation this door opened, so the next say finds it.
func (d *sayDoor) keep(key string, conv tui3.Conversation) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.kept == nil {
		d.kept = map[string]tui3.Conversation{}
	}
	d.kept[key] = conv
}

// forget lets a kept conversation go.
func (d *sayDoor) forget(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.kept, key)
}

// take hands a kept conversation over and forgets it, answering whether one
// was kept.
func (d *sayDoor) take(key string) (tui3.Conversation, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	conv, ok := d.kept[key]
	if ok {
		delete(d.kept, key)
	}
	return conv, ok
}

// saySubmit puts the words in and lets the turn run: its events are drained
// here, because the transcript is where the item page reads the reply.
func saySubmit(turn sayTurn, words string) error {
	events, err := turn.Submit(context.Background(), words)
	if err != nil {
		return err
	}
	if events != nil {
		guard.Go("factory/say-drain", func() {
			for range events {
			}
		})
	}
	return nil
}

// adopt is the window's Open door with this door in front of it: a transcript
// this door opened and keeps is handed over, and the door forgets it (the
// window holds it now, and the next say finds it live or asks the window's
// door again); one another owner in this process holds live is lent as a
// view ([liveConversation]); every other transcript is opened as before.
func (d *sayDoor) adopt(open func(workspace, transcript string) (tui3.Conversation, error)) func(workspace, transcript string) (tui3.Conversation, error) {
	return func(workspace, transcript string) (tui3.Conversation, error) {
		key := sameFileKey(transcript)
		if conv, ok := d.take(key); ok {
			return conv, nil
		}
		// A CONVERSATION THIS PROCESS ALREADY HOLDS ON THE JOURNAL (a running
		// step's own agent) is lent as a view, live, instead of a second open
		// its lock would refuse (factory_embed.go).
		if conv, ok := liveConversation(workspace, transcript); ok {
			return conv, nil
		}
		return open(workspace, transcript)
	}
}

// liveSayTurn is the conversation open in this process on chat.
func liveSayTurn(chat string) (sayTurn, bool) {
	agent, ok := session.LiveAgentFor(chat)
	if !ok {
		return nil, false
	}
	return agent, true
}

// sayFolder is the folder a conversation works in, "" when its meta does not
// say (the window's door then opens it in the launch's own).
func sayFolder(chat string) string {
	if meta, err := session.LoadMeta(filepath.Dir(chat)); err == nil {
		return strings.TrimSpace(meta.Workspace)
	}
	return ""
}

// factoryWireSay hangs the Say door on a window's factory seam and puts it in
// front of the window's Open door, so `T` attaches to what Say opened. A
// window with no Talk door or no Open door gets none.
func factoryWireSay(options *tui3.Options) {
	if options == nil || options.Factory.Talk == nil || options.Open == nil {
		return
	}
	d := &sayDoor{talk: options.Factory.Talk, live: liveSayTurn, open: options.Open, folder: sayFolder}
	options.Factory.Say = d.say
	options.Open = d.adopt(options.Open)
}
