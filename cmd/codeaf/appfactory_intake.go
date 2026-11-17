package main

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

var _ appwire.FactoryIntaker = (*appBackend)(nil)

// FactoryIntakeAdd files outside work on the factory this engine serves,
// through [inbox.Add] on its own floor, in this process.
//
// THE FACTORY TAKES NO WORK OVER HTTP. There is no POST /api/intake any more:
// a WorkAF user files work here, through a built-in connector set up with
// clicks, or through AgentField, whose node runs in this same host. So the
// desk's door is the factory's own machine only, and a factory elsewhere is
// refused rather than reached over the network.
//
// It reads the repository's recipe the way the Jira and AgentField doors do
// ([appFactoryHost.jiraRecipe]), and refuses work for a repository that is
// off in the words every other door uses, before anything is filed.
func (b *appBackend) FactoryIntakeAdd(ctx context.Context, p appwire.FactoryIntakeParams) (inbox.Result, error) {
	host, err := b.localHostOf(p.Factory)
	if err != nil {
		var wire *appwire.Error
		if errors.As(err, &wire) && wire.Code == appwire.CodeNotFound {
			return inbox.Result{}, appwire.Errorf(appwire.CodeNotFound, "work is filed on the machine that runs the factory; on another machine, connect GitHub or Jira there or send it through AgentField")
		}
		return inbox.Result{}, err
	}
	if p.Repo != "" {
		sw, err := host.floor.RepoSwitch()
		if err != nil {
			return inbox.Result{}, err
		}
		if sw.Off(p.Repo) {
			return inbox.Result{}, appwire.Errorf(appwire.CodeConflict, "%s", factory.RepoOffWords(p.Repo))
		}
	}
	// The wire checked the entry with the inbox's own check already
	// (appwire's prepIntake), so what is left to fail here is the floor.
	return inbox.Add(ctx, host.floor, p.Entry, time.Now(), host.jiraRecipe())
}
