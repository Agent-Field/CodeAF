package crewagent

import (
	"context"
	"sync"

	appagent "github.com/Agent-Field/codeaf/internal/apps/agent"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type turnReservationKey struct{}
type turnReservation struct {
	service  *Service
	request  svc.CrewAsk
	release  func()
	finished <-chan struct{}
}

// Prepare connects decorators after the composition root has filled every peer, before serving calls.
func Prepare(all *svc.Services) {
	if all == nil || all.Hub == nil {
		return
	}
	service, ok := all.Crews.Agent.(*Service)
	if !ok {
		return
	}
	service.prepare.Do(func() {
		all.Hub = &threadHub{Hub: all.Hub, aliases: map[string]threadAlias{}}
		if all.Agent != nil {
			all.Agent = &serialAgent{Agent: all.Agent, service: service}
		}
		if all.Approvals != nil {
			all.Approvals = &crewApprovals{Approvals: all.Approvals, service: service}
		}
		if all.Crews.Settings != nil {
			all.Crews.Settings = &crewBudget{Settings: all.Crews.Settings, service: service}
		}
	})
}

type serialAgent struct {
	svc.Agent
	service *Service
}

func (a *serialAgent) Send(ctx context.Context, scope svc.Scope, p bridge.AgentSendParams) (bridge.AgentSendResult, error) {
	reservation, _ := ctx.Value(turnReservationKey{}).(*turnReservation)
	var release func()
	var err error
	if reservation != nil && reservation.service == a.service {
		release = reservation.release
	} else {
		release, err = a.service.acquire(ctx, scope.App)
	}
	if err != nil {
		return bridge.AgentSendResult{}, err
	}
	var once sync.Once
	finish := func() { once.Do(func() { a.service.clearActive(scope.App); release() }) }
	if reservation != nil {
		a.service.setActive(scope.App, reservation.request)
	}
	events, stop := a.service.all.Hub.Subscribe("app/" + scope.App + "/*")
	publicThread := p.Thread
	storedThread := storageThread(publicThread)
	if storedThread != publicThread {
		request := p.Text
		if reservation != nil {
			request = reservation.request.Text
		}
		a.service.all.Hub.(*threadHub).alias(scope.App, storedThread, publicThread, request)
		p.Thread = storedThread
	}
	sender := a.Agent
	var stopTurn func()
	if _, realAgent := sender.(*appagent.Service); realAgent && reservation != nil {
		turnCtx, cancel := context.WithCancel(ctx)
		stopLifetime := context.AfterFunc(a.service.ctx, cancel)
		stopTurn = func() { stopLifetime(); cancel() }
		sender = appagent.New(svc.Config{Root: a.service.root, Ctx: turnCtx}, a.service.all)
	}
	sent, err := sender.Send(ctx, scope, p)
	if sent.Thread == storedThread && storedThread != publicThread {
		sent.Thread = publicThread
	}
	if err != nil {
		if stopTurn != nil {
			stopTurn()
		}
		stop()
		finish()
		return sent, err
	}
	go func() {
		if stopTurn != nil {
			defer stopTurn()
		}
		defer stop()
		defer func() {
			if reservation != nil {
				<-reservation.finished
			}
			finish()
		}()
		for {
			select {
			case <-a.service.ctx.Done():
				return
			case event, open := <-events:
				if !open {
					return
				}
				if event.Topic != bridge.TopicAgent(scope.App, sent.Thread) {
					continue
				}
				var e bridge.AgentEvent
				if decodeEvent(event.Data, &e) == nil && e.Turn == sent.Turn && (e.Kind == bridge.AgentDone || e.Kind == bridge.AgentError) {
					return
				}
			}
		}
	}()
	return sent, nil
}

func (s *Service) setActive(app string, req svc.CrewAsk) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active[app] = req
}
func (s *Service) clearActive(app string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.active, app) }

type crewApprovals struct {
	svc.Approvals
	service *Service
}

func (a *crewApprovals) Open(ctx context.Context, scope svc.Scope, item bridge.Approval) (bridge.Approval, error) {
	a.service.mu.Lock()
	req, ok := a.service.active[scope.App]
	a.service.mu.Unlock()
	if ok && scope.Caller.Agent && scope.Caller.Member == req.Asker.Member {
		payload := map[string]any{}
		for key, value := range item.Payload {
			payload[key] = value
		}
		payload["crew"], payload["crew_name"] = req.Crew, req.CrewName
		item.Who, item.Payload = req.Asker.Member, payload
	}
	return a.Approvals.Open(ctx, scope, item)
}

type crewBudget struct {
	svc.Settings
	service *Service
}

func (b *crewBudget) GetBudget(ctx context.Context, scope svc.Scope) (bridge.BudgetInfo, error) {
	info, err := b.Settings.GetBudget(ctx, scope)
	if err != nil {
		return info, err
	}
	return b.include(scope.App, info)
}
func (b *crewBudget) SetBudget(ctx context.Context, scope svc.Scope, p bridge.SetBudgetParams) (bridge.BudgetInfo, error) {
	info, err := b.Settings.SetBudget(ctx, scope, p)
	if err != nil {
		return info, err
	}
	return b.include(scope.App, info)
}
func (b *crewBudget) include(app string, info bridge.BudgetInfo) (bridge.BudgetInfo, error) {
	spent, err := b.service.Spent(app, b.service.now())
	if err != nil {
		return info, err
	}
	info.Spent += spent
	info.Paused = info.Month > 0 && info.Spent >= info.Month
	return info, nil
}
