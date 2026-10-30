package main

// A job the factory ran before its Room was persisted gets the Room back only
// when exactly one open crew holds every stage transcript and the job's Talk,
// led by that Talk. Every other shape recovers nothing and writes nothing.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/teams/teamstest"
)

type legacyRig struct {
	t       *testing.T
	profile string
	dir     string
	st      *store.Store
}

func newLegacyRig(t *testing.T) *legacyRig {
	t.Helper()
	root := resolvedTempDir(t)
	st, err := store.Open(filepath.Join(root, "factory"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "chats")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return &legacyRig{t: t, profile: filepath.Join(root, "profile"), dir: dir, st: st}
}

// chat makes a transcript file and answers its path.
func (r *legacyRig) chat(name string) string {
	r.t.Helper()
	p := filepath.Join(r.dir, name+".jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
		r.t.Fatal(err)
	}
	return p
}

func legacyMember(path string) teamstore.Member {
	return teamstore.Member{Key: path, File: path, JoinedAt: time.Unix(1, 0)}
}

// seed writes the teams, in order.
func (r *legacyRig) seed(ts ...teamstore.Team) {
	r.t.Helper()
	if err := teamstore.Update(r.profile, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, ts...)
		return nil
	}); err != nil {
		r.t.Fatal(err)
	}
}

// item stores a job with the given Talk, Room and stage chats.
func (r *legacyRig) item(talk, room string, chats ...string) int {
	r.t.Helper()
	var phases []factory.Phase
	for _, c := range chats {
		phases = append(phases, factory.Phase{Chat: c})
	}
	it, err := r.st.Create(factory.Item{Repo: "work", Kind: factory.KindIssue, Title: "old", State: factory.StateNew})
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.st.Update(it.ID, func(it *factory.Item) error {
		it.Talk = talk
		it.Stream = &factory.Stream{Room: room, Phases: phases}
		return nil
	}); err != nil {
		r.t.Fatal(err)
	}
	return it.ID
}

func (r *legacyRig) room(id int) string {
	r.t.Helper()
	it, err := r.st.Get(id)
	if err != nil {
		r.t.Fatal(err)
	}
	return it.Stream.Room
}

func (r *legacyRig) teams() map[string]teamstore.Team {
	r.t.Helper()
	f, err := teamstore.Load(r.profile)
	if err != nil {
		r.t.Fatal(err)
	}
	out := map[string]teamstore.Team{}
	for _, tm := range f.Teams {
		out[tm.ID] = tm
	}
	return out
}

func (r *legacyRig) teamsBytes() []byte {
	r.t.Helper()
	return teamstest.JSON(r.t, r.profile)
}

func (r *legacyRig) reconcile() { factoryStampStore(r.profile, r.st) }

// fiveStages is the native shape: the Talk plus five stage chats, all held by
// one child under an unstamped `factory` parent, the Talk leading it.
type fiveStages struct {
	talk   string
	chats  []string
	child  teamstore.Team
	parent teamstore.Team
}

func (r *legacyRig) five() fiveStages {
	r.t.Helper()
	fs := fiveStages{talk: r.chat("talk")}
	members := []teamstore.Member{legacyMember(fs.talk)}
	for _, n := range []string{"s1", "s2", "s3", "s4", "s5"} {
		c := r.chat(n)
		fs.chats = append(fs.chats, c)
		members = append(members, legacyMember(c))
	}
	fs.parent = teamstore.Team{ID: "parent", Name: factoryTeamName, Made: time.Unix(1, 0)}
	fs.child = teamstore.Team{ID: "child", Name: "#1 · old", Parent: "parent", Members: members, Manager: fs.talk, Made: time.Unix(2, 0)}
	return fs
}

func TestLegacyEmptyRoomIsRecoveredFromExactStageMembership(t *testing.T) {
	r := newLegacyRig(t)
	fs := r.five()
	r.seed(fs.parent, fs.child)
	id := r.item(fs.talk, "", fs.chats...)
	r.reconcile()
	if got := r.room(id); got != "child" {
		t.Fatalf("room = %q, want child", got)
	}
	ts := r.teams()
	if !ts["child"].FactoryManaged || !ts["parent"].FactoryManaged {
		t.Fatalf("child/parent unstamped: %+v", ts)
	}
	staged := 0
	for _, m := range ts["child"].Members {
		if m.File == fs.talk {
			if m.FactoryStage {
				t.Fatal("the Talk was stamped as a stage")
			}
			continue
		}
		if !m.FactoryStage || m.FactoryBrief != "" {
			t.Fatalf("stage %s = %+v", m.File, m)
		}
		staged++
	}
	if staged != 5 {
		t.Fatalf("%d stages stamped", staged)
	}
}

