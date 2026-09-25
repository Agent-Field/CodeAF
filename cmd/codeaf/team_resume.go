package main

import (
	"errors"
	"net"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── A TEAM CONVERSATION NOBODY HOLDS IS OPENED BY ITS HOST ──────────────────
//
// Team traffic wakes the conversation it is for (internal/session's
// team_wakewatch.go), and a conversation wakes only if some process holds it: a
// directive to a member whose window closed an hour ago, and whose host let it
// go, would wake nobody. So the session asks the engine, through
// [session.SetTeamResume], to open that conversation where it lives, and this
// is the engine's answer: a hello to the session host of the member's folder,
// naming its transcript, exactly as a window opening it would send, and then
// the connection is let go.
//
// THE HOST KEEPS WHAT A HELLO OPENED. A conversation with no surface is kept
// while it does anything at all (internal/enginehost's idle policy), so the
// member opened here runs its woken turn with nobody attached, and a window
// that opens it later is handed that same running conversation rather than a
// second one onto its journal.
//
// IT IS SET ONLY WHERE A PROCESS IS AN ENGINE: the host daemon and the pipe
// engine ([runRemoteEngine]), which is every conversation on the ordinary road
// and every one over --host, since the far `codeaf engine` is one of those two.
// A launch that keeps its conversation in the terminal's own process
// (--no-host, --once) has no door, and the session says in the Traffic that it
// could not wake a member rather than pretending it did.

// teamResumeHello is how long the hello may take: a host may have to start,
// and the conversation has to be loaded before the welcome comes back.
const teamResumeHello = 45 * time.Second

// armTeamResume gives this engine process the door.
func armTeamResume() {
	session.SetTeamResume(func(file, workspace string) error {
		return resumeTeamConversation(file, workspace, attachEngineHost)
	})
}

// resumeTeamConversation opens one conversation in its folder's host, over
// attach (the host dialled, and started when it is not running).
func resumeTeamConversation(file, workspace string, attach func(string) (net.Conn, error)) error {
	file, workspace = strings.TrimSpace(file), strings.TrimSpace(workspace)
	if file == "" {
		return errors.New("no transcript to open")
	}
	if workspace == "" {
		return errors.New("the team has no folder recorded for it")
	}
	folder, err := engineWorkspace(workspace)
	if err != nil {
		return err
	}
	conn, err := attach(folder)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(teamResumeHello))
	client, err := remote.Dial(conn, "", remote.Hello{Workspace: folder, Session: file})
	if err != nil {
		_ = conn.Close()
		return err
	}
	return client.Close()
}
