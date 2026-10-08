package remote

// whois.go is the one exchange on this wire that is not about a conversation:
// a connection asking the far end WHICH BUILD IT IS, and asking it to go.
//
// ── WHY A PROTOCOL NEEDS A QUESTION ABOUT ITSELF ────────────────────────────
//
// Decision 3 refuses a version mismatch at the door, and for a pipe engine that
// is the whole story: the process on the other end of ssh IS the build that was
// just installed there, so a mismatch means one of the two MACHINES is behind
// and the sentence says so.
//
// A session host broke that reading, and broke it silently. The host outlives
// the connection by design (internal/enginehost), so after `rm bin/codeaf &&
// make build` on the far machine the NEW binary answers `codeaf version` while
// the OLD one is still holding the socket — and `codeaf engine` spliced the new
// surface straight onto it. What came back was the old host's own refusal,
// `engine: this build speaks protocol 3 and the surface speaks 4`, telling the
// person to update a machine they had just updated. The two halves were the
// same build. The half in the middle was not.
//
// So the splice stopped being a blind copy of bytes: `codeaf engine` asks the
// socket what it is and acts on the answer. An older host holding work is
// joined and asked — [WhoIs.StandDownWhenIdle] — to let go when it is quiet; one
// holding nothing is stood down. Nothing on this road kills a busy engine any
// more, and cmd/codeaf's takeover rule has the whole table.
//
// THE QUESTION IS ASKED BEFORE THE HELLO, on a connection that never
// becomes a surface, because a hello would open a conversation — which is the
// expensive, journal-locking thing this exchange exists to avoid doing twice.
//
// ── AND WHY IT IS SAFE TO ASK A HOST TO LEAVE ───────────────────────────────
//
// The host answers the question about itself, so it is the host that decides.
// Work in flight — a surface, a turn, a waiting question — reads [HostSelf.Busy]
// and the door joins it rather than ending anything, because nothing here does
// the ending: StandDown is a request and StandDownWhenIdle is a note. The
// decision and the admission happen under one lock (internal/enginehost's
// [Host.whois] and [Host.open]), so a host that agreed to go refuses the
// conversation arriving a moment later and one that admitted a conversation
// refuses to go.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
)

// WhoIs is the question, and it is asked on a connection's FIRST frame in place
// of a hello.
type WhoIs struct {
	// StandDown asks the host to retire: close its conversations, flush their
	// journals, drop the socket and exit, so the next connection starts a host
	// from whatever binary is on disk now. It is refused while there is work in
	// flight, and the answer says which happened.
	StandDown bool `json:"standDown,omitempty"`
	// IgnoreWatchGrace asks an environment replacement to disregard only a
	// recently active window whose pipe has gone. Attached windows, streams,
	// unanswered questions and handed-off work still keep the host alive.
	// A stalled window may redial onto the replacement, with every journal
	// flushed and reopened, exactly as an older-build replacement already does.
	IgnoreWatchGrace bool `json:"ignoreWatchGrace,omitempty"`
	// StandDownWhenIdle asks for the slot WITHOUT ending anybody's work: the
	// host keeps everything it is holding and retires at its first quiet
	// moment, instead of waiting out its idle policy (cmd/codeaf's takeover
	// rule). It is a note and not a command — the answer never carries
	// Retiring because of it. A host older than this field ignores it, as it
	// ignores every field it does not know, so the answer says whether it was
	// understood ([HostSelf.StandDownWhenIdle]).
	StandDownWhenIdle bool `json:"standDownWhenIdle,omitempty"`
	// Anyway asks for the retirement even with work in flight, and there is
	// exactly one caller: a person typing `codeaf engine --stop` on the machine
	// itself, who has been told what is running and said stop anyway. A turn
	// caught by it stops where it is and keeps its partial reply — the same
	// thing ctrl+c does locally, by the same road ([Session.Close]).
	Anyway bool `json:"anyway,omitempty"`
}

