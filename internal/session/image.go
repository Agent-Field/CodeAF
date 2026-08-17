package session

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// RoleVision is declared in internal/roles but assigned no default tier there,
// so it is registered HERE — from the package that makes the call, through the
// documented open-registry seam (roles.Register's own doc). The registry stays a
// registry rather than becoming the merge point for every package's vocabulary.
//
// The high tier, not the low one. Every other auxiliary call on this surface is
// cheap because a bad answer costs a glance; this one IS the answer — it is what
// the person reads in place of the reply their model could not give — so it is
// the one auxiliary call that must be good.
func init() { roles.Register(roles.RoleVision, roles.TierHigh) }

// Images enter the conversation as OpenAI-style content parts: one user message
// whose content is the person's words followed by an image_url part per picture,
// each carrying a base64 data URL. That is the shape every backend this surface
// routes through reads, and it is the shape the wire actually sends — the SDK
// collapses content to a bare string only for a lone text part (ai.Message's
// MarshalJSON), so a multi-part message travels as the array it is.
//
// Two limits are enforced here rather than left to the provider. Provider limits
// aside, base64 inflation is real: the bytes grow by a third on the way out, sit
// in the transcript for the rest of the session, and are re-sent on every step of
// every turn after this one. A picture that is merely large when it is attached
// is expensive forever.
const (
	// maxImageBytes is the per-image ceiling, matching the executor's own
	// (internal/exec/media.go) so the same photo is accepted or refused the same
	// way whichever door it arrives at.
	maxImageBytes = 10 << 20

	// maxMessageImageBytes is the ceiling for one message's images together. The
	// per-image limit alone would let ten photos just under it through as a
	// single 100MB turn.
	maxMessageImageBytes = 20 << 20
)

// Image is one picture on its way into the conversation.
//
// Path is where it lives and is what the journal records — a session file must
// stay readable, so it holds a REFERENCE and never the bytes (see [journalPart]).
// MIME may be empty, in which case it is read from the extension. Bytes may be
// nil, in which case the file is read from Path; a caller that already holds the
// bytes (a clipboard paste written to a temp file, a screenshot) passes them and
// the file is not read twice.
type Image struct {
	Path  string
	MIME  string
	Bytes []byte
}

// imageMediaTypes is the accepted set, by extension. It is deliberately the same
// five the rest of the codebase accepts: a format nobody's vision endpoint reads
// is better refused at the door, where the person can convert it, than sent and
// answered with a provider error about a content part.
var imageMediaTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// SubmitImage is [Agent.Submit] with pictures: it appends one user message
// carrying the text and the images as content parts, and runs a normal turn —
// same hub, same events, same journal discipline, same steering rules. A caller
// that already knows how to read a turn's channel needs to learn nothing new.
//
// A model that cannot see does not end the message: the images go to a VISION
// MODEL instead, and its answer is the turn's reply ([Agent.visionTurn]). Only
// when no vision model resolves does the refusal below fire.
//
// It refuses BEFORE anything is recorded when nothing can see the pictures (see
// Config.SupportsImages) or when the images are too large. A refusal here is an
// error return rather than an event stream, unlike the spend rail's: the rail
// refuses a turn the person legitimately asked for and will ask for again, while
// this refuses a message that was never assemblable — nothing was journaled,
// nothing was queued, and the person still holds their text.
//
// No images is exactly Submit, so a surface with an empty attachment tray can
// call one method for both.
func (a *Agent) SubmitImage(ctx context.Context, text string, images []Image) (<-chan Event, error) {
	text = strings.TrimSpace(text)
	if len(images) == 0 {
		return a.Submit(ctx, text)
	}

	// The gate reads the model the NEXT turn will ride, which is the model this
	// message is about to be sent to. Reading it here rather than at assembly
	// keeps the refusal cheap: nothing is read from disk for a model that could
	// not have looked at it, and the fallback's own resolution is cheaper still.
	model := a.Model()
	if a.config.SupportsImages == nil || !a.config.SupportsImages(model) {
		return a.visionTurn(ctx, text, images, model)
	}

	// Text may be empty here, unlike Submit's: a message that is only a picture
	// is a message ("what is this?" is often the picture itself), and the model
	// receives content either way.
	user, err := imageUserMessage(text, images)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, errors.New("session: agent is closed")
	}
	if a.running {
		// Steering, by the same law text steering follows (agent.go): the queue
		// holds the assembled message — parts, journal references and all — and
		// the loop lands it at the next step boundary, where a user message is a
		// legal shape. The references travel with it because the journal write
		// happens then, not now.
		a.steering = append(a.steering, user)
		events := a.hub.subscribe()
		a.mu.Unlock()
		return events, nil
	}
	if err := a.railBlockLocked(); err != nil {
		a.mu.Unlock()
		return refusedStream(err), nil
	}
	events := a.startTurnLocked(ctx, user, nil)
	a.mu.Unlock()
	return events, nil
}

