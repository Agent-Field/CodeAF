package chat

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/cas"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Attachments: the wiring's half of the composer's attachment grammar (5.11,
// 7.2, 12.5.1), and the surface's half of JOURNEY 17.
//
// # The lens law, stated as a shape
//
// This surface writes the SAME journal row the old window writes. A message
// with an attachment is one `message_posted` event whose payload carries a
// `cas://<digest>/<original path>` string in `attachments` — no new event kind,
// no v2-only column, no second convention. That is Decision 7 taken literally:
// the engine, the head's vision seam (internal/head), the workspace stager
// (internal/exec) and the fold all read attachments today, and none of them may
// be able to tell which window a person was typing in. A surface that invented
// its own reference shape would be a second engine wearing a lens's clothes.
//
// # Three steps, each owned by the layer that knows the answer
//
//	composer  a token in the draft becomes a chip        (attach.go there)
//	here      a chip becomes a file on disk, described   (attachmentCandidate)
//	here      a file becomes a content-addressed copy    (keepAttachments)
//
// The composer never touches the filesystem and never learns what a CAS is; the
// engine never learns what a chip is. What crosses each seam is the smallest
// true fact: a path, then a reference.
//
// # Copied at send, never at mention
//
// The copy is made on the way into the journal, in the command that posts —
// off the render goroutine, because hashing a 20 MB screenshot on the keystroke
// path would drop frames the reader would feel. What the journal then holds is
// CONTENT and not a pointer: the file can be moved, renamed or deleted the
// second after it is sent and the conversation still has what was shown. That
// property is the whole reason a CAS is in this path at all, and J17 tests it
// by deleting the original.

// AttachmentKeeper is the optional slice of the live engine that copies a file
// into the content-addressed store and hands back the reference to journal.
//
// It is an optional interface reached by assertion, the way [ModelControl] is,
// rather than a method on [Commander]: the concrete engine cmd/aforge builds
// already has it (internal/command's Commander, which routes to
// internal/exec.KeepAttachment), so the real window gets the real behaviour with
// no wiring, while every test fake and every window with no engine behind it
// keeps compiling and degrades honestly.
//
// The degradation is v1's, exactly (internal/tui/attachments.go's
// keepAttachment): a keeper that is absent or that fails journals the person's
// OWN path. That is a weaker record — it can rot when the file moves — but it
// is a true one, and losing the attachment entirely to protect a purity
// argument would be losing what the reader was trying to show us.
type AttachmentKeeper interface {
	KeepAttachment(path string) (string, error)
}

// imageExtensions and documentExtensions are what may ride along. The two lists
// are v1's (internal/tui/attachments.go) and are kept identical on purpose: the
// same drag onto either window must attach or not attach the same way, or the
// two surfaces disagree about what the product does.
//
// The gate is an extension and a stat, and deliberately not a magic-number
// sniff. A person who names a file .png means the picture; a file whose bytes
// disagree with its name is the provider's problem to report, and reading the
// head of every path-shaped token on every keystroke to find out would be this
// surface doing IO to second-guess the reader.
var (
	imageExtensions = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true,
	}
	documentExtensions = map[string]bool{
		".pdf": true, ".docx": true, ".pptx": true,
	}
)

// attachmentCandidate is [composer.Options.Attach]: it decides whether one
// shell-shaped token in the draft names a file that may ride along.
//
// The order of the checks is the cost order. An extension test is a string
// compare and rejects every ordinary word in a sentence; only a token that
// survives it is ever handed to the filesystem, which is what keeps a keystroke
// in a prose draft free of syscalls.
func attachmentCandidate(token string) (composer.Attachment, bool) {
	path := strings.TrimSpace(token)
	if path == "" {
		return composer.Attachment{}, false
	}
	if _, ok := attachmentKind(path); !ok {
		return composer.Attachment{}, false
	}
	// `~` is the shell's, not the filesystem's. Expanding it here — in the
	// process that knows whose home this is — is what lets a person type the
	// path they would type anywhere else.
	if rest, cut := strings.CutPrefix(path, "~/"); cut {
		home, err := os.UserHomeDir()
		if err != nil {
			return composer.Attachment{}, false
		}
		path = filepath.Join(home, rest)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return composer.Attachment{}, false
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		// A name that is not a file on disk is still just a word. It stays in
		// the draft, where a person can see that we did not take it — v1 does
		// the same, and the alternative (a chip for a file that is not there)
		// is the affordance lying about what the send will carry (5.20).
		return composer.Attachment{}, false
	}
	return composer.Attachment{
		Path: absolute,
		Name: filepath.Base(absolute),
		// The reference mark this surface already uses for a deliverable
		// (message.go's PartArtifact row). The chip is a preview of the row the
		// file becomes once the message is journaled, so it wears the same mark
		// rather than introducing a second vocabulary for the same object.
		Glyph: tokens.GlyphCollapsed,
		Bytes: info.Size(),
	}, true
}

