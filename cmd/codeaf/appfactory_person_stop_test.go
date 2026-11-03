package main

import (
 "strings"
 "testing"

 "github.com/Agent-Field/codeaf/internal/appwire"
 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/api"
 "github.com/Agent-Field/codeaf/internal/factory/lease"
 factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// Every authority converges on the same launch door. A name or fresh process
// supplies no permission to undo the person's Stop.
func TestFactoryPersonStopLaunchAuthorities(t *testing.T) {
 for _, authority := range []string{"manager", "other_job_manager", "crew", "standing_item", "scheduler", "github_poll", "new_commits", "restart"} {
  t.Run(authority, func(t *testing.T) {
   s, status := newPauseSetup(t)
   host := s.backend.local.host
   it, err := host.floor.Create(factory.Item{Repo:"acme/api", Kind:factory.KindIssue, State:factory.StateNeedsYou, Starter:"owner", Question:"which command?", QKind:"step"})
   if err != nil { t.Fatal(err) }
   var before api.Job
   s.must("factory.job", map[string]any{"factory":status.Factory,"job":it.ID}, &before)
   s.must("factory.job_stop", map[string]any{"factory":status.Factory,"job":it.ID,"expect":map[string]any{"version":before.Version}}, &appwire.FactoryJobActed{})
   desk, err := lease.Open(host.floor, lease.Options{})
   if err != nil { t.Fatal(err) }
   var launch func(int) error
   if authority == "crew" || authority == "standing_item" {
    launch = factoryrun.New(factoryrun.Options{Store:host.floor}).Launch
   } else { launch = lease.NewDoors(desk).Launch }
   if err := launch(it.ID); err == nil || !strings.Contains(err.Error(), "person stopped") || !strings.Contains(err.Error(), "Start") { t.Fatalf("%s launched stopped job, or gave a dishonest reason: %v", authority, err) }
   var after api.Job
   s.must("factory.job", map[string]any{"factory":status.Factory,"job":it.ID}, &after)
   if after.State != api.StateNeedsYou { t.Fatalf("stopped job reads as %s", after.State) }
   if after.Can.Answer || after.Can.Approve || after.Can.PRReady || after.Can.Stop { t.Fatalf("stopped job offers effectful controls: %+v",after.Can) }
   if !after.Can.StartNow { t.Fatal("person cannot Start stopped job") }
   saved, err := host.floor.Get(it.ID)
   if err != nil { t.Fatal(err) }
   if err := factory.ReadinessGate(saved); err == nil || !strings.Contains(err.Error(), "person stopped") { t.Fatalf("readiness ignored Stop: %v",err) }
   if _, err := lease.NewDoors(desk).AnswerPending(it.ID, saved.Talk, "late-question", "yes"); err == nil || !strings.Contains(err.Error(), "person stopped") { t.Fatalf("manager answer ignored Stop: %v",err) }
   if factory.Automatic(saved) { t.Fatal("stopped job remains automatic") }
  })
 }
}
