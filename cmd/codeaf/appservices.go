package main

// ── codeaf app: KEPT-RUNNING SERVICES (V77) ────────────────────────────────
//
// ONE SUPERVISOR FOR THE WHOLE APP. A command a person agreed to keep running
// belongs to the machine, not to the conversation that asked for it: it keeps
// running while that conversation is let go, while the app is quit, and after
// codeaf crashes. So it is held by one [service.Supervisor] at
// <home>/services, opened once by the app process and handed to every
// conversation the app assembles ([appServiceDoor], read by assembleEngine). A
// second codeaf holding the store already is answered with the capability
// absent here, never with a second supervisor over the same processes.
//
// A service is listed with its agent's other processes, as a process whose
// `service` is set (appwire/services.go), and every change to it is said as
// process.changed. Its id is "<agent>:s<n>".
//
// A FAILURE EPISODE IS ONE INBOX LINE AND ONE NOTICE. The supervisor names
// each run of trouble ([service.Row.Episode]); the first time the app hears of
// an open one it adds one Inbox line and leaves one notice in the chat that
// started it, and every later change of that episode updates the same line.
// When the episode closes — healthy again, or stopped — the line moves to done,
// which is the Inbox's Today. Nothing here raises an OS notification: that is
// the window's, reading the same events.
//
// What the app quits with is the person's choice: services.stop_all first
// stops every service, and a quit without it leaves them running for the
// next codeaf to take back.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	// appServiceStopAllWait bounds services.stop_all: every grace runs at
	// once, so this is one grace, one kill and room to spare.
	appServiceStopAllWait = service.StopGrace + 5*time.Second
	// appServiceFollowRead is the most of a followed service's output read in
	// one look, and appServiceFollowers how many may follow one service.
	appServiceFollowRead = 64 << 10
	appServiceFollowers  = 8
	// appServiceLineCap is the longest line a follow sends.
	appServiceLineCap = 4 << 10
	// appServiceClosedBy and appServiceQuitBy are what a stopped service says
	// stopped it when the person closed its agent, or quit with stop all.
	appServiceClosedBy = "agent closed"
	appServiceQuitBy   = "quit"
)

// appServiceDoor is the app's one supervisor, nil while kept-running
// processes are absent. assembleEngine hands it to every conversation it
// builds in this process.
var appServiceDoor atomic.Pointer[service.Supervisor]

// appServiceLocal is an agent's own name for a service.
var appServiceLocal = regexp.MustCompile(`^s[1-9][0-9]{0,9}$`)

// openAppServices opens the app's supervisor, or answers nil — the capability
// absent — when this platform cannot run one or another codeaf holds it. Why
// is written to the app's log only.
func openAppServices() *service.Supervisor {
	rt := service.PlatformRuntime()
	if rt == nil {
		return nil
	}
	supervisor, err := service.Open(service.Options{Dir: home.Join("services"), Runtime: rt})
	if err != nil {
		fmt.Fprintln(os.Stderr, "codeaf app: processes cannot be kept running:", err)
		return nil
	}
	return supervisor
}

// appServices is the backend's half of the supervisor: what it has said, the
// open Inbox lines and the follows.
type appServices struct {
	sup    *service.Supervisor
	since  time.Time
	unhear func()
	// order puts one service's episode moves in sequence: the listener's
	// and the replay of what was open when the app started.
	order  sync.Mutex
	mu     sync.Mutex
	closed bool
	lines  map[string]appServiceLine
	// noticed is the episode each service's kept notice is for, said the
	// one said live; notices is the record they are kept in.
	noticed map[string]string
	said    map[string]string
	notices *appNoticeLog
	follows map[string]*appServiceFollow
}

// locked runs fn holding svc.mu, and ordered holding svc.order: every
// critical section here goes through one of them (internal/guard's lock law).
func (svc *appServices) locked(fn func()) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	fn()
}

func (svc *appServices) ordered(fn func()) {
	svc.order.Lock()
	defer svc.order.Unlock()
	fn()
}

// appServiceFollowGate runs between a follow finding the supervisor and
// asking to be admitted; nil but in a test that holds a follow there.
var appServiceFollowGate func()

// appServiceLine is the one Inbox line of a service's open episode.
type appServiceLine struct {
	episode string
	item    string
	ask     appwire.Ask
}

// appServiceFollow is one service's output being followed, by how many.
type appServiceFollow struct {
	consumers int
	stop      chan struct{}
	done      chan struct{}
}

