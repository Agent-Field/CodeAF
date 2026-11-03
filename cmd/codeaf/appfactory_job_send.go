package main

import (
 "context"

 "github.com/Agent-Field/codeaf/internal/appwire"
 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/session"
)

// FactoryJobSend creates the manager on the write path, then uses ordinary sending.
func (b *appBackend) FactoryJobSend(ctx context.Context, p appwire.FactoryJobSendParams) (appwire.FactoryJobSent, error) {
 if err := (session.Submission{ClientID: p.ClientID}).Check(); err != nil { return appwire.FactoryJobSent{}, appEngineError(err) }
 host, err := b.localHostOf(p.Factory)
 if err != nil { return appwire.FactoryJobSent{}, err }
 it, err := host.floor.Get(p.Job)
 if err != nil || !factory.IsIssueJob(it) { return appwire.FactoryJobSent{}, appwire.Errorf(appwire.CodeNotFound, "issue job not found") }
 if err := factory.RefuseDropped(it); err != nil { return appwire.FactoryJobSent{}, appwire.Errorf(appwire.CodeNotFound, "%s", err) }
 if host.seam.Talk == nil { return appwire.FactoryJobSent{}, appwire.Errorf(appwire.CodeEngine, "this factory cannot make a job's conversation here") }
 talk, err := host.seam.Talk(ctx, p.Job)
 if err != nil { return appwire.FactoryJobSent{}, appEngineError(err) }
 id := appAgentID(talk)
 if err := b.holdJobManager(host, p.Job, id); err != nil { return appwire.FactoryJobSent{}, err }
 turn, err := b.Send(id, p.Text, p.ClientID)
 return appwire.FactoryJobSent{Agent: id, Turn: turn}, err
}
