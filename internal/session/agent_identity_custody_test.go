package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/agentdid"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
)

func identityTruthFixture(t *testing.T, id, document string) (Place, string, []byte) {
	t.Helper()
	place := identityPlace(t, id)
	state := filepath.Join(place.Dir, cellStateDir)
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, placeMetaTruth)
	raw := []byte(`{"V":1,"agent_identity":` + document + `}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return place, path, raw
}

func TestAgentIdentityCustodySurvivesRestartAndMovesWithCell(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(home.EnvVar, profile)
	place := identityPlace(t, "custody-chat")
	if err := os.MkdirAll(filepath.Join(place.Dir, cellStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	agent := openIdentityAgent(t, place, nil)
	did := agent.AgentDID()
	agent.stampMeta()
	meta, err := LoadMeta(place.Dir)
	if err != nil || meta.AgentIdentity == nil {
		t.Fatalf("load identity: %v", err)
	}
	doc := *meta.AgentIdentity
	if doc.V != agentdid.Version || doc.Signing == nil || doc.Encryption == nil || len(doc.Custody) == 0 {
		t.Fatalf("missing versioned keys or encrypted custody: %+v", doc)
	}
	message := []byte("a receipt from this conversation")
	sig, err := agent.SignStatement("receipt/v1", message)
	if err != nil || !agentdid.Verify(did, "receipt/v1", message, sig) || agentdid.Verify(did, "other/v1", message, sig) {
		t.Fatalf("sign/verify: %v", err)
	}
	box, err := agentdid.Seal(doc, "mail/v1", message)
	if err != nil {
		t.Fatal(err)
	}
	_ = agent.Close()

	// A relocated chat on a paired computer has the same profile cell key.
	root, err := identity.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	pairedHome := t.TempDir()
	if err := identity.Save(pairedHome, root); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, pairedHome)
	moved := Place{Dir: filepath.Join(t.TempDir(), place.ID())}
	if err := os.Rename(place.Dir, moved.Dir); err != nil {
		t.Fatal(err)
	}
	again := openIdentityAgent(t, moved, nil)
	if again.AgentDID() != did {
		t.Fatal("moving or restarting replaced the DID")
	}
	plain, err := again.OpenSealed("mail/v1", box)
	if err != nil || !bytes.Equal(plain, message) {
		t.Fatalf("moved cell could not open its message: %v", err)
	}
	sig, err = again.SignStatement("receipt/v1", message)
	if err != nil || !agentdid.Verify(did, "receipt/v1", message, sig) {
		t.Fatalf("moved cell lost its signing key: %v", err)
	}
	other := openIdentityAgent(t, identityPlace(t, "other-chat"), nil)
	if _, err := other.OpenSealed("mail/v1", box); err == nil {
		t.Fatal("another conversation opened the sealed message")
	}
	// A copy with a new chat id cannot inherit signing or decryption custody.
	meta.ID = "branched-chat"
	branch := identityPlace(t, meta.ID)
	if err := SaveMeta(branch.Dir, meta); err != nil {
		t.Fatal(err)
	}
	branched := openIdentityAgent(t, branch, nil)
	if branched.AgentDID() == did {
		t.Fatal("branch kept its parent's DID")
	}
	if _, err := branched.OpenSealed("mail/v1", box); err == nil {
		t.Fatal("branch opened its parent's message")
	}
	branched.stampMeta()
	// Copying the files before rewriting meta.json also mints a fresh DID.
	copyPlace := Place{Dir: filepath.Join(t.TempDir(), "copied-chat")}
	if err := os.CopyFS(copyPlace.Dir, os.DirFS(moved.Dir)); err != nil {
		t.Fatal(err)
	}
	copied := openIdentityAgent(t, copyPlace, nil)
	if copied.AgentDID() == did || !agentdid.Valid(copied.AgentDID()) {
		t.Fatal("a folder copy did not mint its own identity")
	}
	// Losing access to the wrapping key must fail closed, never mint a replacement.
	t.Setenv(home.EnvVar, t.TempDir())
	if _, err := identity.EnsureSolo(home.Dir()); err != nil {
		t.Fatal(err)
	}
	if _, err := again.SignStatement("receipt/v1", message); err == nil {
		t.Fatal("unrelated profile unlocked custody")
	}
	if again.AgentDID() != did {
		t.Fatal("failed unlock replaced the DID")
	}
}

func TestAgentIdentityLegacyUpgradeOnlyOnWrite(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(home.EnvVar, profile)
	did, _ := agentdid.New()
	document, _ := json.Marshal(AgentIdentity{DID: did, Session: "legacy-chat"})
	place, path, before := identityTruthFixture(t, "legacy-chat", string(document))
	agent := openIdentityAgent(t, place, nil)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("opening rewrote a public-only identity")
	}
	if _, err := identity.Load(profile); !errors.Is(err, identity.ErrNone) {
		t.Fatalf("opening created profile custody: %v", err)
	}
	agent.stampMeta()
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	doc := meta.AgentIdentity
	if meta.AgentDID() != did || doc.V != agentdid.Version || !doc.SigningUnavailable || doc.Encryption == nil || len(doc.Custody) == 0 {
		t.Fatalf("invalid lazy upgrade: %+v", doc)
	}
	if _, err := agent.SignStatement("receipt/v1", nil); !errors.Is(err, agentdid.ErrUnavailable) {
		t.Fatalf("discarded legacy key must be unavailable: %v", err)
	}
	box, err := agentdid.Seal(*doc, "mail/v1", []byte("legacy mail"))
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := agent.OpenSealed("mail/v1", box); err != nil || string(plain) != "legacy mail" {
		t.Fatalf("upgraded legacy decryption: %v", err)
	}
	agent.stampMeta()
	next, _ := LoadMeta(place.Dir)
	if next.AgentIdentity.Encryption.Public != doc.Encryption.Public || !bytes.Equal(next.AgentIdentity.Custody, doc.Custody) {
		t.Fatal("a later save replaced upgraded keys")
	}
}

func TestAgentIdentityFutureDocumentPreservedVerbatimOnMetaWrites(t *testing.T) {
	for _, fields := range []string{`"did":"future", "session":"future-chat",`, `"future_binding":["opaque"],`} {
		t.Run(fields, func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			document := `{"v":"v99", ` + fields + ` "statements": [ {"what": "future", "n": 1.00} ]}`
			place, path, before := identityTruthFixture(t, "future-chat", document)
			agent := openIdentityAgent(t, place, nil)
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("opening changed a future document")
			}
			agent.stampMeta()
			for range 2 {
				meta, err := LoadMeta(place.Dir)
				if err != nil {
					t.Fatal(err)
				}
				meta.Title = "an unrelated change"
				if err := SaveMeta(place.Dir, meta); err != nil {
					t.Fatal(err)
				}
				after, _ := os.ReadFile(path)
				if !bytes.Contains(after, []byte(document)) {
					t.Fatalf("future document rewritten: %s", after)
				}
			}
		})
	}
}

func TestAgentIdentityRestoringSavedDialsDoesNotMigrateOnOpen(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	did, _ := agentdid.New()
	document, _ := json.Marshal(AgentIdentity{DID: did, Session: "saved-dials"})
	place, path, _ := identityTruthFixture(t, "saved-dials", string(document))
	raw := []byte(`{"V":1,"approval":"allow","effort":"high","agent_identity":` + string(document) + `}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = place
		config.ApprovalGate = &fakeApprovalGate{standing: PostureAsk}
	})
	if err := agent.SetApprovalPosture(PostureAllow); err != nil {
		t.Fatal(err)
	}
	if !agent.SetConversationEffort("high") {
		t.Fatal("saved effort was refused")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(raw, after) {
		t.Fatal("restoring preferences migrated the identity on open")
	}
	if agent.ApprovalPosture() != PostureAllow || agent.ConversationEffort() != "high" {
		t.Fatal("restored preferences were not applied in memory")
	}
	if !agent.SetConversationEffort("low") {
		t.Fatal("new effort was refused")
	}
	meta, _ := LoadMeta(place.Dir)
	if meta.AgentIdentity.V != agentdid.Version || meta.Effort != "low" {
		t.Fatal("an actual preference change did not persist and migrate")
	}
}

func TestAgentIdentityCustodyPreservesExistingProfilesSyncPosture(t *testing.T) {
	for _, solo := range []bool{false, true} {
		t.Run(map[bool]string{false: "unmarked", true: "solo"}[solo], func(t *testing.T) {
			profile := t.TempDir()
			t.Setenv(home.EnvVar, profile)
			ensure := identity.Ensure
			if solo {
				ensure = identity.EnsureSolo
			}
			if _, err := ensure(profile); err != nil {
				t.Fatal(err)
			}
			agent := openIdentityAgent(t, identityPlace(t, "sync-posture"), nil)
			agent.stampMeta()
			if identity.Solo(profile) != solo {
				t.Fatal("custody changed whether this profile talks to sync")
			}
		})
	}
}

func TestAgentIdentityCustodyMakesAFreshProfileQuiet(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(home.EnvVar, profile)
	agent := openIdentityAgent(t, identityPlace(t, "fresh-profile"), nil)
	agent.stampMeta()
	if !identity.Solo(profile) {
		t.Fatal("custody made a fresh profile eligible for sync")
	}
}
