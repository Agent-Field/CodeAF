package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestCopyBucket_D3_TenNativeMovesKeepExistingRoots(t *testing.T) {
	celltest.RequireEngine(t)
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	h := newHostedMoveRig(t)
	c, err := cell.CreateIn(filepath.Join(h.a.Home, "v3", "projects", "original"), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(c.Root, "work")
	if err = os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err = session.SaveMeta(c.Root, session.Meta{ID: c.ID, Workspace: work, Owned: true, Title: "ten moves"}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(session.Place{Dir: c.Root}.Transcript(), []byte(fmt.Sprintf("{\"type\":\"session\",\"version\":1,\"id\":%q}\n{\"type\":\"message\",\"role\":\"user\",\"content\":\"ten moves\"}\n", c.ID)), 0600); err != nil {
		t.Fatal(err)
	}
	homes := []*syncsetup.Sync{h.a, h.b}
	engines := []cellstore.Engine{cellstore.EngineFor(""), cellstore.EngineFor("")}
	for i, s := range homes {
		engines[i].DataRoot = filepath.Join(s.Home, "v3", "stores")
		if err = syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, fmt.Sprintf("home %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	roots := map[string]string{h.a.Home: c.Root}
	for hop := 0; hop < 10; hop++ {
		from, to := hop%2, (hop+1)%2
		src, dst := homes[from], homes[to]
		t.Setenv(home.EnvVar, src.Home)
		work = filepath.Join(c.Root, "work")
		marker := fmt.Sprintf("move %d", hop)
		if err = os.WriteFile(filepath.Join(work, "latest.txt"), []byte(marker), 0600); err != nil {
			t.Fatal(err)
		}
		journal, err := os.OpenFile(session.Place{Dir: c.Root}.Transcript(), os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := fmt.Fprintf(journal, "{\"type\":\"message\",\"role\":\"assistant\",\"content\":%q}\n", marker)
		closeErr := journal.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("journal: %v, %v", writeErr, closeErr)
		}
		machine, err := preflight.OpenMachine(c.Root, work)
		if err != nil {
			t.Fatal(err)
		}
		machine.Observer().Refresh()
		engine := engines[from]
		engine.Workspace = work
		drive, err := src.Drive(t.Context(), engine, c, syncsetup.DriveOptions{DeviceName: fmt.Sprintf("home %d", from)})
		if err != nil {
			t.Fatal(err)
		}
		sealed, sealErr := drive.Store(engine).Seal(t.Context(), c, cellstore.TurnInfo{})
		closeErr = drive.Close(context.Background())
		if sealErr != nil || closeErr != nil {
			t.Fatalf("publish: %v, %v", sealErr, closeErr)
		}
		view, err := src.Dir.Cell(t.Context(), c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if view.Cell.Head != sealed.Turn.ID || view.Cell.Lease.Expires != 0 {
			t.Fatalf("unpublished or held move: %+v", view)
		}
		t.Setenv(home.EnvVar, dst.Home)
		got, err := chatContinuer(dst, nil, nil).TakeSeen(t.Context(), c.ID, nil)
		if err != nil {
			t.Fatalf("D3 hop %d: %v", hop, err)
		}
		c = got.Taken.Cell
		if hop == 0 && !strings.HasPrefix(filepath.Base(filepath.Dir(c.Root)), "copy-") {
			t.Fatalf("D3 first arrival was not filed into its origin bucket: %s", c.Root)
		}
		if prior := roots[dst.Home]; prior != "" && c.Root != prior {
			t.Fatalf("D3 hop %d moved an existing local root: %s -> %s", hop, prior, c.Root)
		}
		roots[dst.Home] = c.Root
		for _, part := range strings.Split(filepath.ToSlash(c.Root), "/") {
			if len(part) > 255 {
				t.Fatalf("D3 hop %d component has %d bytes", hop, len(part))
			}
		}
		raw, err := os.ReadFile(filepath.Join(c.Root, "work", "latest.txt"))
		if err != nil || string(raw) != marker {
			t.Fatalf("D3 restored file=%q, %v", raw, err)
		}
		t.Logf("D3 hop %d root=%s", hop, c.Root)
	}
}
