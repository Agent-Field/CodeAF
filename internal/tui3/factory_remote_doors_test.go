package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestFactoryRemoteJobKeysFollowTheLocalWorker(t *testing.T) {
	remote, local, worker := &factoryFake{}, &factoryFake{}, &factoryFake{}
	a := factoryVerbLab(t, remote)
	workers := map[int]factory.Seam{8: worker.seam()}
	wired := remote.seam()
	wired.Hold = func(id int, on bool) error { return remote.rec("Hold", id, on) }
	a.factory = api.ComposeJobs(wired, local.seam(), func() bool { return false }, func(id int) factory.Seam { return workers[id] })
	factoryOn(t, a, 8)
	drive(t, a, key("c"))
	if got := strings.Join(worker.said(), " "); got != "SetCap(8,8)" {
		t.Fatalf("held job budget key = %q", got)
	}
	if got := local.said(); len(got) != 0 {
		t.Fatalf("remote job acted on the local floor: %v", got)
	}
	delete(workers, 8)
	for _, k := range []string{"c", "b", "e", "s", "T", "u", "p", "g"} {
		drive(t, a, key(k))
	}
	if got := worker.said(); len(got) != 0 {
		t.Fatalf("unheld job kept local worker keys: %v", got)
	}
	if got := local.said(); len(got) != 0 {
		t.Fatalf("unheld job called local floor doors: %v", got)
	}
	it, _ := a.factoryCursorItem()
	if a.factoryShapeable(it) || a.factoryIssueEnter(it) != factoryIssueNone {
		t.Fatal("an unheld job still offers a local manager or shaping door")
	}
	factoryOn(t, a, 2)
	if a.factory.Job(2).Pause != nil {
		t.Fatal("the unheld job has a local pause fallback")
	}
	drive(t, a, key(" "))
	if got := strings.Join(remote.said(), " "); got != "Hold(2,true)" {
		t.Fatalf("remote pause key = %q; want the wired Hold door", got)
	}
}
