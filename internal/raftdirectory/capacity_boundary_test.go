package raftdirectory_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// r1.4
func TestR14CapacityRefusalDoesNotStopMembers(t *testing.T) {
	for _, spare := range []uint64{0, 1} {
		t.Run(fmt.Sprint(spare), func(t *testing.T) {
			g := newGroup(t, func() time.Time { return time.UnixMilli(1000) })
			_, size, err := g.stores[0].ApplicationView(ctx)
			must(t, err)
			g.ceiling = size + spare
			c, err := cs.Sign(cs.Command{Schema: cs.SchemaVersion, Cluster: cluster, Actor: "a", Session: "new-full-session", Sequence: 1, Now: 1000, Change: cs.Mutation{SetVault: &cs.VaultCAS{New: strings.Repeat("v", 4000)}}}, key("a"))
			must(t, err)
			first, err := g.submit(c)
			must(t, err)
			if first.Code != cs.Capacity {
				t.Fatal("growing command was not refused", first)
			}
			retry, err := g.submit(c)
			must(t, err)
			if !reflect.DeepEqual(first, retry) {
				t.Fatal("retry changed committed result")
			}
			for _, store := range g.stores {
				app, _, err := store.ApplicationView(ctx)
				must(t, err)
				k := cs.SessionKey(c.Actor, c.Session)
				rows, _, err := store.Read(ctx, app.Applied, []raftstore.Key{{Namespace: raftstore.Sessions, Key: k.Key}})
				must(t, err)
				if len(rows[raftstore.Key{Namespace: raftstore.Sessions, Key: k.Key}]) == 0 {
					t.Fatal("member lost refusal")
				}
			}
			c.Sequence = 2
			c.Ack = 1
			c.Change = cs.Mutation{AdvanceTime: &cs.AdvanceTime{}}
			c, err = cs.Sign(c, key("a"))
			must(t, err)
			next, err := g.submit(c)
			must(t, err)
			if next.Code != cs.OK {
				t.Fatal("next entry did not apply", next)
			}
			var applied uint64
			for i, store := range g.stores {
				app, _, err := store.ApplicationView(ctx)
				must(t, err)
				if i == 0 {
					applied = app.Applied
				}
				if app.Applied != applied {
					t.Fatal("members stopped at different indices")
				}
			}
		})
	}
}
