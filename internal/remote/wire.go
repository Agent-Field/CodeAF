// Package remote is the wire between a surface on one machine and an engine on
// another. The surface half dials `ssh <host> aforge engine …` and speaks this
// protocol over the pipes; the engine half wraps an ordinary *session.Agent and
// answers. Both halves import THIS file and nothing of each other.
//
// THE CONTRACT IS THE ENVELOPE, NOT THE PAYLOADS. Payloads are the session
// package's own types carried as JSON — both ends compile against
// internal/session, so a field added there travels without a wire change. The
// one exception is [EventWire], because error does not survive encoding/json.
//
// Frames are JSON, one per line (a journal's own framing, for a journal's own
// reason: a torn write is one lost line, not a lost stream).
package remote

import (
	"encoding/json"
	"errors"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// Version is the protocol's version. The hello and the welcome both carry it,
// and a mismatch is a refusal at the door — two builds that might disagree
// about a frame must not guess at each other.
const Version = 1

// Frame is one line on the wire, either direction.
type Frame struct {
	// Kind says what this frame is: "hello", "welcome", "call", "result",
	// "event", "closed", "fatal".
	Kind string `json:"kind"`
	// ID correlates a call with its result, and an event with the Submit that
	// opened its stream. The client mints call ids; the server mints stream ids
	// and names the stream in the call's result.
	ID uint64 `json:"id,omitempty"`
	// Method is the call's name, on "call" frames only — a [Method] constant.
	Method string `json:"method,omitempty"`
	// Payload is the frame's body, shaped by Kind and Method.
	Payload json.RawMessage `json:"payload,omitempty"`
	// Error is a call that failed, on "result" frames, and the reason on
	// "fatal" frames.
	Error string `json:"error,omitempty"`
}

// The methods, one per door. The Agent group mirrors tui3.Agent exactly (plus
// the rewind pair rewind.go type-asserts for); the Session group is the doors
// only a remote surface needs, because a local one reads the disk directly.
const (
	// Agent — payloads are the method's own argument struct below; results are
	// the return values likewise.
	MethodSubmit          = "Submit"          // SubmitArgs → StreamRef, then "event" frames
	MethodSubmitImage     = "SubmitImage"     // SubmitImageArgs → StreamRef, then "event" frames
	MethodFollowUp        = "FollowUp"        // SubmitArgs → StreamRef, then "event" frames
	MethodInterrupt       = "Interrupt"       // nothing → nothing
	MethodCompact         = "Compact"         // nothing → nothing (error carries the failure)
	MethodClose           = "Close"           // nothing → nothing
	MethodModel           = "Model"           // nothing → string
	MethodSetModel        = "SetModel"        // string → nothing
	MethodSetContext      = "SetContextWindow" // int → nothing
	MethodReasoningFor    = "ReasoningFor"    // string → string
	MethodSetReasoningFor = "SetReasoningFor" // ReasoningArgs → nothing
	MethodConsent         = "ResolveConsent"  // ConsentArgs → nothing
	MethodConsentRemember = "ResolveConsentRemember" // ConsentArgs → nothing
	MethodHarness         = "ResolveHarness"  // HarnessArgs → nothing
	MethodConnect         = "ResolveConnect"  // ConnectArgs → nothing
	MethodConnectKey      = "ResolveConnectKey" // ConnectArgs → nothing
	MethodNoteConnected   = "NoteConnected"   // ConnectedArgs → nothing
	MethodTitle           = "Title"           // nothing → string
	MethodUsage           = "Usage"           // nothing → session.Usage
	MethodContextTokens   = "ContextTokens"   // nothing → int
	MethodTranscript      = "Transcript"      // nothing → []session.DisplayEntry
	MethodRewindPoints    = "RewindPoints"    // nothing → []session.RewindPoint
	MethodRewindAt        = "RewindAt"        // int → []session.DisplayEntry

	// Session doors.
	MethodSessionsRecent = "Sessions.Recent" // nothing → []session.Summary
	MethodSessionNew     = "Session.New"     // nothing → Welcome (the engine swaps to a fresh session)
	MethodSessionOpen    = "Session.Open"    // string (path) → Welcome (the engine swaps to that session)
)

// Hello is the client's first frame ("hello"). Workspace is the path AS TYPED
// after the colon — empty means the engine's own home — and the engine answers
// with the path it resolved.
type Hello struct {
	Version   int    `json:"version"`
	Workspace string `json:"workspace,omitempty"`
	// Session is an explicit session file to open, empty for the workspace's
	// latest-or-new (the same meaning the --session flag has locally).
	Session string `json:"session,omitempty"`
}

// Welcome is the server's answer ("welcome"): the facts a surface needs before
// its first frame, which are the same facts newApp reads off a local agent.
type Welcome struct {
	Version     int    `json:"version"`
	Workspace   string `json:"workspace"`
	SessionFile string `json:"sessionFile"`
	Resumed     bool   `json:"resumed"`
	Model       string `json:"model"`
	Title       string `json:"title,omitempty"`
	// Note is a sentence worth showing once — "session open elsewhere, started
	// a new one" travels here.
	Note string `json:"note,omitempty"`
}

// SubmitArgs carries Submit and FollowUp.
type SubmitArgs struct {
	Text string `json:"text"`
}

// SubmitImageArgs carries SubmitImage. Images travel with their bytes filled
// in — the engine has no way to read a path on the surface's disk — and the
// engine writes them to its own image store before submitting, so the journal
// holds references the way it always does.
type SubmitImageArgs struct {
	Text   string          `json:"text"`
	Images []session.Image `json:"images"`
}

// StreamRef is the result of the three stream-opening calls: the id every
// "event" frame of that turn carries. The stream ends with a "closed" frame
// bearing the same id, which is the channel close.
type StreamRef struct {
	Stream uint64 `json:"stream"`
}

type ReasoningArgs struct {
	Model string `json:"model"`
	Level string `json:"level"`
}

type ConsentArgs struct {
	ID    uint64               `json:"id"`
	Allow bool                 `json:"allow"`
	Scope session.ConsentScope `json:"scope,omitempty"`
}

type HarnessArgs struct {
	ID    uint64 `json:"id"`
	Run   bool   `json:"run"`
	Model string `json:"model,omitempty"`
}

type ConnectArgs struct {
	ID      string `json:"id"`
	Approve bool   `json:"approve,omitempty"`
	Key     string `json:"key,omitempty"`
}

type ConnectedArgs struct {
	Service string `json:"service"`
	Account string `json:"account"`
}

// EventWire is a session.Event that survives JSON. Err is an interface and
// marshals to nothing, so the string rides beside it and shadows it on the
// wire; [EventWire.Event] restores the one field that needs restoring.
type EventWire struct {
	session.Event
	Err string `json:"Err,omitempty"`
}

// WireEvent wraps an event for sending.
func WireEvent(ev session.Event) EventWire {
	w := EventWire{Event: ev}
	if ev.Err != nil {
		w.Err = ev.Err.Error()
	}
	w.Event.Err = nil
	return w
}

// Unwire unwraps a received event.
func (w EventWire) Unwire() session.Event {
	ev := w.Event
	if w.Err != "" {
		ev.Err = errors.New(w.Err)
	}
	return ev
}