// attachServices gives the backend the app's supervisor and starts hearing
// it. A nil supervisor leaves every services.* refused as unknown.
func (b *appBackend) attachServices(sup *service.Supervisor) {
	if sup == nil {
		return
	}
	svc := &appServices{sup: sup, since: b.now(), lines: map[string]appServiceLine{}, noticed: map[string]string{}, said: map[string]string{}, notices: b.noticeLog(), follows: map[string]*appServiceFollow{}}
	b.locked(func() { b.services = svc })
	svc.unhear = sup.Subscribe(b.serviceSaw)
	// WHAT WAS ALREADY IN TROUBLE IS IN THE INBOX FROM THE START, not from
	// the next check ten seconds on.
	for _, row := range sup.All() {
		svc.ordered(func() { b.serviceEpisode(svc, row, appServiceProcess(row, false), false) })
	}
}

// servicesOf is the backend's supervisor half, or an unknown_method refusal.
func (b *appBackend) servicesOf() (*appServices, error) {
	var svc *appServices
	b.locked(func() { svc = b.services })
	if svc == nil {
		return nil, appwire.Errorf(appwire.CodeUnknownMethod, "processes cannot be kept running here")
	}
	return svc, nil
}

// closeServices stops hearing the supervisor, ends every follow and lets the
// supervisor go, leaving every service running ([service.Supervisor.Close]).
func (b *appBackend) closeServices() {
	var svc *appServices
	b.locked(func() { svc, b.services = b.services, nil })
	if svc == nil {
		return
	}
	svc.unhear()
	svc.locked(func() {
		svc.closed = true
		for id, f := range svc.follows {
			close(f.stop)
			delete(svc.follows, id)
		}
	})
	// A TAIL IS TOLD TO GO AND NOT WAITED FOR. One may be inside an Emit the
	// wire is not taking, and a quit must not wait on a slow window; it reads
	// stop before every send and goes the moment that Emit returns.
	svc.sup.Close()
	appServiceDoor.CompareAndSwap(svc.sup, nil)
}

// appServiceID is a wire id's agent and the supervisor's id, for a service.
func appServiceID(id string) (agent, local string, ok bool) {
	agent, local, ok = appSplitID(id)
	return agent, local, ok && appServiceLocal.MatchString(local)
}

// appServiceError keeps the supervisor's words and gives them the wire's code.
func appServiceError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, service.ErrNotFound):
		return appwire.Errorf(appwire.CodeNotFound, "%s", err.Error())
	case errors.Is(err, service.ErrClosed):
		return appwire.Errorf(appwire.CodeClosed, "%s", err.Error())
	case errors.Is(err, service.ErrRunning), errors.Is(err, service.ErrStopping), errors.Is(err, service.ErrFull):
		return appwire.Errorf(appwire.CodeBusy, "%s", err.Error())
	}
	return appwire.Errorf(appwire.CodeEngine, "%s", redact.Secrets(err.Error()))
}

// appServiceProcess is one service's row as a process on the wire.
func appServiceProcess(row service.Row, removed bool) appwire.Process {
	process := appServiceProcessBase(row, removed)
	process.LastLine = redact.Secrets(appLastLine(row.LogPath))
	return process
}