// HostSelf is the answer: what this process is, and what it did with the
// request.
type HostSelf struct {
	// Version is the protocol this build speaks — [Version], filled in by the
	// answering side rather than by whoever built the struct, because ONE
	// SOURCE OF TRUTH about a build's protocol is the constant compiled into
	// it.
	Version int `json:"version"`
	// Build identifies the running executable even when its protocol is unchanged.
	// It is the source the executable was built from, so rebuilding the same commit
	// answers with the same word and only a real source change reads as another
	// build. An empty stamp identifies a host predating this check, not a matching
	// build.
	Build string `json:"build,omitempty"`
	// Busy is work in flight: a surface attached, a turn running, or a question
	// waiting for somebody to come back and answer it.
	Busy bool `json:"busy,omitempty"`
	// Retiring says the host took the stand-down and is on its way out. The
	// asker waits for the socket to stop answering and then starts a fresh one.
	Retiring bool `json:"retiring,omitempty"`
	// Workspace is the directory this host holds, for a sentence that has to
	// name it.
	Workspace string `json:"workspace,omitempty"`

	// StandDownWhenIdle says THIS HOST UNDERSTOOD the request of that name and
	// has recorded it. Absence is false: a host older than the field ignored
	// the ask and cannot retire at a quiet moment nobody told it about, so the
	// door that asked reads this before it promises anything.
	StandDownWhenIdle bool `json:"standDownWhenIdle,omitempty"`

	// ── WHAT A PERSON TYPING `codeaf engine --status` IS TOLD ──────────────
	//
	// The five fields below are the process's account of itself, and they
	// exist because the answer to "which engine is holding this folder" used to
	// be `ps` and a kill by hand. Every one is omitted by a build older than
	// them, which reads as "not said" rather than as a zero: a host that does
	// not name its binary is a host too old to, and the door that asks falls
	// back to the kernel's own answer for the pid.

	// PID is the host process.
	PID int `json:"pid,omitempty"`
	// Binary is the file the host was started from, as it resolved at start.
	Binary string `json:"binary,omitempty"`
	// Revision is the build as a person reads it — the source revision and
	// when it was built — which [HostSelf.Build] is not written for.
	Revision string `json:"revision,omitempty"`
	// Started is when the host process began holding the workspace.
	Started time.Time `json:"started,omitzero"`
	// BuiltAt is the moment the host's binary was built: the stamp `make
	// build` links in, or the binary file's own modification time when there
	// is none. IT IS WHAT DECIDES WHICH OF TWO BUILDS IS THE OLDER ONE, and so
	// which of them gives up the workspace (cmd/codeaf's takeover rule): the
	// source identity says two builds differ and never which came first.
	BuiltAt time.Time `json:"builtAt,omitzero"`
	// Surfaces is how many windows are attached right now, not counting the
	// connection asking.
	Surfaces int `json:"surfaces,omitempty"`
	// Conversations is how many conversations the host is holding open.
	Conversations int `json:"conversations,omitempty"`
}

// ErrNoHostThere is a far end that answered the question with a refusal, which
// is what EVERY BUILD FROM BEFORE THIS EXCHANGE EXISTED does: an unknown first
// frame has always been `engine: the first frame was %q, not a hello`. It is
// the most useful thing a silence could have said — the process holding that
// socket is not this build, and it cannot be asked anything else either.
var ErrNoHostThere = errors.New("remote: the far end did not answer what build it is")

// AskHost puts the question on an already-open connection and reads the answer.
// It owns the connection for the length of the exchange and nothing else uses
// it afterwards: the answer is the whole of what this connection was for.
func AskHost(conn io.ReadWriter, ask WhoIs) (HostSelf, error) {
	payload, err := json.Marshal(ask)
	if err != nil {
		return HostSelf{}, err
	}
	line, err := json.Marshal(Frame{Kind: "whois", Payload: payload})
	if err != nil {
		return HostSelf{}, err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return HostSelf{}, err
	}
	scan := bufio.NewScanner(conn)
	scan.Buffer(make([]byte, 0, 4*1024), 1<<20)
	if !scan.Scan() {
		if err := scan.Err(); err != nil {
			return HostSelf{}, err
		}
		// A far end that hung up without a word is one that could not answer,
		// and it is read exactly as a refusal is: not this build.
		return HostSelf{}, ErrNoHostThere
	}
	var frame Frame
	if err := json.Unmarshal(scan.Bytes(), &frame); err != nil {
		return HostSelf{}, ErrNoHostThere
	}
	if frame.Kind != "whoami" {
		return HostSelf{}, ErrNoHostThere
	}
	var self HostSelf
	if err := json.Unmarshal(frame.Payload, &self); err != nil {
		return HostSelf{}, fmt.Errorf("remote: the answer did not parse: %w", err)
	}
	return self, nil
}

// whois answers the question and ends the connection. It is the server's half,
// reached from the handshake before the version check — because THE WHOLE POINT
// IS TO BE ANSWERABLE BY A BUILD THAT WOULD BE REFUSED.
//
// A connection with no host behind it says so plainly. A bare `codeaf engine`
// on a pipe is nobody's host: it has no socket, nothing can outlive it, and
// there is nothing to ask to leave.
func (s *server) whois(frame Frame) error {
	if s.host == nil {
		return s.refuse("engine: nothing is holding a conversation here")
	}
	var ask WhoIs
	if len(frame.Payload) > 0 {
		if err := json.Unmarshal(frame.Payload, &ask); err != nil {
			return s.refuse("engine: the question did not parse")
		}
	}
	self := s.host(ask)
	self.Version = Version
	self.Build = buildinfo.Identity()
	self.Revision = buildinfo.String()
	// The connection is over either way, and it is over WITHOUT a session: the
	// serve loop reads [server.asked] and returns before it waits for a second
	// line.
	s.asked = true
	return s.send(Frame{Kind: "whoami", Payload: mustJSON(self)})
}
