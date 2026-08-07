package resident

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	serviceHealthTimeout      = 2 * time.Second
	serviceHealthStartupGrace = 2 * time.Second
	serviceRestartBackoff     = 250 * time.Millisecond
	serviceRestartLimit       = 3
	// serviceHygieneAge is how long a service must have been up before the
	// retrospective may wonder aloud whether it is still wanted, and
	// serviceHygieneQuiet how long its session must have been silent. One
	// service per retrospective keeps the noticing to a single quiet line.
	serviceHygieneAge     = 72 * time.Hour
	serviceHygieneQuiet   = 24 * time.Hour
	serviceHygienePerPass = 1
)

// ServiceRuntime is the fakeable platform membrane for health, process
// identity, detached restart, and group stop.
type ServiceRuntime interface {
	IdentityMatches(pid int, startedAt time.Time) (bool, error)
	Healthy(context.Context, store.Service) error
	Start(store.Service) (pid int, startedAt time.Time, err error)
	Stop(pid int) error
}

type platformServiceRuntime struct{ reap map[int]bool }

func newPlatformServiceRuntime() *platformServiceRuntime {
	return &platformServiceRuntime{reap: make(map[int]bool)}
}

func (runtime *platformServiceRuntime) IdentityMatches(pid int, startedAt time.Time) (bool, error) {
	if runtime.reap[pid] {
		var status syscall.WaitStatus
		waited, waitErr := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		if waitErr == nil && waited == pid {
			delete(runtime.reap, pid)
			return false, nil
		}
	}
	matched, err := executor.ProcessIdentityMatches(pid, startedAt)
	if err == nil {
		return matched, nil
	}
	// Graceful degradation for Unix targets whose ps lacks lstart: preserve a
	// demonstrably live process, but do not silently start a replacement.
	liveErr := syscall.Kill(pid, 0)
	if liveErr == nil || errors.Is(liveErr, syscall.EPERM) {
		return true, nil
	}
	return false, nil
}

func (*platformServiceRuntime) Healthy(ctx context.Context, service store.Service) error {
	ctx, cancel := context.WithTimeout(ctx, serviceHealthTimeout)
	defer cancel()
	switch service.Health.Kind {
	case store.ServiceHealthPort:
		dialer := net.Dialer{Timeout: serviceHealthTimeout}
		connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", service.Health.Value))
		if err != nil {
			return err
		}
		return connection.Close()
	case store.ServiceHealthURL:
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, service.Health.Value, nil)
		if err != nil {
			return err
		}
		response, err := (&http.Client{Timeout: serviceHealthTimeout}).Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", response.StatusCode)
		}
		return nil
	case store.ServiceHealthCmd:
		command := osexec.CommandContext(ctx, "bash", "-lc", service.Health.Value)
		command.Dir = service.Dir
		return command.Run()
	default:
		return fmt.Errorf("unknown health kind %q", service.Health.Kind)
	}
}

func (runtime *platformServiceRuntime) Start(service store.Service) (int, time.Time, error) {
	if err := os.MkdirAll(filepath.Dir(service.LogPath), 0o755); err != nil {
		return 0, time.Time{}, err
	}
	pid, startedAt, err := executor.StartDetachedService(service.Command, service.Dir, service.LogPath)
	if err == nil {
		runtime.reap[pid] = true
	}
	return pid, startedAt, err
}

func (runtime *platformServiceRuntime) Stop(pid int) error {
	if err := executor.StopServiceProcess(pid); err != nil {
		return err
	}
	if runtime.reap[pid] {
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &status, 0, nil)
		delete(runtime.reap, pid)
	}
	return nil
}

// ServiceSupervisor owns journaled services. It creates no goroutines: Tick,
// Stop, and Restart finish their bounded process work synchronously.
type ServiceSupervisor struct {
	store   *store.Store
	runtime ServiceRuntime
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
}