// appServiceProcessBase is a service as a process WITHOUT its last line: it
// opens no file. processes.page uses it; the legacy doors add the line.
func appServiceProcessBase(row service.Row, removed bool) appwire.Process {
	process := appwire.Process{
		ID: row.Owner + ":" + row.ID, Agent: row.Owner, Title: redact.Secrets(row.Name), Command: redact.Secrets(row.Command),
		Reason:  redact.Secrets(row.Reason),
		EndedAt: row.EndedAt, ExitCode: row.ExitCode,
	}
	if row.StartedAt != nil {
		process.StartedAt = *row.StartedAt
	} else {
		process.StartedAt = row.From.At
	}
	switch {
	case row.Running || row.Stopping || row.State == service.StateStarting || row.State == service.StateRestarting:
		// OWNED OR ON ITS WAY IS RUNNING: a service between restarts, or
		// whose stop could not prove its process gone, is still active.
		process.State, process.EndedAt, process.ExitCode = appwire.ProcessRunning, nil, nil
	case row.State == service.StateStopped:
		process.State = appwire.ProcessStopped
	case row.State == service.StateFailed || row.State == service.StateGaveUp:
		process.State = appwire.ProcessFailed
	default:
		process.State = appwire.ProcessEnded
	}
	s := &appwire.Service{
		Name: redact.Secrets(row.Name), Dir: row.Dir, Timing: appServiceTiming(),
		Health:  appwire.ServiceHealth{Kind: string(row.Health.Kind), Port: row.Health.Port, URL: redact.Secrets(row.Health.URL), Cmd: redact.Secrets(row.Health.Cmd), Words: redact.Secrets(row.HealthWords)},
		OpenURL: redact.Secrets(row.OpenURL), Policy: string(row.Policy), Lifecycle: string(row.State), Reason: process.Reason,
		Running: row.Running, Stopping: row.Stopping, PID: row.PID, StartedHow: row.StartedHow,
		StartedBy: appwire.ServiceOrigin{Label: redact.Secrets(row.From.Label), Turn: row.From.Turn, CallID: row.From.CallID, At: row.From.At},
		Restarts:  row.Restarts, RestartLimit: row.RestartLimit, BackoffUntil: row.BackoffUntil,
		ReAdopted: row.ReAdopted, ReAdoptedAt: row.ReAdoptedAt, PortConflict: row.PortConflict,
		Episode: row.Episode, EpisodeOpen: row.EpisodeOpen, EpisodeReason: redact.Secrets(row.EpisodeReason),
		EpisodeStartedAt: row.EpisodeStartedAt, EpisodeResolvedAt: row.EpisodeResolvedAt,
		StoppedBy: row.StoppedBy, OutputTruncated: row.OutputTruncated, Removed: removed,
	}
	if row.Check != nil {
		s.Check = &appwire.ServiceCheck{OK: row.Check.OK, At: row.Check.At, Fails: row.Check.Fails, Detail: redact.Secrets(row.Check.Detail)}
	}
	process.Service = s
	return process
}

// serviceProcesses is one agent's services as processes, for processes.list.
func (b *appBackend) serviceProcesses(agent string) []appwire.Process {
	svc, err := b.servicesOf()
	if err != nil {
		return nil
	}
	var out []appwire.Process
	for _, row := range svc.sup.List(agent) {
		out = append(out, appServiceProcess(row, false))
	}
	return out
}

// serviceKeeps says an agent has a service up or on its way, which keeps its
// conversation held through the idle sweep.
func (b *appBackend) serviceKeeps(agent string) bool {
	svc, err := b.servicesOf()
	if err != nil {
		return false
	}
	for _, row := range svc.sup.List(agent) {
		if row.Running || row.Stopping || row.State == service.StateStarting || row.State == service.StateRestarting {
			return true
		}
	}
	return false
}

// fenceAgentServices holds every new start off an agent being closed, until
// the returned func ([service.Supervisor.Fence]).
func (b *appBackend) fenceAgentServices(agent string) func() {
	svc, err := b.servicesOf()
	if err != nil {
		return func() {}
	}
	return svc.sup.Fence(agent)
}

// stopAgentServices stops every service of an agent the person closed.
func (b *appBackend) stopAgentServices(agent string) error {
	svc, err := b.servicesOf()
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), appServiceStopAllWait)
	defer cancel()
	_, err = svc.sup.StopOwnerWait(ctx, agent, appServiceClosedBy)
	return appServiceError(err)
}

// ── what the supervisor says ────────────────────────────────────────────────

// serviceSaw says one change on the wire, and keeps its episode's Inbox line
// and notice in step. It runs on the supervisor's listener and returns
// promptly: every write it makes is a queued event.
func (b *appBackend) serviceSaw(event service.Event) {
	defer guard.Recover("app/service")
	svc, err := b.servicesOf()
	if err != nil {
		return
	}
	var closed bool
	svc.locked(func() { closed = svc.closed })
	if closed {
		return
	}
	process := appServiceProcess(event.Row, event.Removed)
	b.emit.Emit(appwire.EventProcessChanged, appwire.ProcessChanged{Process: process})
	if event.Removed {
		b.serviceFollowEnd(svc, process.ID)
	}
	svc.ordered(func() { b.serviceEpisode(svc, event.Row, process, event.Removed) })
}