func TestLegacyRecoveryIsIdempotent(t *testing.T) {
	r := newLegacyRig(t)
	fs := r.five()
	r.seed(fs.parent, fs.child)
	id := r.item(fs.talk, "", fs.chats...)
	r.reconcile()
	first := r.teamsBytes()
	revision := func() string { raw, _ := os.ReadFile(r.st.ItemPath(id)); return string(raw) }
	before := revision()
	r.reconcile()
	if string(r.teamsBytes()) != string(first) || revision() != before {
		t.Fatal("a second reconcile changed the teams file or the item")
	}
}

func TestLegacyRecoveryLeavesASameNameOrdinaryCrewAlone(t *testing.T) {
	r := newLegacyRig(t)
	fs := r.five()
	ordinary := teamstore.Team{ID: "same", Name: fs.child.Name, Parent: "parent", Made: time.Unix(3, 0)}
	r.seed(fs.parent, fs.child, ordinary)
	id := r.item(fs.talk, "", fs.chats...)
	r.reconcile()
	if r.room(id) != "child" || r.teams()["same"].FactoryManaged {
		t.Fatalf("room = %q, same = %+v", r.room(id), r.teams()["same"])
	}
}

// refuses runs one shape that must recover nothing and write nothing.
func refuses(t *testing.T, build func(r *legacyRig, fs fiveStages) (talk string, chats []string, room string)) {
	t.Helper()
	r := newLegacyRig(t)
	fs := r.five()
	talk, chats, room := build(r, fs)
	id := r.item(talk, room, chats...)
	before := r.teamsBytes()
	r.reconcile()
	if got := r.room(id); got != room {
		t.Fatalf("room = %q, want %q", got, room)
	}
	if room == "" && string(r.teamsBytes()) != string(before) {
		t.Fatal("the teams file was written")
	}
}

func TestLegacyRecoveryRefusals(t *testing.T) {
	t.Run("a crew sharing only some stages is not the holder", func(t *testing.T) {
		r := newLegacyRig(t)
		fs := r.five()
		partial := teamstore.Team{ID: "part", Name: "mine", Members: []teamstore.Member{legacyMember(fs.chats[0]), legacyMember(fs.talk)}, Made: time.Unix(3, 0)}
		r.seed(fs.parent, fs.child, partial)
		id := r.item(fs.talk, "", fs.chats...)
		r.reconcile()
		if r.room(id) != "child" || r.teams()["part"].FactoryManaged {
			t.Fatalf("room = %q part = %+v", r.room(id), r.teams()["part"])
		}
	})
	t.Run("a closed copy holding everything is ambiguity", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			copy := teamstore.Team{ID: "copy", Name: "copy", State: teamstore.TeamClosed, ClosedAt: time.Unix(5, 0), Members: append([]teamstore.Member(nil), fs.child.Members...), Made: time.Unix(3, 0)}
			r.seed(fs.parent, fs.child, copy)
			return fs.talk, fs.chats, ""
		})
	})
	t.Run("an open copy holding everything is ambiguity", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			copy := teamstore.Team{ID: "copy", Name: "copy", Members: append([]teamstore.Member(nil), fs.child.Members...), Made: time.Unix(3, 0)}
			r.seed(fs.parent, fs.child, copy)
			return fs.talk, fs.chats, ""
		})
	})
	t.Run("stages moved to another crew leave no full holder", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			moved := teamstore.Team{ID: "moved", Name: "mine", Members: fs.child.Members[1:], Made: time.Unix(3, 0)}
			fs.child.Members = fs.child.Members[:1]
			r.seed(fs.parent, fs.child, moved)
			return fs.talk, fs.chats, ""
		})
	})
	t.Run("the Talk does not lead", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			fs.child.Manager = fs.chats[0]
			r.seed(fs.parent, fs.child)
			return fs.talk, fs.chats, ""
		})
	})
	t.Run("no manager", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			fs.child.Manager = ""
			r.seed(fs.parent, fs.child)
			return fs.talk, fs.chats, ""
		})
	})
	t.Run("a talk alone proves nothing", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			r.seed(fs.parent, fs.child)
			return fs.talk, nil, ""
		})
	})
	t.Run("a stage chat without a talk proves nothing", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			r.seed(fs.parent, fs.child)
			return "", fs.chats, ""
		})
	})
	t.Run("an existing room is never overwritten", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			other := teamstore.Team{ID: "other", Name: "elsewhere", Made: time.Unix(3, 0)}
			r.seed(fs.parent, fs.child, other)
			return fs.talk, fs.chats, "other"
		})
	})
	t.Run("a started stage is a crew member", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			fs.child.Members[2].Started = true
			r.seed(fs.parent, fs.child)
			return fs.talk, fs.chats, ""
		})
	})
	t.Run("a stage with a start entry is a crew member", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			fs.child.Members[3].StartEntry = "line-1"
			r.seed(fs.parent, fs.child)
			return fs.talk, fs.chats, ""
		})
	})
	t.Run("a closed holder is not a room", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			fs.child.State = teamstore.TeamClosed
			fs.child.ClosedAt = time.Unix(5, 0)
			r.seed(fs.parent, fs.child)
			return fs.talk, fs.chats, ""
		})
	})
	t.Run("a former member is not a holder", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			fs.child.FormerMembers = []teamstore.Member{fs.child.Members[5]}
			fs.child.Members = fs.child.Members[:5]
			r.seed(fs.parent, fs.child)
			return fs.talk, fs.chats, ""
		})
	})
	t.Run("a path that differs only in the file string does not match", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			fs.child.Members[1].File = fs.chats[0] + ".other"
			r.seed(fs.parent, fs.child)
			return fs.talk, fs.chats, ""
		})
	})
}