// imageUserMessage assembles the message and the journal references for it in
// one pass, because they are two readings of the same bytes: the data URL the
// model gets and the digest the transcript keeps.
//
// The text part comes first and only when there is text. Order is the message's
// meaning — a question about pictures reads as the question, then the pictures —
// and an empty leading text part is a content block several backends reject.
func imageUserMessage(text string, images []Image) (userMessage, error) {
	parts := make([]ai.ContentPart, 0, len(images)+1)
	refs := make([]journalPart, 0, len(images))
	if text != "" {
		parts = append(parts, ai.ContentPart{Type: "text", Text: text})
	}

	total := 0
	for _, image := range images {
		path := strings.TrimSpace(image.Path)
		if path == "" {
			return userMessage{}, errors.New("session: image has no path")
		}
		mediaType, err := imageMediaType(image)
		if err != nil {
			return userMessage{}, err
		}
		data, err := imageBytes(image)
		if err != nil {
			return userMessage{}, err
		}
		total += len(data)
		if total > maxMessageImageBytes {
			return userMessage{}, fmt.Errorf("session: these images total more than the %s a single message may carry — send them across a few messages", byteLimit(maxMessageImageBytes))
		}
		sum := sha256.Sum256(data)
		parts = append(parts, ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{
			URL: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data),
		}})
		refs = append(refs, journalPart{
			Type:   journalPartImage,
			Path:   path,
			SHA256: hex.EncodeToString(sum[:]),
			MIME:   mediaType,
		})
	}
	return userMessage{message: ai.Message{Role: "user", Content: parts}, refs: refs}, nil
}

// imageMediaType takes the caller's word when it gave one and reads the
// extension when it did not. An unknown type is refused rather than guessed:
// "image/png" over a JPEG is a 400 from the provider with nothing in it that
// points back here.
func imageMediaType(image Image) (string, error) {
	if declared := strings.TrimSpace(image.MIME); declared != "" {
		return declared, nil
	}
	mediaType := imageMediaTypes[strings.ToLower(filepath.Ext(image.Path))]
	if mediaType == "" {
		return "", fmt.Errorf("session: %s is not an image this surface can send — png, jpeg, webp and gif are", filepath.Base(image.Path))
	}
	return mediaType, nil
}

// imageBytes returns the picture's content, reading the file only when the
// caller did not already hold it.
//
// The size is checked against the stat BEFORE the read, not only after it. A
// caller can hand over a path to anything, and a limit enforced after the read
// is a limit that already pulled a multi-gigabyte file into memory to discover
// it was too big.
func imageBytes(image Image) ([]byte, error) {
	if image.Bytes != nil {
		if len(image.Bytes) > maxImageBytes {
			return nil, oversizeImage(image.Path)
		}
		return image.Bytes, nil
	}
	info, err := os.Stat(image.Path)
	if err != nil || info.IsDir() {
		return nil, fmt.Errorf("session: could not read %s", filepath.ToSlash(image.Path))
	}
	if info.Size() > maxImageBytes {
		return nil, oversizeImage(image.Path)
	}
	data, err := os.ReadFile(image.Path)
	if err != nil {
		return nil, fmt.Errorf("session: could not read %s", filepath.ToSlash(image.Path))
	}
	// Checked again: the file could have grown between the stat and the read.
	if len(data) > maxImageBytes {
		return nil, oversizeImage(image.Path)
	}
	return data, nil
}