// serviceEpisode moves one service's Inbox line and notice.
func (b *appBackend) serviceEpisode(svc *appServices, row service.Row, process appwire.Process, removed bool) {
	key := process.ID
	now := b.now()
	// AN EPISODE ENDS WHEN THE SUPERVISOR SAYS IT ENDED, so a late or
	// replayed event never moves its line's time to the moment it was heard.
	resolved := now
	if row.EpisodeResolvedAt != nil {
		resolved = *row.EpisodeResolvedAt
	}
	var line appServiceLine
	var lined bool
	var said string
	svc.locked(func() {
		line, lined = svc.lines[key]
		said = svc.said[key]
	})
	// A line whose episode is over, or replaced by a newer one, is resolved.
	if lined && (line.episode != row.Episode || !row.EpisodeOpen || removed) {
		by := appwire.ResolvedRecovered
		if row.State == service.StateStopped || removed || line.episode != row.Episode {
			by = appwire.ResolvedStopped
		}
		at := now
		if line.episode == row.Episode {
			at = resolved
		}
		b.serviceLineResolved(svc, key, line, by, at)
		lined = false
	}
	if row.Episode == "" || removed {
		if removed {
			svc.locked(func() {
				delete(svc.said, key)
				delete(svc.noticed, key)
			})
		}
		return
	}
	ask := appServiceAsk(row, process)
	if row.EpisodeOpen {
		b.serviceNotice(svc, row, key, ask.Title)
		if lined && line.ask.Title == ask.Title && line.ask.Detail == ask.Detail {
			return
		}
		svc.locked(func() {
			svc.lines[key] = appServiceLine{episode: row.Episode, item: appInboxID(row.Owner, ask.ID), ask: ask}
		})
		if b.inbox != nil {
			b.inboxOpened(ask)
		}
		return
	}
	// AN EPISODE THAT OPENED AND CLOSED UNHEARD — a failure that recovered
	// between two looks — still gets its one line, straight into done, and its
	// one notice. One that closed before this app opened is history, not news.
	if row.EpisodeResolvedAt == nil || row.EpisodeResolvedAt.Before(svc.since) {
		return
	}
	b.serviceNotice(svc, row, key, ask.Title)
	if said != row.Episode && b.inbox != nil {
		b.inboxOpened(ask)
		by := appwire.ResolvedRecovered
		if row.State == service.StateStopped {
			by = appwire.ResolvedStopped
		}
		b.serviceLineResolved(svc, key, appServiceLine{episode: row.Episode, item: appInboxID(row.Owner, ask.ID), ask: ask}, by, resolved)
	}
}

// serviceNotice leaves an episode's one notice in its chat: said once live,
// and kept in the agent's record (appnotices.go). A notice the record already
// holds — an episode still open when the app was opened again — is in the
// chat's history and is not said again. One the record could not take is said
// live once and written again at the episode's next change, until it is kept.
func (b *appBackend) serviceNotice(svc *appServices, row service.Row, key, text string) {
	var kept, said bool
	svc.locked(func() { kept, said = svc.noticed[key] == row.Episode, svc.said[key] == row.Episode })
	if kept {
		return
	}
	at := b.now()
	if row.EpisodeStartedAt != nil {
		at = *row.EpisodeStartedAt
	}
	notice := appServiceNotice{Agent: row.Owner, Ref: key, Episode: row.Episode, Text: text, At: at}
	if svc.notices.Noticed(notice) {
		svc.locked(func() { svc.noticed[key], svc.said[key] = row.Episode, row.Episode })
		return
	}
	if err := svc.notices.Append(notice); err != nil {
		fmt.Fprintln(os.Stderr, "codeaf app: a kept-running process's notice is not kept yet:", err)
	} else {
		svc.locked(func() { svc.noticed[key] = row.Episode })
	}
	if !said {
		svc.locked(func() { svc.said[key] = row.Episode })
		b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appServiceNoticeEntry(notice)})
	}
}