func NewServiceSupervisor(graph *store.Store) *ServiceSupervisor {
	return &ServiceSupervisor{
		store: graph, runtime: newPlatformServiceRuntime(), now: time.Now,
		sleep: func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func (supervisor *ServiceSupervisor) WithRuntime(runtime ServiceRuntime) *ServiceSupervisor {
	if runtime != nil {
		supervisor.runtime = runtime
	}
	return supervisor
}

func (supervisor *ServiceSupervisor) Tick(ctx context.Context) error {
	if supervisor == nil || supervisor.store == nil {
		return nil
	}
	services, err := supervisor.store.ActiveServices()
	if err != nil {
		return err
	}
	for _, service := range services {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch service.Status {
		case store.ServiceRunning:
			if supervisor.now().Sub(service.StartedAt) < serviceHealthStartupGrace {
				continue
			}
			matched, identityErr := supervisor.runtime.IdentityMatches(service.PID, service.StartedAt)
			healthErr := identityErr
			if identityErr == nil && !matched {
				healthErr = errors.New("process identity no longer matches")
			}
			if healthErr == nil {
				healthErr = supervisor.runtime.Healthy(ctx, service)
			}
			if healthErr == nil {
				continue
			}
			if err := supervisor.store.FailService(service.ID, healthErr.Error(), service.RestartCount); err != nil {
				return err
			}
			service.Status = store.ServiceFailed
			_ = supervisor.attention(service, fmt.Sprintf("%s stopped answering on %s", service.Name, service.Health.Suffix()))
			if service.AutoRestart {
				if err := supervisor.restartFailed(ctx, service); err != nil {
					return err
				}
			}
		case store.ServiceFailed:
			if service.AutoRestart {
				if err := supervisor.restartFailed(ctx, service); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (supervisor *ServiceSupervisor) restartFailed(ctx context.Context, service store.Service) error {
	if service.RestartCount >= serviceRestartLimit {
		return supervisor.rest(service)
	}
	if err := supervisor.runtime.Stop(service.PID); err != nil {
		return err
	}
	backoff := serviceRestartBackoff * time.Duration(1<<service.RestartCount)
	if err := supervisor.sleep(ctx, backoff); err != nil {
		return err
	}
	next := service.RestartCount + 1
	pid, startedAt, err := supervisor.runtime.Start(service)
	if err != nil {
		if updateErr := supervisor.store.FailService(service.ID, err.Error(), next); updateErr != nil {
			return updateErr
		}
		service.RestartCount = next
		if next >= serviceRestartLimit {
			return supervisor.rest(service)
		}
		return nil
	}
	return supervisor.store.RestartService(service.ID, pid, startedAt, next)
}

func (supervisor *ServiceSupervisor) rest(service store.Service) error {
	if err := supervisor.store.RestService(service.ID, "restart limit reached", service.RestartCount); err != nil {
		return err
	}
	return supervisor.attention(service, fmt.Sprintf("%s rested after %d restarts — say 'restart it' when ready",
		service.Name, service.RestartCount))
}

func (supervisor *ServiceSupervisor) attention(service store.Service, line string) error {
	node, found, err := supervisor.store.Node(service.Provenance.LeafNodeID)
	if err != nil || !found || strings.TrimSpace(node.Provenance.SessionID) == "" {
		return err
	}
	_, err = supervisor.store.PostMessage(store.Message{
		SessionID: node.Provenance.SessionID, Role: store.RoleSystem, Body: line,
	})
	return err
}

func (supervisor *ServiceSupervisor) Stop(id, reason string) error {
	service, found, err := supervisor.store.Service(id)
	if err != nil {
		return err
	}
	if !found || service.Status == store.ServiceStopped {
		return fmt.Errorf("stop service: %w: %q", store.ErrNotFound, id)
	}
	if err := supervisor.runtime.Stop(service.PID); err != nil {
		return err
	}
	return supervisor.store.StopService(id, reason)
}

func (supervisor *ServiceSupervisor) Restart(id string) error {
	service, found, err := supervisor.store.Service(id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("restart service: %w: %q", store.ErrNotFound, id)
	}
	if service.Status != store.ServiceStopped {
		if err := supervisor.runtime.Stop(service.PID); err != nil {
			return err
		}
	}
	pid, startedAt, err := supervisor.runtime.Start(service)
	if err != nil {
		return err
	}
	return supervisor.store.RestartService(id, pid, startedAt, 0)
}

func (supervisor *ServiceSupervisor) SetAutoRestart(id string, enabled bool) error {
	return supervisor.store.SetServiceAutoRestart(id, enabled)
}

func (r *Reconciler) applyServiceCommand(command store.Command) (commandOutcome, error) {
	if r.services == nil {
		return commandOutcome{}, errors.New("service supervisor is unavailable")
	}
	service, found, err := r.store.Service(command.Target)
	if err != nil {
		return commandOutcome{}, err
	}
	if !found {
		return commandOutcome{}, fmt.Errorf("service %q is missing", command.Target)
	}
	verb, receipt := "", ""
	switch command.Kind {
	case store.CommandServiceStop:
		err = r.services.Stop(service.ID, command.Instruction)
		verb, receipt = "stopped", service.Name+" stopped."
	case store.CommandServiceRestart:
		err = r.services.Restart(service.ID)
		verb, receipt = "restarted", service.Name+" restarted."
	case store.CommandServiceAutoRestart:
		enabled := !strings.Contains(strings.ToLower(command.Instruction), "disable") &&
			!strings.Contains(strings.ToLower(command.Instruction), "off")
		err = r.services.SetAutoRestart(service.ID, enabled)
		verb = "auto-restart enabled"
		receipt = service.Name + " will restart up to 3 times if it stops answering."
		if !enabled {
			verb, receipt = "auto-restart disabled", service.Name+" auto-restart disabled."
		}
	}
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{status: store.CommandApplied, result: verb, receipt: receipt}, nil
}

// proposeServiceHygiene is the retrospective noticing that something has been
// up a long time with nobody near it. It asks once per service, defaults to
// keep, and never asks about the same service again — the durable question is
// the memory, exactly like a declined charter proposal.
func (r *Reconciler) proposeServiceHygiene(now time.Time) {
	services, err := r.store.ActiveServices()
	if err != nil || len(services) == 0 {
		return
	}
	asked := 0
	for _, service := range services {
		if asked >= serviceHygienePerPass {
			return
		}
		if service.Status != store.ServiceRunning || now.Sub(service.StartedAt) < serviceHygieneAge {
			continue
		}
		nudged, err := r.store.ServiceHygieneAsked(service.ID)
		if err != nil || nudged {
			continue
		}
		node, found, err := r.store.Node(service.Provenance.LeafNodeID)
		if err != nil || !found {
			continue
		}
		session := strings.TrimSpace(node.Provenance.SessionID)
		if session == "" {
			continue
		}
		quiet, err := r.store.SessionQuietSince(session, now.Add(-serviceHygieneQuiet))
		if err != nil || !quiet {
			continue
		}
		if err := r.askServiceHygiene(service, session, now); err == nil {
			asked++
		}
	}
}

func (r *Reconciler) askServiceHygiene(service store.Service, session string, now time.Time) error {
	allowFree := true
	options := []store.QuestionOption{
		{Label: "keep", Value: store.ServiceHygieneKeepValue(service.ID)},
		{Label: "stop it", Value: store.ServiceHygieneStopValue(service.ID)},
	}
	text := store.QuestionMessageBody(
		fmt.Sprintf("%s has run %s — still needed?", service.Name, serviceRunLength(now.Sub(service.StartedAt))),
		options, store.QuestionConfig{Kind: store.QuestionConfirm, Default: "1", AllowFree: &allowFree})
	question, err := r.store.AskQuestion(store.AgentQuestion{
		SessionID: session, Text: text, OriginNodeID: service.Provenance.LeafNodeID,
		Urgency: store.QuestionWhenever, Options: options,
	})
	if err != nil {
		return err
	}
	_, err = r.store.SurfaceQuestion(question.Seq)
	return err
}

func serviceRunLength(age time.Duration) string {
	days := int(age / (24 * time.Hour))
	if days >= 1 {
		return fmt.Sprintf("%d %s", days, plural(days, "day", "days"))
	}
	hours := int(age / time.Hour)
	if hours < 1 {
		hours = 1
	}
	return fmt.Sprintf("%d %s", hours, plural(hours, "hour", "hours"))
}

// ReAdoptServices is the startup hook beside orphan release. A stale identity
// is stopped honestly and never respawned.
func ReAdoptServices(graph *store.Store, sessionID string, runtime ServiceRuntime) error {
	if graph == nil {
		return nil
	}
	if runtime == nil {
		runtime = newPlatformServiceRuntime()
	}
	services, err := graph.ActiveServices()
	if err != nil {
		return err
	}
	for _, service := range services {
		if service.Status != store.ServiceRunning {
			continue
		}
		matched, matchErr := runtime.IdentityMatches(service.PID, service.StartedAt)
		if matchErr == nil && matched {
			if err := graph.AdoptService(service.ID); err != nil {
				return err
			}
			continue
		}
		if err := graph.StopService(service.ID, "process was not running at startup"); err != nil {
			return err
		}
		messageSession := strings.TrimSpace(sessionID)
		if node, found, _ := graph.Node(service.Provenance.LeafNodeID); found && node.Provenance.SessionID != "" {
			messageSession = node.Provenance.SessionID
		}
		if messageSession != "" {
			_, _ = graph.PostMessage(store.Message{SessionID: messageSession, Role: store.RoleSystem,
				Body: fmt.Sprintf("%s was not running anymore — say 'start it again' to relaunch", service.Name)})
		}
	}
	return nil
}