func TestLegacyRecoveryRefusesTheRootAndAFactoryParent(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			fs.child.Root = true
			fs.child.Parent = ""
			r.seed(fs.child)
			return fs.talk, fs.chats, ""
		})
	})
	t.Run("a stamped factory parent holding the set", func(t *testing.T) {
		refuses(t, func(r *legacyRig, fs fiveStages) (string, []string, string) {
			fs.child.Parent = ""
			fs.child.Name = factoryTeamName
			fs.child.FactoryManaged = true
			r.seed(fs.child)
			return fs.talk, fs.chats, ""
		})
	})
}

func TestLegacyRecoveryAbstainsWhenTwoItemsResolveToOneTeam(t *testing.T) {
	r := newLegacyRig(t)
	fs := r.five()
	r.seed(fs.parent, fs.child)
	a := r.item(fs.talk, "", fs.chats...)
	b := r.item(fs.talk, "", fs.chats...)
	r.reconcile()
	if r.room(a) != "" || r.room(b) != "" || r.teams()["child"].FactoryManaged {
		t.Fatalf("duplicates recovered: %q %q %+v", r.room(a), r.room(b), r.teams()["child"])
	}
}

func TestLegacyRecoveryAbstainsWhenAnotherItemRecordsTheTeam(t *testing.T) {
	r := newLegacyRig(t)
	fs := r.five()
	r.seed(fs.parent, fs.child)
	legacy := r.item(fs.talk, "", fs.chats...)
	r.item(r.chat("x"), "child")
	r.reconcile()
	if r.room(legacy) != "" {
		t.Fatalf("recovered a room another item records: %q", r.room(legacy))
	}
}

func TestLegacyRecoveryFollowsASymlinkedHome(t *testing.T) {
	r := newLegacyRig(t)
	fs := r.five()
	link := filepath.Join(filepath.Dir(r.dir), "link")
	if err := os.Symlink(r.dir, link); err != nil {
		t.Fatal(err)
	}
	// The record names the chats through the link; the teams file holds the
	// exact link spelling as File and the resolved path as Key, as joining does.
	var linked []string
	for i, c := range fs.chats {
		p := filepath.Join(link, filepath.Base(c))
		linked = append(linked, p)
		fs.child.Members[i+1] = teamstore.Member{Key: c, File: p, JoinedAt: time.Unix(1, 0)}
	}
	r.seed(fs.parent, fs.child)
	id := r.item(fs.talk, "", linked...)
	r.reconcile()
	if r.room(id) != "child" {
		t.Fatalf("room = %q through a symlinked home", r.room(id))
	}
}

func TestLegacyRecoveryChangedItemKeepsItsRoomEmpty(t *testing.T) {
	r := newLegacyRig(t)
	fs := r.five()
	r.seed(fs.parent, fs.child)
	id := r.item(fs.talk, "", fs.chats...)
	snap, _ := teamstore.Snapshot(r.profile)
	items, _ := r.st.List()
	claims := legacyRoomClaims(snap, items)
	if len(claims) != 1 {
		t.Fatalf("claims = %+v", claims)
	}
	// The item moves its Talk between the proof and the write: the CAS refuses.
	if err := r.st.Annotate(id, func(it *factory.Item) error { it.Talk = r.chat("new-talk"); return nil }); err != nil {
		t.Fatal(err)
	}
	r.reconcile()
	if r.room(id) != "" {
		t.Fatalf("room = %q after the evidence changed", r.room(id))
	}
}

func TestLegacyRecoveryNeverOverwritesARoomSetBeforeTheWrite(t *testing.T) {
	r := newLegacyRig(t)
	fs := r.five()
	r.seed(fs.parent, fs.child)
	id := r.item(fs.talk, "", fs.chats...)
	if err := r.st.Annotate(id, func(it *factory.Item) error { it.Stream.Room = "chosen"; return nil }); err != nil {
		t.Fatal(err)
	}
	r.reconcile()
	if r.room(id) != "chosen" {
		t.Fatalf("room = %q", r.room(id))
	}
}