// appServiceAsk is a service's trouble as the Inbox's line.
func appServiceAsk(row service.Row, process appwire.Process) appwire.Ask {
	name := process.Title
	title := name + " is not answering"
	switch row.State {
	case service.StateRestarting:
		title = fmt.Sprintf("%s stopped · restarting, %d of %d", name, row.Restarts, row.RestartLimit)
	case service.StateGaveUp:
		title = fmt.Sprintf("%s stopped · not restarted again after %d tries", name, row.RestartLimit)
	case service.StateFailed, service.StateNotRunning:
		title = name + " stopped"
	case service.StateHealthy:
		title = name + " is healthy again"
	}
	detail := redact.Secrets(row.EpisodeReason)
	if row.Check != nil && !row.Check.OK && row.Check.Detail != "" {
		detail = redact.Secrets(row.Check.Detail)
	}
	return appwire.Ask{
		ID: "service:" + row.Episode, Agent: row.Owner, Kind: appwire.AskQuestion, Subkind: appwire.SubkindServiceHealth,
		Title: title, Detail: detail,
		Options: []appwire.AskOption{{ID: appwire.OptionRestart, Label: "Restart"}, {ID: appwire.OptionStop, Label: "Stop"}},
		Subject: &appwire.AskSubject{Kind: appwire.SubjectService, Ref: process.ID},
	}
}

// serviceLineResolved moves a service's line to done and says so.
func (b *appBackend) serviceLineResolved(svc *appServices, key string, line appServiceLine, by string, at time.Time) {
	svc.locked(func() {
		if have, ok := svc.lines[key]; ok && have.item == line.item {
			delete(svc.lines, key)
		}
	})
	in := b.inbox
	if in == nil {
		return
	}
	receipt := "Healthy again · " + at.Local().Format("15:04")
	if by == appwire.ResolvedStopped {
		receipt = "Stopped · " + at.Local().Format("15:04")
	}
	moved := false
	in.locked(func() {
		item := in.items[line.item]
		if item == nil {
			return
		}
		moved = true
		if held := in.held[line.item]; held != nil && !held.sent {
			held.timer.Stop()
		}
		delete(in.held, line.item)
		delete(in.items, line.item)
		delete(in.closing, line.item)
		delete(in.from, line.item)
		for i, have := range in.order {
			if have == line.item {
				in.order = append(in.order[:i], in.order[i+1:]...)
				break
			}
		}
		item.List = appwire.InboxDone
		item.Answered = &appwire.InboxAnswered{By: by, At: at, Text: receipt}
		in.done = append(in.done, *item)
		if len(in.done) > appInboxDone {
			in.done = in.done[len(in.done)-appInboxDone:]
		}
	})
	if !moved {
		return
	}
	in.next.Emit(appwire.EventInboxResolved, appwire.InboxResolvedEvent{ID: line.item, By: by, Receipt: receipt})
	b.inboxCounts()
}

// answerInboxItem sends an Inbox answer once its hold is over: a service's
// line to the supervisor, anything else through the conversation's door.
func (b *appBackend) answerInboxItem(id, agent, ask string, answer appwire.AskAnswer) error {
	episode, ok := strings.CutPrefix(ask, "service:")
	if !ok {
		return b.AnswerAskWith(agent, ask, answer)
	}
	if err := appValidateServiceAnswer(answer.Option); err != nil {
		return err
	}
	local, _, _ := strings.Cut(episode, ".")
	process := agent + ":" + local
	var err error
	switch answer.Option {
	case appwire.OptionRestart:
		_, err = b.RestartService(process)
	case appwire.OptionStop:
		_, err = b.StopService(process)
	default:
		err = appwire.Errorf(appwire.CodeBadParams, "a kept-running process is answered restart or stop")
	}
	// A RESTART LEAVES THE LINE WAITING until the service is healthy again,
	// and it may be answered again in the meantime.
	if in := b.inbox; in != nil {
		in.locked(func() {
			delete(in.held, id)
			delete(in.from, id)
		})
	}
	return err
}

func appValidateServiceAnswer(option string) error {
	switch option {
	case appwire.OptionRestart, appwire.OptionStop:
		return nil
	}
	return appwire.Errorf(appwire.CodeBadParams, "a kept-running process is answered restart or stop")
}

// laterServiceItem puts a service's line aside, or back. It answers false
// for an item that is not a service's.
func (b *appBackend) laterServiceItem(id, ask string, later bool) (bool, error) {
	if !strings.HasPrefix(ask, "service:") {
		return false, nil
	}
	in := b.inbox
	var item appwire.InboxItem
	found := false
	in.locked(func() {
		if have := in.items[id]; have != nil {
			have.Ask.Later = later
			have.List = appwire.InboxWaiting
			if later {
				have.List = appwire.InboxLater
			}
			item, found = *have, true
		}
	})
	if !found {
		return true, appwire.Errorf(appwire.CodeNotFound, "the Inbox item %s is no longer waiting", id)
	}
	in.next.Emit(appwire.EventInboxAdded, appwire.InboxAddedEvent{Item: item})
	b.inboxCounts()
	return true, nil
}

