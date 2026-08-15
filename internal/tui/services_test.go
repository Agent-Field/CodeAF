package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/ansi"
)

type serviceActionBackend struct {
	*fakeBackend
	services  []store.Service
	requested []store.Command
}

func (backend *serviceActionBackend) ActiveServices() ([]store.Service, error) {
	return append([]store.Service(nil), backend.services...), nil
}

func (backend *serviceActionBackend) RequestCommand(command store.Command) (store.Command, error) {
	backend.requested = append(backend.requested, command)
	command.Seq = int64(len(backend.requested))
	return command, nil
}

func TestServicesSectionCardLogTailAndOptionRouting(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	logPath := t.TempDir() + "/service.log"
	var log strings.Builder
	for line := 1; line <= 12; line++ {
		fmt.Fprintf(&log, "\x1b[31mline %d\x1b[0m\n", line)
	}
	if err := os.WriteFile(logPath, []byte(log.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	service := store.Service{
		ID: "svc", Name: "dev-server", Command: "npm run dev", LogPath: logPath,
		Health: store.ServiceHealth{Kind: store.ServiceHealthPort, Value: "5173"},
		Status: store.ServiceRunning, StartedAt: now.Add(-2 * time.Hour),
	}
	backend := &serviceActionBackend{fakeBackend: &fakeBackend{snapshot: store.Snapshot{Nodes: []store.Node{{ID: store.RootID}}}},
		services: []store.Service{service}}
	model := New(backend, "services")
	model.standingNow = func() time.Time { return now }
	model.setSize(90, 30)
	model.toggleGraph()
	section := ansi.Strip(model.renderServicesSection(80))
	if !strings.Contains(section, "services\n▸ dev-server · up 2h · :5173") {
		t.Fatalf("services section = %q", section)
	}
	if model.servicesSectionHeight() != 3 || model.selectedNodeID != serviceGraphRowID(service.ID) {
		t.Fatalf("service rail height=%d selected=%q", model.servicesSectionHeight(), model.selectedNodeID)
	}
	model.openServiceCard(service.ID)
	card := ansi.Strip(model.renderServiceCardBody(80))
	if strings.Contains(card, "line 1\n") || !strings.Contains(card, "line 3") || !strings.Contains(card, "line 12") || strings.Contains(card, "\x1b") {
		t.Fatalf("service card tail = %q", card)
	}
	command := model.requestServiceAction("stop")
	if command == nil {
		t.Fatal("stop action returned no command")
	}
	_ = command()
	if len(backend.requested) != 1 || backend.requested[0].Kind != store.CommandServiceStop || backend.requested[0].Target != service.ID {
		t.Fatalf("requested service action = %+v", backend.requested)
	}
}

// railReadBackend counts the two store reads the rail's sections make while a
// frame is composed.
type railReadBackend struct {
	*countingBackend
	services     []store.Service
	charters     []store.Charter
	serviceReads int
	charterReads int
}

func (b *railReadBackend) ActiveServices() ([]store.Service, error) {
	b.serviceReads++
	return append([]store.Service(nil), b.services...), nil
}

func (b *railReadBackend) Charters(...store.CharterStatus) ([]store.Charter, error) {
	b.charterReads++
	return append([]store.Charter(nil), b.charters...), nil
}

// The rail's height, its rows, and its card title all ask for services and
// charters while one frame is composed. The store answers once per poll — the
// same watermark that gates every other projection — never once per accessor.
func TestRailSectionsReadTheStoreOncePerPollNotPerFrame(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	inner := &fakeBackend{snapshot: store.Snapshot{Nodes: []store.Node{{ID: store.RootID}}}}
	backend := &railReadBackend{
		countingBackend: newCountingBackend(inner),
		services: []store.Service{{
			ID: "svc", Name: "dev-server", Status: store.ServiceRunning, StartedAt: now.Add(-time.Hour),
		}},
	}
	model := New(backend, "rail")
	// The rail composes its charters from the store table here, so the read
	// this counts is the one the rail itself makes.
	model.useStoreCharters()
	model.standingNow = func() time.Time { return now }
	model.setSize(90, 30)
	model.applyPoll(model.poll()().(pollResultMsg))
	model.toggleGraph()

	services, charters := backend.serviceReads, backend.charterReads
	for range 3 {
		_ = model.View()
	}
	if backend.serviceReads != services || backend.charterReads != charters {
		t.Fatalf("three frames read services %d times and charters %d times",
			backend.serviceReads-services, backend.charterReads-charters)
	}

	backend.bump()
	model.applyPoll(model.poll()().(pollResultMsg))
	_ = model.View()
	if backend.serviceReads <= services || backend.charterReads <= charters {
		t.Fatal("a moved journal did not re-open the rail reads")
	}
}

func TestServicesZeroStateRendersZeroBytes(t *testing.T) {
	backend := &serviceActionBackend{fakeBackend: &fakeBackend{}}
	model := New(backend, "services")
	model.graphOpen = true
	if section := model.renderServicesSection(80); section != "" {
		t.Fatalf("zero services rendered %q", section)
	}
	if height := model.servicesSectionHeight(); height != 0 {
		t.Fatalf("zero services reserved %d rows", height)
	}
}