func oversizeImage(path string) error {
	return fmt.Errorf("session: %s is over the %s image limit", filepath.ToSlash(path), byteLimit(maxImageBytes))
}

func byteLimit(bytes int) string { return fmt.Sprintf("%dMB", bytes>>20) }

// ── the vision fallback ─────────────────────────────────────────────────────
//
// A person who attaches a photo to a model that cannot see it has not made a
// mistake. They have a picture and a question, and the surface has, quite often,
// a second model that can look at it. Refusing outright — the whole behaviour
// before this — made the person do the routing by hand: switch model, re-attach,
// ask again, switch back. The fallback does it for them, once, and says so.
//
// Three properties are the design:
//
//   - IT IS ONE SHOT, NOT A SESSION. The vision model is sent the picture and
//     the person's words and NOTHING ELSE — no system prompt, no transcript, no
//     tools. It is being asked what it can see, not being made a second agent
//     with a second memory of this conversation.
//
//   - THE TRANSCRIPT KEEPS TEXT, NOT PARTS. What goes into the conversation is
//     the person's words, a line naming the files, and the vision model's answer
//     as an ordinary assistant message. The image parts go to the vision model
//     and are never recorded, because the chat model is BLIND BY CONSTRUCTION:
//     leaving parts in the transcript would put an image_url in front of it on
//     every step of every turn after this one, which is the 400 this whole path
//     exists to avoid. The journal still writes the references (image.go's law),
//     so the file, its digest and its type are all on the record.
//
//   - IT IS SAID OUT LOUD. The reply is prefixed "[vision: <model>]". A second
//     model answering in the first one's voice, silently, would be the surface
//     lying about who is talking.

// visionSeer answers WHICH MODEL LOOKS, and it is the ONE answer the whole
// surface uses: this fallback, the view_image tool (tools_view.go) and
// read_document's image rung (tools_doc.go) all ask here, so "can I see" has a
// single answer wherever it is asked (docs/MULTIMODAL.md, Decision 8). It is ""
// when nothing on this machine can look at a picture.
//
// THE LOOKING SLOT IS THE FRONT DOOR. [Config.MediaModel] is the surface's own
// use-time resolver, and every rung behind it — the settings slot, the role
// pin, the best catalog candidate, the curated fallback — is capability-checked
// against the catalog before it answers, so a slug it names is a model that
// publishes image input. A resolver that answers "" has said there is none, and
// the roles ladder is deliberately NOT tried after it: the pin is a rung of that
// same ladder, and asking twice would resurrect the model the resolver just
// passed over for being unable to see.
//
// A NIL resolver is a surface built before the slot existed — a test, an old
// door — and it falls to roles.Resolve, which is exactly what this path did
// before. The floor is "" rather than the session's model on purpose:
// everywhere else roles.Resolve falls back to the model the person is already
// talking to, and here that is often the model just established as blind, so
// the ladder must be allowed to run out instead of returning it.
func (a *Agent) visionSeer() string {
	// The modality word is the one Config.MediaModel documents; it is spelled
	// here rather than shared as a constant because the resolver's vocabulary is
	// the surface's, and this package reads it rather than owning it.
	//
	// STUB(media/knob): Config.MediaModel is the settings lane's use-time
	// resolver — the looking slot, the pin, the catalog, the curated fallback —
	// and it is already capability-checked when it answers.
	if resolve := a.config.MediaModel; resolve != nil {
		return strings.TrimSpace(resolve("vision"))
	}
	seer, err := roles.Resolve(roles.Source(a.config.RolesSource), roles.RoleVision, "")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(seer)
}