// ── appwire.Servicer ────────────────────────────────────────────────────────

func (b *appBackend) serviceRow(id string) (*appServices, string, string, error) {
	svc, err := b.servicesOf()
	if err != nil {
		return nil, "", "", err
	}
	agent, local, ok := appServiceID(id)
	if !ok {
		return nil, "", "", appwire.Errorf(appwire.CodeBadParams, "%s is not a kept-running process", id)
	}
	return svc, agent, local, nil
}

// ListServices is one agent's services, or every agent's.
func (b *appBackend) ListServices(agent string) ([]appwire.Process, error) {
	svc, err := b.servicesOf()
	if err != nil {
		return nil, err
	}
	rows := svc.sup.All()
	if agent != "" {
		rows = svc.sup.List(agent)
	}
	out := make([]appwire.Process, 0, len(rows))
	for _, row := range rows {
		out = append(out, appServiceProcess(row, false))
	}
	return out, nil
}

// GetService is one service, or one removed with its output kept.
func (b *appBackend) GetService(id string) (appwire.Process, error) {
	svc, agent, local, err := b.serviceRow(id)
	if err != nil {
		return appwire.Process{}, err
	}
	row, err := svc.sup.Get(agent, local)
	if errors.Is(err, service.ErrNotFound) {
		gone, archErr := svc.sup.Archive(agent, local)
		if archErr != nil {
			return appwire.Process{}, appServiceError(err)
		}
		removed := gone.Removed
		return appwire.Process{
			ID: id, Agent: agent, Title: redact.Secrets(gone.Name), Command: redact.Secrets(gone.Command),
			State: appwire.ProcessStopped, EndedAt: &removed,
			Service: &appwire.Service{Name: redact.Secrets(gone.Name), Lifecycle: string(service.StateStopped), Removed: true, OutputTruncated: gone.OutputTruncated, Timing: appServiceTiming()},
		}, nil
	}
	if err != nil {
		return appwire.Process{}, appServiceError(err)
	}
	return appServiceProcess(row, false), nil
}

func (b *appBackend) serviceAct(id string, act func(*service.Supervisor, string, string) (service.Row, error)) (appwire.Process, error) {
	svc, agent, local, err := b.serviceRow(id)
	if err != nil {
		return appwire.Process{}, err
	}
	row, err := act(svc.sup, agent, local)
	if err != nil && row.ID == "" {
		return appwire.Process{}, appServiceError(err)
	}
	return appServiceProcess(row, false), appServiceError(err)
}

// SetServicePolicy is the person changing what happens at the next failure.
func (b *appBackend) SetServicePolicy(id, policy string) (appwire.Process, error) {
	return b.serviceAct(id, func(s *service.Supervisor, agent, local string) (service.Row, error) {
		return s.SetPolicy(agent, local, service.Policy(policy))
	})
}

// RestartService is the person's restart: outside the automatic budget.
func (b *appBackend) RestartService(id string) (appwire.Process, error) {
	return b.serviceAct(id, (*service.Supervisor).Restart)
}

// StartService starts a stopped service again from its saved command.
func (b *appBackend) StartService(id string) (appwire.Process, error) {
	return b.serviceAct(id, (*service.Supervisor).Start)
}

// StopService stops one service and keeps its output.
func (b *appBackend) StopService(id string) (appwire.Process, error) {
	return b.serviceAct(id, (*service.Supervisor).Stop)
}

// RemoveService takes a service with no process off the list, keeping its
// output readable by its id.
func (b *appBackend) RemoveService(id string) error {
	svc, agent, local, err := b.serviceRow(id)
	if err != nil {
		return err
	}
	return appServiceError(svc.sup.Remove(agent, local))
}

// StopAllServices stops every service before a quit that asked to, and says
// how many were up. An error names what could not be proved stopped.
func (b *appBackend) StopAllServices() (int, error) {
	svc, err := b.servicesOf()
	if err != nil {
		return 0, err
	}
	n := svc.sup.Running()
	ctx, cancel := context.WithTimeout(context.Background(), appServiceStopAllWait)
	defer cancel()
	if err := svc.sup.StopAll(ctx, appServiceQuitBy); err != nil {
		return n, appServiceError(err)
	}
	return n, nil
}