// attachmentKind reports whether a path's extension is attachable, and whether
// it is an image. The bool pair is what [attachmentBody] needs to write a body
// that says what was actually sent.
func attachmentKind(path string) (image bool, ok bool) {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case imageExtensions[ext]:
		return true, true
	case documentExtensions[ext]:
		return false, true
	}
	return false, false
}

// attachmentBody is the body a send with no words gets.
//
// A message needs a line: the transcript draws a turn, the head reads prose,
// and an empty user row would render as a blank the reader cannot tell from a
// bug. The three sentences are v1's, word for word (internal/tui/model.go's
// submit), because a person who attaches the same file to either window must
// see the same thing in the journal afterwards.
func attachmentBody(list []composer.Attachment) string {
	images, documents := 0, 0
	for _, attachment := range list {
		if image, ok := attachmentKind(attachment.Path); ok {
			if image {
				images++
			} else {
				documents++
			}
		}
	}
	switch {
	case documents > 0 && images > 0:
		return "Attachments added."
	case documents > 0:
		return "Document attached."
	case images > 0:
		return "Image attached."
	}
	return ""
}

// keepAttachments turns captured files into the strings the journal holds.
//
// It runs inside the posting command, never on the render goroutine — see the
// note at the top of this file. Each failure is contained to its own file: one
// unreadable path degrades to itself and the rest of the message is unaffected,
// because a send that dropped four attachments because the fifth was locked
// would be punishing the reader for the one thing that went wrong.
func keepAttachments(keeper AttachmentKeeper, list []composer.Attachment) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, attachment := range list {
		if keeper == nil {
			out = append(out, attachment.Path)
			continue
		}
		reference, err := keeper.KeepAttachment(attachment.Path)
		if err != nil || strings.TrimSpace(reference) == "" {
			out = append(out, attachment.Path)
			continue
		}
		out = append(out, reference)
	}
	return out
}

// submitSend is the ONE send door: the fork between the steer door and the post
// door, with any attachments threaded through whichever one the composer is
// bound to. [App.submit] is this call with no files.
//
// A ROOM STILL BEING MINTED HOLDS THE SEND instead of performing it, and this is
// the last place that can be decided — everything below reads [App.session], and
// during a mint that is still the room the reader has just left. See [mintHold]
// for the gap and [App.applyRoomOpened] for where the queue is performed, in the
// new room, one instruction after the window moves into it.
func (a *App) submitSend(send composer.Send) tea.Cmd {
	if a.mint.take(send) {
		return nil
	}
	if a.composerBind.mode == rail.ComposerSteer && a.composerBind.node != "" {
		return a.steerNode(a.composerBind.node, send.Text, send.Attachments...)
	}
	return a.postCmd(send.Text, send.Attachments...)
}

// -- rendering ----------------------------------------------------------------

// appendAttachments draws a journaled message's attachments as reference rows.
//
// 12.5.1's artifact law, in the inbound direction: a thing the conversation
// carries is REFERENCED by what it is and where it came from, never inlined.
// The row says the person's own path, not the digest — v1 renders the same
// thing (internal/tui/markdown.go's renderMediaArtifacts) for the same reason:
// what a reader recognizes, and what they would click, is their file under its
// own name. The content-addressed copy is the record's business, not theirs.
//
// No size and no byte count. The journal does not carry one, and stat-ing the
// file at render time would make a finalized block's rendering depend on a
// filesystem that can change under it — and would print an em dash for every
// message whose original has since been deleted, which is precisely the file
// the CAS still has. 8.2.20's rule settles it: what is not in the record is not
// rendered as though it were.
func (b *messageBlock) appendAttachments(message store.Message) {
	for _, attachment := range message.Attachments {
		label := attachmentLabel(attachment)
		if label == "" {
			continue
		}
		b.segs = append(b.segs, segment{
			kind: segRef, glyph: tokens.GlyphCollapsed, text: label,
			hue: blocks.HueNone, state: blocks.StateSettled, indent: bodyIndent,
		})
	}
}

// attachmentLabel is what one journaled reference reads as.
//
// [cas.SourcePath] is the accessor built for exactly this: it unwraps a
// `cas://<digest>/<path>` back to the path a person would recognize, and hands
// a plain path straight back. So a row written by a window with no CAS behind
// it — the honest degradation above — renders identically to one written with,
// which is what keeps the fallback invisible to the reader rather than a second
// visual state to explain.
func attachmentLabel(reference string) string {
	if path := strings.TrimSpace(cas.SourcePath(reference)); path != "" {
		return path
	}
	return strings.TrimSpace(reference)
}