// visionTurn is the fallback path: SubmitImage's answer when the chat model
// cannot see. It returns SubmitImage's refusal, unchanged, when nothing else can
// see either.
func (a *Agent) visionTurn(ctx context.Context, text string, images []Image, model string) (<-chan Event, error) {
	// A seer that names the blind model is refused rather than called: a slot or
	// a tier set to the model we just established cannot see is a configuration,
	// not a capability, and sending the picture to it is the 400 this whole path
	// exists to avoid.
	seer := a.visionSeer()
	if seer == "" || strings.EqualFold(seer, model) {
		return nil, blindRefusal(model)
	}

	// Assembled first because the images have to be read and bounded before
	// anything is recorded — the same order the sighted path uses, and the same
	// errors.
	live, err := imageUserMessage(text, images)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, errors.New("session: agent is closed")
	}
	if a.running {
		// The sighted path steers here, and this one cannot: steering means
		// splicing the message into the RUNNING turn, whose model is the one
		// that cannot read it. The fallback is a turn of its own, so it waits
		// for the room — and the person is told that rather than left to wonder
		// why their picture did nothing.
		a.mu.Unlock()
		return nil, fmt.Errorf("session: %s cannot read images and %s answers between turns — wait for this one to finish, or switch to a model with vision", model, seer)
	}
	if err := a.railBlockLocked(); err != nil {
		a.mu.Unlock()
		return refusedStream(err), nil
	}
	events := a.startVisionTurnLocked(ctx, visionUserMessage(text, live.refs), live.message, seer)
	a.mu.Unlock()
	return events, nil
}

// blindRefusal is the message SubmitImage has always returned: no model here can
// look at this, in the words the person can act on.
func blindRefusal(model string) error {
	return fmt.Errorf("session: %s cannot read images — switch to a model with vision, or describe what the picture shows", model)
}

// visionUserMessage is what the TRANSCRIPT keeps of the person's message: their
// words, and a line naming what they attached.
//
// The line is there for the chat model, which will read this turn on every step
// of the next one and would otherwise find an assistant describing a photograph
// nobody mentioned. The references ride along untouched, so the journal records
// exactly what it records on the sighted path — path, digest, type — and a
// replayed session marks the attachment the same way.
func visionUserMessage(text string, refs []journalPart) userMessage {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, filepath.Base(ref.Path))
	}
	marker := "[attached image: " + strings.Join(names, ", ") + "]"
	if len(names) > 1 {
		marker = "[attached images: " + strings.Join(names, ", ") + "]"
	}
	body := marker
	if text != "" {
		body = text + "\n\n" + marker
	}
	return userMessage{message: textMessage("user", body), refs: refs}
}

// startVisionTurnLocked begins the fallback as A TURN, with a.mu held.
//
// It is [Agent.startTurnLocked]'s bookkeeping with a different body, and every
// line of that bookkeeping is load-bearing: running is what makes a concurrent
// Submit queue instead of starting a second conversation, the hub is what a
// surface subscribes to, done is what Close waits on, and the two drains under
// the lock that clears running are what keep a steered line or a queued
// follow-up from landing out of the order the person typed it. The system
// prompt is NOT refreshed — this turn does not send one.
//
// The body is one provider call rather than runTurn, which is why the two are
// not the same function today. If a third kind of turn ever appears, the shared
// half is this function and it should move to agent.go beside the original.
func (a *Agent) startVisionTurnLocked(ctx context.Context, kept userMessage, live ai.Message, seer string) <-chan Event {
	a.running = true
	hub := newEventHub()
	a.hub = hub
	turnCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	done := make(chan struct{})
	a.done = done
	a.recordUserLocked(kept)
	events := hub.subscribe()

	go func() {
		completed := false
		defer cancel()
		defer hub.close()
		defer func() {
			a.mu.Lock()
			a.drainSteeringLocked()
			a.running = false
			a.cancel = nil
			a.hub = nil
			a.done = nil
			close(done)
			if next, ok := a.nextFollowUpLocked(completed); ok {
				// An ordinary turn, on the chat model: the follow-up is words,
				// and words are what that model can read.
				a.startTurnLocked(context.Background(), next.message, next.stream)
			}
			a.mu.Unlock()
		}()
		defer func() {
			if recovered := recover(); recovered != nil {
				completed = false
				hub.send(Event{Kind: EventError, Err: guard.Note("session/vision", recovered)})
			}
		}()
		completed = a.runVision(turnCtx, hub, live, seer)
	}()
	return events
}