// ServiceOutput is a bounded page of a service's output.
func (b *appBackend) ServiceOutput(id string, from, max int) (appwire.OutputPage, error) {
	svc, agent, local, err := b.serviceRow(id)
	if err != nil {
		return appwire.OutputPage{}, err
	}
	page, err := svc.sup.Output(agent, local, from, max)
	return appServicePage(page), appServiceError(err)
}

// ServiceTail is the last lines of a service's output.
func (b *appBackend) ServiceTail(id string, lines int) (appwire.OutputPage, error) {
	svc, agent, local, err := b.serviceRow(id)
	if err != nil {
		return appwire.OutputPage{}, err
	}
	page, err := svc.sup.Tail(agent, local, lines)
	return appServicePage(page), appServiceError(err)
}

func appServicePage(page service.OutputPage) appwire.OutputPage {
	lines := make([]string, len(page.Lines))
	for i, line := range page.Lines {
		lines[i] = redact.Secrets(line)
	}
	return appwire.OutputPage{Lines: lines, FirstLine: page.FirstLine, NextLine: page.NextLine, TotalLines: page.TotalLines, Truncated: page.Truncated, Partial: page.Partial}
}

// ── the processes.* doors, for a service ────────────────────────────────────

// serviceProcessOutput is processes.output for a service.
func (b *appBackend) serviceProcessOutput(id string, from int) ([]string, int, error) {
	page, err := b.ServiceOutput(id, from, 0)
	if err != nil {
		return nil, 0, err
	}
	return page.Lines, page.NextLine, nil
}

// serviceProcessStop is processes.stop for a service.
func (b *appBackend) serviceProcessStop(id string) error {
	_, err := b.StopService(id)
	return err
}

// FOLLOWING IS COUNTED. Every view that follows a service's output adds one,
// every unfollow takes one, and the tail stops when none is left — or when the
// service is removed, or the app closes. The tail reads the live log by its
// byte offset, so it costs one stat a look while nothing is written, and a log
// cut back by its rotation is read again from its start.

// serviceFollow starts or stops one view's follow of a service.
func (b *appBackend) serviceFollow(id string, follow bool) error {
	svc, agent, local, err := b.serviceRow(id)
	if err != nil {
		return err
	}
	path, err := svc.sup.OutputPath(agent, local)
	if err != nil {
		return appServiceError(err)
	}
	if appServiceFollowGate != nil {
		appServiceFollowGate()
	}
	var start *appServiceFollow
	refused, closed := false, false
	svc.locked(func() {
		// ADMISSION IS UNDER THE SAME LOCK THE CLOSE DRAINS UNDER, so a
		// follow that found the supervisor just before the quit is refused
		// here rather than started after the drain, where nothing would end it.
		if closed = svc.closed; closed {
			return
		}
		current := svc.follows[id]
		switch {
		case !follow && current != nil:
			current.consumers--
			if current.consumers <= 0 {
				close(current.stop)
				delete(svc.follows, id)
			}
		case follow && current != nil:
			if current.consumers >= appServiceFollowers {
				refused = true
			} else {
				current.consumers++
			}
		case follow:
			start = &appServiceFollow{consumers: 1, stop: make(chan struct{}), done: make(chan struct{})}
			svc.follows[id] = start
		}
	})
	if closed {
		if follow {
			return appwire.Errorf(appwire.CodeClosed, "codeaf is closing")
		}
		return nil
	}
	if refused {
		return appwire.Errorf(appwire.CodeBusy, "%d views already follow %s", appServiceFollowers, id)
	}
	// An unfollow does not wait for the tail either (closeServices says why).
	if start != nil {
		offset := int64(0)
		if info, err := os.Stat(path); err == nil {
			offset = info.Size()
		}
		guard.Go("app/service-follow", func() { b.serviceTail(id, path, offset, start) })
	}
	return nil
}

// serviceFollowEnd ends a removed service's follow, whoever was following.
func (b *appBackend) serviceFollowEnd(svc *appServices, id string) {
	svc.locked(func() {
		if f := svc.follows[id]; f != nil {
			close(f.stop)
			delete(svc.follows, id)
		}
	})
}

// serviceTail sends a followed service's new lines as process.output.
func (b *appBackend) serviceTail(id, path string, offset int64, f *appServiceFollow) {
	defer close(f.done)
	ticker := time.NewTicker(appFollowEvery)
	defer ticker.Stop()
	buf := make([]byte, appServiceFollowRead)
	for {
		select {
		case <-f.stop:
			return
		case <-ticker.C:
		}
		lines, next := appReadFrom(path, offset, buf)
		offset = next
		if len(lines) == 0 {
			continue
		}
		// Told to go while it read: what it read is not sent.
		select {
		case <-f.stop:
			return
		default:
		}
		b.emit.Emit(appwire.EventProcessOutput, appwire.ProcessOutput{ID: id, Lines: lines})
	}
}

// appReadFrom reads the complete lines written to path since offset, at most
// len(buf) bytes of them, and answers the offset after the last. A file
// shorter than offset was cut back, and is read from its start.
func appReadFrom(path string, offset int64, buf []byte) ([]string, int64) {
	file, err := os.Open(path)
	if err != nil {
		return nil, offset
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, offset
	}
	if info.Size() < offset {
		offset = 0
	}
	n, err := file.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, offset
	}
	chunk := buf[:n]
	end := strings.LastIndexByte(string(chunk), '\n')
	if end < 0 {
		if n == len(buf) {
			// One line longer than a whole read is sent cut, and skipped.
			return []string{redact.Secrets(appCut(string(chunk)))}, offset + int64(n)
		}
		return nil, offset
	}
	var lines []string
	for _, line := range strings.Split(string(chunk[:end]), "\n") {
		lines = append(lines, redact.Secrets(appCut(strings.TrimRight(line, "\r"))))
	}
	return lines, offset + int64(end) + 1
}

func appCut(line string) string {
	if len(line) <= appServiceLineCap {
		return line
	}
	return strings.ToValidUTF8(line[:appServiceLineCap], "") + " …[line cut]"
}

// ── the keep-running ask ────────────────────────────────────────────────────

// appKeepOptions is the keep-running card's three answers in the app's names.
func appKeepOptions(q session.Question) ([]appOption, bool) {
	if q.Kind != session.QuestionKeep {
		return nil, false
	}
	ids := map[string]string{session.KeepAllowKey: appwire.OptionAllow, session.KeepAllowNeverKey: appwire.OptionAllowNever, session.KeepDenyKey: appwire.OptionDeny}
	var out []appOption
	for _, option := range q.Options {
		id, known := ids[option.Key]
		if !known {
			return nil, false
		}
		out = append(out, appOption{id: id, key: option.Key, label: option.Label})
	}
	return out, true
}

// appServiceTiming is the supervisor's fixed rules on the wire, from its own
// constants.
func appServiceTiming() appwire.ServiceTiming {
	return appwire.ServiceTiming{
		CheckEverySeconds: int(service.CheckEvery / time.Second), CheckTimeoutSeconds: int(service.CheckTimeout / time.Second),
		UnhealthyAfter: service.UnhealthyAfter, FirstBackoffSeconds: int(service.FirstBackoff / time.Second),
		RestartLimit: service.RestartLimit, RestartWindowSeconds: int(service.RestartWindow / time.Second),
		HealthyResetSeconds: int(service.RestartWindow / time.Second), StopGraceSeconds: int(service.StopGrace / time.Second),
	}
}

// appKeepFacts is a keep-running card's proposal, typed, from the card the
// session put on the question.
func appKeepFacts(q session.Question) *appwire.KeepRunning {
	if q.Kind != session.QuestionKeep || q.Keep == nil {
		return nil
	}
	card := q.Keep
	return &appwire.KeepRunning{
		Name: redact.Secrets(card.Name), Command: redact.Secrets(card.Command), Dir: card.Dir,
		Health: appwire.ServiceHealth{Kind: string(card.Health.Kind), Port: card.Health.Port, URL: redact.Secrets(card.Health.URL), Cmd: redact.Secrets(card.Health.Cmd), Words: redact.Secrets(card.HealthWords)},
		Policy: string(card.Policy), CallID: card.CallID, Timing: appServiceTiming(),
	}
}

// noticeLog is the records of the episodes' notices, beside the app's file.
func (b *appBackend) noticeLog() *appNoticeLog {
	b.noticesOnce.Do(func() {
		dir := filepath.Dir(appStatePath())
		if b.state != nil {
			dir = filepath.Dir(b.state.where())
		}
		b.notices = newAppNoticeLog(filepath.Join(dir, appNoticeFolder))
	})
	return b.notices
}