// runVision is the fallback turn's body: one call, one answer, one message.
//
// The usage folds into the SESSION total and never into the turn's
// ([Agent.addAuxiliaryUsage]), which is the same treatment the title and the
// compaction summary get and for the same reason: the person pays for it, but no
// turn of theirs ran on that model, and reporting this turn's cost as the vision
// model's would make the conversation's arithmetic unreadable.
func (a *Agent) runVision(ctx context.Context, hub *eventHub, live ai.Message, seer string) bool {
	started := time.Now()
	note := "[vision: " + seer + "]\n\n"

	// The note is the first thing on the stream and the first thing in the
	// buffer, so the live view and the recorded message begin with the same
	// words — including for an interrupt, where the buffer is what survives.
	partial := &partialBuffer{}
	partial.write(note)
	hub.send(Event{Kind: EventTextDelta, Text: note})

	var streamed atomic.Int64
	ctx = provider.WithStreamObserver(ctx, func(event provider.StreamEvent) {
		switch event.Kind {
		case provider.StreamDelta:
			streamed.Add(int64(len(event.Delta)))
			partial.write(event.Delta)
			hub.send(Event{Kind: EventTextDelta, Text: event.Delta})
		case provider.StreamThinking:
			hub.send(Event{Kind: EventThinking})
		case provider.StreamReasoning:
			// Never written to the buffer, by loop.go's law: the model's working
			// is not its answer.
			hub.send(Event{Kind: EventReasoning, Text: event.Delta})
		}
	})

	response, err := a.client.CompleteWithMessages(ctx, []ai.Message{live}, ai.WithModel(seer))
	if err == nil && response != nil {
		a.addAuxiliaryUsage(response)
	}
	answer := ""
	if response != nil {
		answer = strings.TrimSpace(response.Text())
	}

	if err != nil || answer == "" {
		// Whatever was streamed before the cut is real work the person watched
		// arrive, so it is kept exactly as an interrupted turn's partial is. A
		// call that streamed nothing keeps nothing: an assistant message holding
		// only the note would be a reply that says who spoke and not what they
		// said.
		if streamed.Load() > 0 {
			a.keepPartial(partial)
		} else {
			partial.reset()
		}
		if ctx.Err() != nil {
			hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{}, started)})
			return false
		}
		if err == nil {
			err = fmt.Errorf("session: %s returned no answer for the image", seer)
		}
		hub.send(Event{Kind: EventError, Err: err, Usage: a.sealTurn(Usage{}, started)})
		return false
	}

	// A provider that did not stream still owes the surface its text, and the
	// deltas are the only place a surface reads it from. Sending it once here —
	// and only when nothing streamed — is what keeps a non-streaming endpoint
	// from showing a blank reply and a streaming one from showing two copies.
	if streamed.Load() == 0 {
		hub.send(Event{Kind: EventTextDelta, Text: answer})
	}
	partial.reset()
	a.record(textMessage("assistant", note+answer))
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{}, started)})
	// The session may name itself off this exchange like any other: a
	// conversation that opened with a photograph is still a conversation about
	// something (title.go).
	a.maybeTitle(ctx, hub)
	return true
}
