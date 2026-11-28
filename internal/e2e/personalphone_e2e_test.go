//go:build e2e

package e2e

// personalphone_e2e_test.go is THE KEYLESS END-TO-END PROOF OF THE PERSONAL
// PHONE (design 10.3): real `bin/codeaf` child processes (`phone serve`,
// `pair approve`, `devices revoke`), a real local Go relay behind a wire
// recorder, the scripted model, and the stand-in phone (`internal/phonetest`).
//
// EVERY ASSERTION ABOUT WHAT A COMPUTER SENT READS THE RECORDED WIRE. The
// reverse proxy in front of the relay records the HTTP method, path, raw query,
// and verified caller device id for every request that reaches the relay.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/phonetest"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

const (
	ppNameA     = "desktop-a"
	ppNameB     = "laptop-b"
	ppNameC     = "solo-c"
	ppNameD     = "tablet-d"
	ppPhoneName = "pixel"

	ppPhoneServeStartLine = "serving your phone on this computer. ctrl+c stops it."
	ppPhoneQuietLine      = "no phone is paired with this computer yet. Approve one first, then run this again."
	ppWrongCheckLine      = "that check number does not match. Nothing was approved."

	ppServeStartWithin = 25 * time.Second
	ppDiscoveryWithin  = 30 * time.Second
	ppCallWithin       = 35 * time.Second
	ppRevokeWithin     = 25 * time.Second
	// ppFirstRoundWithin is how long after its start line a restarted computer may
	// take to claim an entry that was already waiting for it.
	ppFirstRoundWithin = 5 * time.Second
)

// TestPersonalPhone proves the computer side of the personal phone through the
// shipped binary without provider keys, external network access, or tmux.
func TestPersonalPhone(t *testing.T) {
	binary(t)
	for _, sub := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"P8_quiet_until_a_phone_exists", ppScenarioP8QuietUntilPhoneExists},
		{"D3_a_fresh_computer_says_no_phone_is_paired", ppScenarioD3FreshComputerQuiet},
		{"P2_P9_pairing_through_the_real_door", ppScenarioP2P9PairingThroughTheRealDoor},
		{"P1_P4_P5_one_pairing_both_computers", ppScenarioP1P4P5OnePairingBothComputers},
		{"S1_S2_L1_start_a_chat_on_B_see_the_reply", ppScenarioS1S2L1StartChatOnBSeeReply},
		{"S2_create_idempotency", ppScenarioS2CreateIdempotency},
		{"V1_revoke", ppScenarioV1Revoke},
		{"P13_solo_computer_serves_a_phone_without_sharing_chats", ppScenarioP13SoloComputerServesPhoneWithoutSharingChats},
		{"S5_lifetime", ppScenarioS5Lifetime},
		{"D5_two_chats_in_a_row_are_both_listed", ppScenarioD5TwoChatsInARowAreBothListed},
		{"D5_a_restart_keeps_the_released_chat_in_the_list", ppScenarioD5RestartKeepsTheReleasedChatInTheList},
		{"D7_with_relay_storage_off_a_phone_still_starts_a_chat_and_sees_the_reply", ppScenarioD7StorageOffPhoneStillStartsAChat},
	} {
		t.Run(sub.name, sub.run)
	}
}

// ppHelloAsk is what the phone types in the storage-off scenario. It carries the
// scripted model's marker and none of its special words, so the model answers it
// with rcHostReply.
const ppHelloAsk = "rc-host say hello"

// ppScenarioD7StorageOffPhoneStillStartsAChat proves D7: with the relay's storage
// switched off, a phone can still start a chat on a computer and see its reply.
// Storage off means the relay keeps no chat content; it never meant that a
// computer may not start a chat, and a phone reads a computer's chats from the
// slots sealed to it, which is phone traffic and not storage.
//
// The relay here is the real Go relay started with `--storage off`, so it
// refuses every chat upload exactly as the hosted relay does for an account it
// has not approved. Nothing writes the computers' storage answer: they learn it
// from the relay's own refusal.
func ppScenarioD7StorageOffPhoneStillStartsAChat(t *testing.T) {
	rig := newPPRig(t, "--storage", "off")
	rig.startPhoneServe(rig.compA)
	rig.startPhoneServe(rig.compB)
	rig.pairPhoneAt(rig.compA)

	rig.waitForComputers(ppDiscoveryWithin, rig.compA.device, rig.compB.device)
	rig.waitForCard(rig.compA.device, ppDiscoveryWithin)
	rig.waitForCard(rig.compB.device, ppDiscoveryWithin)

	ctx := t.Context()
	createRef, err := rig.phone.Create(ctx, rig.compB.device)
	if err != nil {
		t.Fatalf("phone.Create(B): %v", err)
	}
	createOut := rig.waitOutcome(rig.compB.device, createRef, ppCallWithin)
	if createOut.Status != clientproto.OutcomeDone || createOut.Agent == "" {
		t.Fatalf("with relay storage off, Create(B) outcome = %+v, want status=done with a chat", createOut)
	}
	chatID := createOut.Agent
	if !ppChatExistsInHome(rig.compB.home, chatID) {
		t.Fatalf("created chat %s does not exist under B's home %s", chatID, rig.compB.home)
	}
	rig.waitForEntries(rig.compB.device, ppCallWithin, "the new chat listed in B's agents.entries", func(envs []ppEntriesEnvelope) bool {
		return ppEnvelopesHaveChat(envs, chatID)
	})

	sendRef, err := rig.phone.Send(ctx, rig.compB.device, chatID, ppHelloAsk)
	if err != nil {
		t.Fatalf("phone.Send(B, %s): %v", chatID, err)
	}
	if sendOut := rig.waitOutcome(rig.compB.device, sendRef, ppCallWithin); sendOut.Status != clientproto.OutcomeDone || sendOut.Agent != chatID {
		t.Fatalf("with relay storage off, Send(B) outcome = %+v, want status=done agent=%s", sendOut, chatID)
	}
	replied := rig.waitForEntries(rig.compB.device, ppCallWithin, "the scripted reply in B's agents.entries", func(envs []ppEntriesEnvelope) bool {
		for _, env := range envs {
			if env.Agent != chatID {
				continue
			}
			for _, e := range env.Result.Entries {
				if strings.Contains(e.Text, rcHostReply) {
					return true
				}
			}
		}
		return false
	})

	// The phone sees the conversation and nothing else: no line about a chat that
	// could not be saved or shared, which would be noise about a relay that is
	// working as it was set up.
	for _, env := range replied {
		if env.Agent != chatID {
			continue
		}
		for _, e := range env.Result.Entries {
			if e.Kind == appwire.EntryNotice && !strings.HasSuffix(e.Text, " started this chat.") {
				t.Fatalf("with relay storage off the chat carries the notice %q, want only the line saying the phone started it", e.Text)
			}
		}
	}
	// The phone's traffic is not storage: the relay still holds no chat content,
	// and the chat is on B and no other computer.
	if ppChatExistsInHome(rig.compA.home, chatID) {
		t.Fatalf("created chat %s unexpectedly exists under A's home %s", chatID, rig.compA.home)
	}
	var held []string
	_ = filepath.Walk(filepath.Join(rig.root, "relay", "blobs"), func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			held = append(held, path)
		}
		return nil
	})
	if len(held) > 0 {
		t.Fatalf("the relay holds chat content with storage off: %d file(s), first %s", len(held), held[0])
	}
}

// ── scenarios ────────────────────────────────────────────────────────────────

// ppScenarioP8QuietUntilPhoneExists proves contract P8: with A and B running
// `codeaf phone serve` and no phone enrolled, both open their `serving=1`
// directory watch and neither computer makes any request under `/v1/clients`.
func ppScenarioP8QuietUntilPhoneExists(t *testing.T) {
	rig := newPPRig(t)
	rig.startPhoneServe(rig.compA)
	rig.startPhoneServe(rig.compB)

	rig.waitForServingWatch(rig.compA.device, ppDiscoveryWithin)
	rig.waitForServingWatch(rig.compB.device, ppDiscoveryWithin)

	// Give both runners an additional poll beat after both serving watches are
	// open so any eager client-fabric request would reach the recorder.
	rig.waitForDirListCount(rig.compA.device, 1, 10*time.Second)
	rig.waitForDirListCount(rig.compB.device, 1, 10*time.Second)

	calls := rig.recorder.snapshot()
	for _, c := range calls {
		if strings.HasPrefix(c.path, "/v1/clients") {
			t.Fatalf("recorder saw %s before any phone enrolled (all calls: %s)", c.describe(), rig.recorder.summary())
		}
	}
	for _, comp := range []ppComputer{rig.compA, rig.compB} {
		sawList := false
		sawServingWatch := false
		for _, c := range calls {
			if c.device != comp.device {
				continue
			}
			if c.method == http.MethodGet && c.path == "/v1/dir/list" {
				sawList = true
			}
			if c.method == http.MethodGet && c.path == "/v1/dir/watch" && ppQueryHasServing(c.rawQuery) {
				sawServingWatch = true
			}
		}
		if !sawList || !sawServingWatch {
			t.Fatalf("computer %s (%s) missing dir list (%v) or serving=1 watch (%v); calls: %s",
				comp.name, comp.device, sawList, sawServingWatch, rig.recorder.summaryFor(comp.device))
		}
	}
}

// ppScenarioD3FreshComputerQuiet proves D3 of the first emulator run: on a
// computer whose home holds no identity at all, `codeaf phone serve` exits
// non-zero with the quiet sentence the manual promises, never the sentence that
// tells a person to import an identity, and sends nothing to the relay.
func ppScenarioD3FreshComputerQuiet(t *testing.T) {
	root := ppNewShortRoot(t)
	model := startRCModel(t)
	relayURL, recorder := startPersonalPhoneRelay(t, root)
	t.Setenv(syncsetup.URLVar, relayURL)
	comp := ppComputer{name: "fresh-e", home: filepath.Join(root, "e"), work: filepath.Join(root, "we")}
	if err := os.MkdirAll(comp.home, 0o700); err != nil {
		t.Fatal(err)
	}
	workspaceAt(t, comp.work, false)
	ppWriteConfig(t, comp.home)
	rig := &ppRig{t: t, root: root, model: model, recorder: recorder, relayURL: relayURL}
	recorder.reset()

	out, err := rig.runCLI(comp, "phone", "serve")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("phone serve on a fresh computer: err = %v, want a non-zero exit; output:\n%s", err, out)
	}
	if !strings.Contains(out, ppPhoneQuietLine) {
		t.Fatalf("phone serve on a fresh computer said %q, want it to contain %q", out, ppPhoneQuietLine)
	}
	if strings.Contains(out, "identity import") {
		t.Fatalf("phone serve on a fresh computer told the person to import an identity: %q", out)
	}
	if _, loadErr := identity.Load(comp.home); !errors.Is(loadErr, identity.ErrNone) {
		t.Fatalf("phone serve made an identity on a fresh computer: load err = %v", loadErr)
	}
	if calls := recorder.snapshot(); len(calls) != 0 {
		t.Fatalf("a fresh computer sent requests before anything was paired: %s", recorder.summary())
	}
}

// ppScenarioP2P9PairingThroughTheRealDoor proves contracts P2 and P9 (and the
// wire side of P3): `codeaf pair approve <token> --check <wrong>` exits
// non-zero with the mismatch sentence and grants nothing, while approving with
// the phone's locally computed check digits prints the phone approval sentence
// and delivers a grant naming this identity's public key, with the phone only
// ever signing requests under its own device id.
func ppScenarioP2P9PairingThroughTheRealDoor(t *testing.T) {
	rig := newPPRig(t)
	rig.startPhoneServe(rig.compA)
	rig.startPhoneServe(rig.compB)

	ctx := t.Context()
	link, err := rig.phone.StartLink(ctx, rig.relayURL)
	if err != nil {
		t.Fatalf("StartLink: %v", err)
	}

	wrongDigits := "0000"
	if link.CheckDigits == wrongDigits {
		wrongDigits = "0001"
	}
	outWrong, errWrong := rig.runCLI(rig.compA, "pair", "approve", link.Token, "--check", wrongDigits)
	if errWrong == nil {
		t.Fatalf("pair approve with wrong check digits succeeded; output:\n%s", outWrong)
	}
	if !strings.Contains(outWrong, ppWrongCheckLine) {
		t.Fatalf("pair approve wrong check output = %q, want substring %q", outWrong, ppWrongCheckLine)
	}

	shortCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	_, errAwait := rig.phone.AwaitGrant(shortCtx, link)
	cancel()
	if !errors.Is(errAwait, context.DeadlineExceeded) {
		t.Fatalf("AwaitGrant after wrong check digits = %v, want context.DeadlineExceeded", errAwait)
	}
	if rig.phone.Paired() {
		t.Fatal("phone reported Paired() == true after wrong check digits refusal")
	}

	outOK, errOK := rig.runCLI(rig.compA, "pair", "approve", link.Token, "--check", link.CheckDigits)
	if errOK != nil {
		t.Fatalf("pair approve with matching check digits failed: %v\n%s", errOK, outOK)
	}
	wantLine := pair.ApprovedPhoneLine(rig.phone.Name())
	if !strings.Contains(outOK, wantLine) {
		t.Fatalf("pair approve output = %q, want substring %q", outOK, wantLine)
	}

	grant, err := rig.phone.AwaitGrant(ctx, link)
	if err != nil {
		t.Fatalf("AwaitGrant after matching approval: %v", err)
	}
	if !bytes.Equal(grant.IdentityPub, rig.identity.PublicKey()) {
		t.Fatalf("grant.IdentityPub = %x, want %x", grant.IdentityPub, rig.identity.PublicKey())
	}

	// Make a signed request from the phone so the recorder observes the phone's
	// signed wire traffic after pairing.
	if _, err := rig.phone.Computers(ctx); err != nil {
		t.Fatalf("phone.Computers: %v", err)
	}

	phoneID := rig.phone.DeviceID()
	wantPhonePubHex := hex.EncodeToString(rig.phone.SignPublicKey())
	knownComputers := map[string]bool{
		rig.compA.device: true,
		rig.compB.device: true,
	}
	sawPhoneSigned := false
	for _, c := range rig.recorder.snapshot() {
		if c.device == phoneID {
			sawPhoneSigned = true
			if !c.signed || !c.sigValid || !strings.EqualFold(c.signerPub, wantPhonePubHex) {
				t.Fatalf("phone request %s did not carry a valid signature by the phone's own device key", c.describe())
			}
		}
		if !c.signed && !strings.HasPrefix(c.path, "/v1/link/requests") && !strings.HasPrefix(c.path, "/v1/dir/link/requests") && c.path != "/status" {
			t.Fatalf("unexpected unsigned request on relay: %s", c.describe())
		}
		if c.signed && c.device != phoneID && !knownComputers[c.device] {
			t.Fatalf("recorder saw unknown signed device %q on %s", c.device, c.describe())
		}
	}
	if !sawPhoneSigned {
		t.Fatal("recorder never saw a signed request from the phone")
	}
}

// ppScenarioP1P4P5OnePairingBothComputers proves contracts P1, P4, and P5:
// after approving the phone on A only (with B already running `phone serve`
// and untouched), the phone's `Computers` lists both A and B and `Card(B)`
// verifies; and a computer D added to the same identity and started after the
// phone is already paired also appears and publishes a verified card without
// re-pairing the phone.
func ppScenarioP1P4P5OnePairingBothComputers(t *testing.T) {
	rig := newPPRig(t)
	rig.startPhoneServe(rig.compA)
	rig.startPhoneServe(rig.compB)

	rig.pairPhoneAt(rig.compA)

	// P1 / P4: without any action on B after approval at A, B appears in
	// Computers with Serving=true and publishes a verifiable executor card.
	rig.waitForComputers(ppDiscoveryWithin, rig.compA.device, rig.compB.device)
	cardB := rig.waitForCard(rig.compB.device, ppDiscoveryWithin)
	if cardB.Writer != rig.compB.device || cardB.Name != rig.compB.name {
		t.Fatalf("Card(B) = %+v, want Writer=%s Name=%s", cardB, rig.compB.device, rig.compB.name)
	}

	// P5: a computer paired to the identity and started after the phone was
	// paired also appears in Computers and publishes a verifiable card without
	// the phone re-pairing.
	compD := rig.addSharedComputer(ppNameD)
	rig.startPhoneServe(compD)
	rig.waitForComputers(ppDiscoveryWithin, rig.compA.device, rig.compB.device, compD.device)
	cardD := rig.waitForCard(compD.device, ppDiscoveryWithin)
	if cardD.Writer != compD.device || cardD.Name != compD.name {
		t.Fatalf("Card(D) = %+v, want Writer=%s Name=%s", cardD, compD.device, compD.name)
	}
}

// ppScenarioS1S2L1StartChatOnBSeeReply proves contracts S1, S2, L1, and the
// design 3.5 client_id echo requirement: creating a chat on B produces a
// `done` outcome naming the chat, creates the chat only on B (not on A), lists
// it in B's `agents.entries` (and not A's), and sending a message to it echoes
// the phone's `client_id` on the user entry and delivers the scripted model's
// reply in B's `agents.entries`.
func ppScenarioS1S2L1StartChatOnBSeeReply(t *testing.T) {
	rig := newPPRig(t)
	rig.startPhoneServe(rig.compA)
	rig.startPhoneServe(rig.compB)
	rig.pairPhoneAt(rig.compA)

	rig.waitForComputers(ppDiscoveryWithin, rig.compA.device, rig.compB.device)
	rig.waitForCard(rig.compA.device, ppDiscoveryWithin)
	rig.waitForCard(rig.compB.device, ppDiscoveryWithin)

	ctx := t.Context()
	createRef, err := rig.phone.Create(ctx, rig.compB.device)
	if err != nil {
		t.Fatalf("phone.Create(B): %v", err)
	}
	createOut := rig.waitOutcome(rig.compB.device, createRef, ppCallWithin)
	if createOut.Status != clientproto.OutcomeDone || createOut.Agent == "" {
		t.Fatalf("Create(B) outcome = %+v, want status=done with non-empty agent", createOut)
	}
	chatID := createOut.Agent

	// S1: the chat exists on B's engine disk and on NO other computer.
	if !ppChatExistsInHome(rig.compB.home, chatID) {
		t.Fatalf("created chat %s does not exist under B's home %s", chatID, rig.compB.home)
	}
	if ppChatExistsInHome(rig.compA.home, chatID) {
		t.Fatalf("created chat %s unexpectedly exists under A's home %s", chatID, rig.compA.home)
	}

	// B's agents.entries lists the chat; A's agents.entries does not list it.
	rig.waitForEntries(rig.compB.device, ppCallWithin, "created chat listed in B's agents.entries", func(envs []ppEntriesEnvelope) bool {
		return ppEnvelopesHaveChat(envs, chatID)
	})
	envsA := rig.waitForAnyEntriesSlot(rig.compA.device, ppDiscoveryWithin)
	if ppEnvelopesHaveChat(envsA, chatID) {
		t.Fatalf("A's agents.entries unexpectedly lists B's chat %s: %+v", chatID, envsA)
	}

	// Send a message with a client_id that triggers the scripted model's reply.
	sendRef, err := rig.phone.Send(ctx, rig.compB.device, chatID, rcHostAsk)
	if err != nil {
		t.Fatalf("phone.Send(B, %s): %v", chatID, err)
	}
	if sendRef.ClientID == "" {
		t.Fatalf("phone.Send returned empty ClientID: %+v", sendRef)
	}
	sendOut := rig.waitOutcome(rig.compB.device, sendRef, ppCallWithin)
	if sendOut.Status != clientproto.OutcomeDone || sendOut.Agent != chatID {
		t.Fatalf("Send(B) outcome = %+v, want status=done agent=%s", sendOut, chatID)
	}

	// Wait for the scripted model's reply to appear in B's agents.entries slot.
	envsB := rig.waitForEntries(rig.compB.device, ppCallWithin, "model reply in B's agents.entries", func(envs []ppEntriesEnvelope) bool {
		for _, env := range envs {
			if env.Agent != chatID {
				continue
			}
			for _, e := range env.Result.Entries {
				if strings.Contains(e.Text, rcHostReply) {
					return true
				}
			}
		}
		return false
	})

	// Coordinator requirement (design 3.5): the client_id the phone drew must
	// appear on the person's own entry for that message in the messages slot.
	var userEntry *appwire.Entry
	var chatEntries []appwire.Entry
	for _, env := range envsB {
		if env.Agent != chatID {
			continue
		}
		chatEntries = env.Result.Entries
		for i := range env.Result.Entries {
			e := &env.Result.Entries[i]
			if e.Text == rcHostAsk || (e.Kind == appwire.EntryUser && strings.Contains(e.Text, rcHostAsk)) {
				userEntry = e
				break
			}
		}
	}
	if userEntry == nil {
		t.Fatalf("B's agents.entries for chat %s has no entry for sent text %q; entries=%+v", chatID, rcHostAsk, chatEntries)
	}
	if userEntry.ClientID != sendRef.ClientID {
		t.Fatalf("user entry client_id echo mismatch in slot agents.entries.%s for chat %s: entry=%+v got client_id=%q, want %q",
			rig.compB.device, chatID, *userEntry, userEntry.ClientID, sendRef.ClientID)
	}
}

// ppScenarioS2CreateIdempotency proves contract S2: queueing two `agents.create`
// calls with the same explicit `ClientID` creates only one chat on B and both
// outcomes report `done` with the same chat id.
func ppScenarioS2CreateIdempotency(t *testing.T) {
	rig := newPPRig(t)
	rig.startPhoneServe(rig.compA)
	rig.startPhoneServe(rig.compB)
	rig.pairPhoneAt(rig.compA)

	rig.waitForComputers(ppDiscoveryWithin, rig.compA.device, rig.compB.device)
	rig.waitForCard(rig.compB.device, ppDiscoveryWithin)

	ctx := t.Context()
	const explicitClientID = "11111111222222223333333344444444"

	ref1, err := rig.phone.Call(ctx, rig.compB.device, clientproto.MethodCreate, phonetest.CreateParams{}, phonetest.CallOptions{
		ClientID: explicitClientID,
	})
	if err != nil {
		t.Fatalf("first Create with explicit ClientID: %v", err)
	}
	out1 := rig.waitOutcome(rig.compB.device, ref1, ppCallWithin)
	if out1.Status != clientproto.OutcomeDone || out1.Agent == "" {
		t.Fatalf("first Create outcome = %+v, want done with agent", out1)
	}

	ref2, err := rig.phone.Call(ctx, rig.compB.device, clientproto.MethodCreate, phonetest.CreateParams{}, phonetest.CallOptions{
		ClientID: explicitClientID,
	})
	if err != nil {
		t.Fatalf("second Create with same ClientID: %v", err)
	}
	if ref2.Number <= ref1.Number {
		t.Fatalf("second Create number = %d, want > %d", ref2.Number, ref1.Number)
	}
	out2 := rig.waitOutcome(rig.compB.device, ref2, ppCallWithin)
	if out2.Status != clientproto.OutcomeDone || out2.Agent != out1.Agent {
		t.Fatalf("second Create outcome = %+v, want status=done agent=%s", out2, out1.Agent)
	}

	if got := ppCountChatsInHome(rig.compB.home); got != 1 {
		t.Fatalf("B has %d chat folders under %s after duplicate Create, want 1", got, rig.compB.home)
	}
	envsB := rig.waitForEntries(rig.compB.device, ppCallWithin, "single chat in B's agents.entries", func(envs []ppEntriesEnvelope) bool {
		return ppEnvelopesHaveChat(envs, out1.Agent)
	})
	if len(envsB) != 1 || envsB[0].Agent != out1.Agent {
		t.Fatalf("B's agents.entries envelopes = %+v, want exactly 1 envelope for %s", envsB, out1.Agent)
	}
}

// ppScenarioV1Revoke proves contract V1: revoking the phone on A with
// `codeaf devices revoke <phone name>` cuts off the phone at the relay and
// causes B to stop reading that phone's queue, so a post-revoke `Send` never
// runs or reaches the scripted model.
func ppScenarioV1Revoke(t *testing.T) {
	rig := newPPRig(t)
	rig.startPhoneServe(rig.compA)
	rig.startPhoneServe(rig.compB)
	rig.pairPhoneAt(rig.compA)

	rig.waitForComputers(ppDiscoveryWithin, rig.compA.device, rig.compB.device)
	rig.waitForCard(rig.compB.device, ppDiscoveryWithin)

	ctx := t.Context()
	createRef, err := rig.phone.Create(ctx, rig.compB.device)
	if err != nil {
		t.Fatalf("phone.Create(B): %v", err)
	}
	createOut := rig.waitOutcome(rig.compB.device, createRef, ppCallWithin)
	if createOut.Status != clientproto.OutcomeDone || createOut.Agent == "" {
		t.Fatalf("Create(B) outcome = %+v", createOut)
	}
	chatID := createOut.Agent

	// Revoke the phone on computer A through the real CLI door.
	outRevoke, errRevoke := rig.runCLI(rig.compA, "devices", "revoke", rig.phone.Name())
	if errRevoke != nil {
		t.Fatalf("devices revoke %s failed: %v\n%s", rig.phone.Name(), errRevoke, outRevoke)
	}
	wantRevokeLine := pair.PhoneRevokedLine(rig.phone.Name())
	if !strings.Contains(outRevoke, wantRevokeLine) {
		t.Fatalf("devices revoke output = %q, want substring %q", outRevoke, wantRevokeLine)
	}

	// Wait until the relay reports the phone as revoked and B has processed the
	// revocation tombstoning the phone locally.
	ppWaitUntil(t, ppRevokeWithin, "phone reported Revoked and B wrote tombstone", func() bool {
		return rig.phone.Revoked(ctx) && ppTombstoneHas(rig.compB.home, rig.phone.DeviceID())
	})

	// Wait for any in-flight round on B that started before the tombstone to settle,
	// then verify B never reads the phone's queue again across a quiet window.
	queuePath := "/v1/clients/" + rig.phone.DeviceID() + "/queue"
	rig.waitForNoNewMatchingRequests(ppRevokeWithin, 1500*time.Millisecond, "B reading revoked phone's queue", func(c ppRecordedCall) bool {
		return c.device == rig.compB.device && strings.HasPrefix(c.path, queuePath)
	})

	const revokedWords = "rc-host words after phone revoke"
	_, sendErr := rig.phone.Send(ctx, rig.compB.device, chatID, revokedWords)
	if !errors.Is(sendErr, phonetest.ErrRevoked) {
		t.Fatalf("phone.Send after revoke = %v, want phonetest.ErrRevoked", sendErr)
	}

	// Wait one runner refresh cycle to prove B neither polls the revoked phone's
	// queue nor asks the scripted model the post-revoke words.
	rig.assertQuietFor(6*time.Second, "B reading revoked phone's queue after refused Send", func(c ppRecordedCall) bool {
		return c.device == rig.compB.device && strings.HasPrefix(c.path, queuePath)
	})
	if got := rig.model.count(revokedWords); got != 0 {
		t.Fatalf("scripted model was asked post-revoke words %d times, want 0", got)
	}
}

// ppScenarioP13SoloComputerServesPhoneWithoutSharingChats proves contract P13:
// solo computer C makes zero relay requests before a phone is approved (and
// `codeaf phone serve` refuses with the quiet sentence), stays solo after
// `codeaf pair approve`, serves the phone (`Create`, `agents.entries`, `Send`
// and reply) while sending only the allowed phone-serving routes and never any
// chat-sync / cell / vault / store / lease / cluster route, and goes completely
// quiet again once the phone is revoked on C.
func ppScenarioP13SoloComputerServesPhoneWithoutSharingChats(t *testing.T) {
	rig := newPPSoloRig(t)
	ctx := t.Context()

	if !identity.Solo(rig.compC.home) {
		t.Fatalf("identity.Solo(%s) = false before pairing, want true", rig.compC.home)
	}
	if calls := rig.recorder.callsFrom(rig.compC.device); len(calls) != 0 {
		t.Fatalf("solo computer C sent requests before any command: %v", calls)
	}

	// Before any phone is paired, `codeaf phone serve` on C must refuse with the
	// quiet sentence and make zero relay requests.
	outQuiet, errQuiet := rig.runCLI(rig.compC, "phone", "serve")
	if errQuiet == nil {
		t.Fatalf("phone serve on solo computer C with no paired phone succeeded; output:\n%s", outQuiet)
	}
	if !strings.Contains(outQuiet, ppPhoneQuietLine) {
		t.Fatalf("phone serve quiet refusal = %q, want substring %q", outQuiet, ppPhoneQuietLine)
	}
	if calls := rig.recorder.snapshot(); len(calls) != 0 {
		t.Fatalf("solo computer C made relay requests before any phone enrolled: %s", rig.recorder.summary())
	}

	// Start the phone link and approve it on C through the real CLI door.
	link, err := rig.phone.StartLink(ctx, rig.relayURL)
	if err != nil {
		t.Fatalf("StartLink: %v", err)
	}
	approveStartIdx := len(rig.recorder.snapshot())
	outApprove, errApprove := rig.runCLI(rig.compC, "pair", "approve", link.Token, "--check", link.CheckDigits)
	if errApprove != nil {
		t.Fatalf("pair approve on solo C failed: %v\n%s", errApprove, outApprove)
	}
	if !strings.Contains(outApprove, pair.ApprovedPhoneLine(rig.phone.Name())) {
		t.Fatalf("pair approve on solo C output = %q, want %q", outApprove, pair.ApprovedPhoneLine(rig.phone.Name()))
	}
	if _, err := rig.phone.AwaitGrant(ctx, link); err != nil {
		t.Fatalf("AwaitGrant from solo C: %v", err)
	}

	// Contract P13: C stays solo after approving a phone.
	if !identity.Solo(rig.compC.home) {
		t.Fatalf("identity.Solo(%s) = false after approving a phone, want true", rig.compC.home)
	}
	if syncsetup.MayTalk(rig.compC.home) {
		t.Fatalf("syncsetup.MayTalk(%s) = true on solo computer C, want false", rig.compC.home)
	}

	serveStartIdx := len(rig.recorder.snapshot())
	rig.startPhoneServe(rig.compC)
	rig.waitForServingWatch(rig.compC.device, ppDiscoveryWithin)
	rig.waitForComputers(ppDiscoveryWithin, rig.compC.device)
	rig.waitForCard(rig.compC.device, ppDiscoveryWithin)

	// Phone creates a chat on C, sees it in agents.entries, sends a message, and sees the reply.
	createRef, err := rig.phone.Create(ctx, rig.compC.device)
	if err != nil {
		t.Fatalf("phone.Create(C): %v", err)
	}
	createOut := rig.waitOutcome(rig.compC.device, createRef, ppCallWithin)
	if createOut.Status != clientproto.OutcomeDone || createOut.Agent == "" {
		t.Fatalf("Create(C) outcome = %+v, want done with agent", createOut)
	}
	chatID := createOut.Agent

	rig.waitForEntries(rig.compC.device, ppCallWithin, "created chat listed in C's agents.entries", func(envs []ppEntriesEnvelope) bool {
		return ppEnvelopesHaveChat(envs, chatID)
	})

	sendRef, err := rig.phone.Send(ctx, rig.compC.device, chatID, rcHostAsk)
	if err != nil {
		t.Fatalf("phone.Send(C, %s): %v", chatID, err)
	}
	sendOut := rig.waitOutcome(rig.compC.device, sendRef, ppCallWithin)
	if sendOut.Status != clientproto.OutcomeDone || sendOut.Agent != chatID {
		t.Fatalf("Send(C) outcome = %+v, want status=done agent=%s", sendOut, chatID)
	}
	rig.waitForEntries(rig.compC.device, ppCallWithin, "model reply in C's agents.entries", func(envs []ppEntriesEnvelope) bool {
		for _, env := range envs {
			if env.Agent != chatID {
				continue
			}
			for _, e := range env.Result.Entries {
				if strings.Contains(e.Text, rcHostReply) {
					return true
				}
			}
		}
		return false
	})

	if !identity.Solo(rig.compC.home) {
		t.Fatalf("identity.Solo(%s) became false while serving phone, want true", rig.compC.home)
	}

	revokeStartIdx := len(rig.recorder.snapshot())
	outRevoke, errRevoke := rig.runCLI(rig.compC, "devices", "revoke", rig.phone.Name())
	if errRevoke != nil {
		t.Fatalf("devices revoke on solo C failed: %v\n%s", errRevoke, outRevoke)
	}
	if !strings.Contains(outRevoke, pair.PhoneRevokedLine(rig.phone.Name())) {
		t.Fatalf("devices revoke output = %q, want %q", outRevoke, pair.PhoneRevokedLine(rig.phone.Name()))
	}

	// After revoking the last phone on solo computer C, C must go completely
	// quiet and send no further requests to the relay.
	rig.waitForNoNewMatchingRequests(ppRevokeWithin, 2*time.Second, "solo C after revoking its phone", func(c ppRecordedCall) bool {
		return c.device == rig.compC.device
	})

	// Check every request C sent over the whole run against the contract P13
	// allowlist and forbidden-route list.
	allCalls := rig.recorder.snapshot()
	var badServingRun []string
	var badForbidden []string
	var badWholeRun []string
	phoneID := rig.phone.DeviceID()
	for i, c := range allCalls {
		if c.device != rig.compC.device {
			continue
		}
		phase := "before-approve"
		switch {
		case i >= revokeStartIdx:
			phase = "devices-revoke"
		case i >= serveStartIdx:
			phase = "phone-serve"
		case i >= approveStartIdx:
			phase = "pair-approve"
		}
		if ppIsForbiddenSoloPath(c.path) {
			badForbidden = append(badForbidden, fmt.Sprintf("[%s] %s", phase, c.describe()))
		}
		// THE RUNNER'S OWN TRAFFIC is held to the exact serving allowlist. The two
		// commands the owner types (the approval and the removal) are held to the
		// requests that doing that thing needs, and to the forbidden list like
		// everything else: they may enrol, approve, read the device list and revoke,
		// and must never touch the shared storage, a chat's cell, the vault or a lease.
		allowed := ppIsAllowedSoloServingRequest(c, rig.compC.device, phoneID)
		switch phase {
		case "pair-approve":
			allowed = allowed || ppIsAllowedSoloApproveRequest(c, phoneID)
		case "devices-revoke":
			allowed = allowed || ppIsAllowedSoloRevokeRequest(c, phoneID)
		}
		if ppIsForbiddenSoloPath(c.path) || !allowed {
			if phase == "phone-serve" {
				badServingRun = append(badServingRun, fmt.Sprintf("[%s] %s", phase, c.describe()))
			}
			badWholeRun = append(badWholeRun, fmt.Sprintf("[%s] %s", phase, c.describe()))
		}
	}
	if len(badServingRun) > 0 {
		t.Errorf("P13 violation during C's phone serve run: C sent disallowed requests: %s",
			strings.Join(badServingRun, "; "))
	}
	if len(badForbidden) > 0 {
		t.Errorf("P13 forbidden route violation on solo computer C: %s",
			strings.Join(badForbidden, "; "))
	}
	if len(badWholeRun) > 0 {
		t.Errorf("P13 violation over C's whole run: C sent requests outside the P13 allowlist: %s (all C calls: %s)",
			strings.Join(badWholeRun, "; "), rig.recorder.summaryFor(rig.compC.device))
	}
}

// ppScenarioS5Lifetime proves contract S5 (and S4): a `Create` with a short
// `opts.Lifetime` queued while B's `phone serve` is stopped causes the phone to
// derive `expired` once its clock passes `not_after`, and when B's `phone serve`
// is restarted the expired call does not run and creates no chat.
func ppScenarioS5Lifetime(t *testing.T) {
	rig := newPPRig(t)
	rig.startPhoneServe(rig.compA)
	procB := rig.startPhoneServe(rig.compB)
	rig.pairPhoneAt(rig.compA)

	rig.waitForComputers(ppDiscoveryWithin, rig.compA.device, rig.compB.device)
	rig.waitForCard(rig.compB.device, ppDiscoveryWithin)

	// Stop B's `phone serve` before queueing the short-lifetime Create.
	procB.stop()

	ctx := t.Context()
	expRef, err := rig.phone.Call(ctx, rig.compB.device, clientproto.MethodCreate, phonetest.CreateParams{}, phonetest.CallOptions{
		Lifetime: 300 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("phone.Call(Create, short lifetime): %v", err)
	}

	// While B is offline and the phone's clock passes not_after + ExpiryAllowance,
	// WaitOutcome derives expired from the phone's own clock.
	derivedOut, err := rig.phone.WaitOutcome(ctx, rig.compB.device, expRef, func() {
		time.Sleep(50 * time.Millisecond)
	})
	if err != nil {
		t.Fatalf("WaitOutcome(short lifetime while B stopped): %v", err)
	}
	if derivedOut.Status != clientproto.OutcomeExpired || derivedOut.Reason != clientproto.ReasonExpired {
		t.Fatalf("phone-derived outcome = %+v, want status=expired reason=expired", derivedOut)
	}

	// Restart B's `phone serve` and wait for B to drain and acknowledge the
	// expired queue entry without creating a chat.
	rig.startPhoneServe(rig.compB)
	ackPath := fmt.Sprintf("/v1/clients/%s/queue/%d", rig.phone.DeviceID(), expRef.Number)
	ppWaitUntil(t, ppCallWithin, "restarted B acknowledged expired queue entry", func() bool {
		for _, c := range rig.recorder.snapshot() {
			if c.device == rig.compB.device && c.method == http.MethodDelete && c.path == ackPath {
				return true
			}
		}
		return false
	})

	if got := ppCountChatsInHome(rig.compB.home); got != 0 {
		t.Fatalf("restarted B created %d chats for an expired Create, want 0", got)
	}
}

// ppMakeChat has the phone create a chat on computer comp and send it one
// message, and returns the chat's id once the message is answered.
func ppMakeChat(t *testing.T, rig *ppRig, comp ppComputer, ref func() (phonetest.CallRef, error)) string {
	t.Helper()
	made, err := ref()
	if err != nil {
		t.Fatalf("queue a create on %s: %v", comp.name, err)
	}
	out := rig.waitOutcome(comp.device, made, ppCallWithin)
	if out.Status != clientproto.OutcomeDone || out.Agent == "" {
		t.Fatalf("Create(%s) outcome = %+v, want status=done with a chat", comp.name, out)
	}
	sent, err := rig.phone.Send(t.Context(), comp.device, out.Agent, rcHostAsk)
	if err != nil {
		t.Fatalf("Send(%s, %s): %v", comp.name, out.Agent, err)
	}
	if sendOut := rig.waitOutcome(comp.device, sent, ppCallWithin); sendOut.Status != clientproto.OutcomeDone {
		t.Fatalf("Send(%s, %s) outcome = %+v", comp.name, out.Agent, sendOut)
	}
	return out.Agent
}

// ppScenarioD5TwoChatsInARowAreBothListed proves that the `agents.list` slot a
// computer publishes carries every chat it holds: two chats the phone makes on B
// one after the other, with no restart between them, are both in B's newest slot,
// each with its own row.
func ppScenarioD5TwoChatsInARowAreBothListed(t *testing.T) {
	rig := newPPRig(t)
	rig.startPhoneServe(rig.compA)
	rig.startPhoneServe(rig.compB)
	rig.pairPhoneAt(rig.compA)
	rig.waitForComputers(ppDiscoveryWithin, rig.compA.device, rig.compB.device)
	rig.waitForCard(rig.compB.device, ppDiscoveryWithin)

	create := func() (phonetest.CallRef, error) { return rig.phone.Create(t.Context(), rig.compB.device) }
	first := ppMakeChat(t, rig, rig.compB, create)
	second := ppMakeChat(t, rig, rig.compB, create)
	list := rig.waitForList(rig.compB.device, ppCallWithin, "both chats in B's agents.list", first, second)
	if len(list) != 2 {
		t.Fatalf("B holds two chats and its agents.list slot has %d envelopes: %s", len(list), ppDescribeList(list))
	}
	if !ppChatExistsInHome(rig.compB.home, first) || !ppChatExistsInHome(rig.compB.home, second) {
		t.Fatalf("B's home lacks one of the chats %s, %s", first, second)
	}
}

// ppScenarioD5RestartKeepsTheReleasedChatInTheList proves the contract for a
// chat across a restart of the computer's `phone serve`, which the first emulator
// run saw as "the phone lists only the newer chat". B's `phone serve` is stopped
// cleanly, which hands every chat it was running back (the lease is released),
// and the phone queues a second create meanwhile. When B comes back the first
// chat is still B's: nobody holds it and B was its last holder, so B's slot lists
// it beside the chat it just made. A message from the phone to it then takes the
// lease first (the conditional take) and runs, and the lease is B's afterwards.
//
// It also measures D4': after the restart the entry that was already waiting is
// claimed within a round of the start line.
func ppScenarioD5RestartKeepsTheReleasedChatInTheList(t *testing.T) {
	rig := newPPRig(t)
	rig.startPhoneServe(rig.compA)
	procB := rig.startPhoneServe(rig.compB)
	rig.pairPhoneAt(rig.compA)
	rig.waitForComputers(ppDiscoveryWithin, rig.compA.device, rig.compB.device)
	rig.waitForCard(rig.compB.device, ppDiscoveryWithin)

	ctx := t.Context()
	first := ppMakeChat(t, rig, rig.compB, func() (phonetest.CallRef, error) { return rig.phone.Create(ctx, rig.compB.device) })
	rig.waitForList(rig.compB.device, ppCallWithin, "the first chat in B's agents.list", first)

	procB.stop()
	secondRef, err := rig.phone.Call(ctx, rig.compB.device, clientproto.MethodCreate, phonetest.CreateParams{}, phonetest.CallOptions{})
	if err != nil {
		t.Fatalf("phone.Call(Create) while B is stopped: %v", err)
	}
	restarted := rig.startPhoneServe(rig.compB)
	second := ppMakeChat(t, rig, rig.compB, func() (phonetest.CallRef, error) { return secondRef, nil })

	// D4': the first claim of the waiting entry, counted from the start line.
	claimed := time.Time{}
	for _, c := range rig.recorder.snapshot() {
		if c.device == rig.compB.device && c.method == http.MethodPost && strings.HasSuffix(c.path, "/claim") && c.at.After(restarted.began) {
			claimed = c.at
			break
		}
	}
	if claimed.IsZero() {
		t.Fatalf("restarted B never claimed the waiting create: %s", rig.recorder.summaryFor(rig.compB.device))
	}
	afterReady := claimed.Sub(restarted.began) - restarted.ready
	t.Logf("D4': restarted B said it was serving after %s and claimed the waiting create %s later", restarted.ready.Round(time.Millisecond), afterReady.Round(time.Millisecond))
	if afterReady > ppFirstRoundWithin {
		t.Errorf("restarted B claimed the waiting create %s after its start line, want within %s", afterReady, ppFirstRoundWithin)
	}

	// The stop released the first chat to nobody, and the restart holds only the second.
	sa, ok, err := syncsetup.Open(rig.compA.home)
	if err != nil || !ok {
		t.Fatalf("syncsetup.Open(A): ok=%v err=%v", ok, err)
	}
	listing, err := sa.Dir.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if l := listing.Cells[first].Lease; l.Device != rig.compB.device || l.Expires != 0 || l.ReleasedAt == 0 {
		t.Fatalf("first chat's lease after B's clean stop = %+v, want released by B", l)
	}
	if l := listing.Cells[second].Lease; l.Device != rig.compB.device || l.Expires <= listing.Now {
		t.Fatalf("second chat's lease after B's restart = %+v, want held by B", l)
	}
	// Both chats are in B's slot: the one it holds and the one it released last.
	list := rig.waitForList(rig.compB.device, ppCallWithin, "both chats in B's agents.list", first, second)
	t.Logf("B's agents.list slot after the restart: %s", ppDescribeList(list))
	if !ppChatExistsInHome(rig.compB.home, first) || !ppChatExistsInHome(rig.compB.home, second) {
		t.Fatalf("B's home lacks one of the chats %s, %s", first, second)
	}

	// A message to the released chat takes it first, runs, and leaves it held by B.
	sendRef, err := rig.phone.Send(ctx, rig.compB.device, first, rcLaptopAsk)
	if err != nil {
		t.Fatalf("Send(B, released chat): %v", err)
	}
	if out := rig.waitOutcome(rig.compB.device, sendRef, ppCallWithin); out.Status != clientproto.OutcomeDone || out.Agent != first {
		t.Fatalf("Send(B, released chat) outcome = %+v, want done for %s", out, first)
	}
	rig.waitForEntries(rig.compB.device, ppCallWithin, "the reply to the message sent to the released chat", func(envs []ppEntriesEnvelope) bool {
		for _, env := range envs {
			if env.Agent != first {
				continue
			}
			for _, e := range env.Result.Entries {
				if strings.Contains(e.Text, rcLaptopReply) {
					return true
				}
			}
		}
		return false
	})
	if listing, err = sa.Dir.List(ctx); err != nil {
		t.Fatal(err)
	}
	if l := listing.Cells[first].Lease; l.Device != rig.compB.device || l.Expires <= listing.Now {
		t.Fatalf("first chat's lease after the phone's message = %+v, want held by B", l)
	}
}

// ── world and helpers ────────────────────────────────────────────────────────

type ppComputer struct {
	name   string
	home   string
	work   string
	device string
}

type ppRig struct {
	t        *testing.T
	root     string
	model    *rcModel
	recorder *ppRelayRecorder
	relayURL string
	identity identity.Identity
	compA    ppComputer
	compB    ppComputer
	compC    ppComputer
	phone    *phonetest.Phone

	entriesMu   sync.Mutex
	lastEntries map[string][]ppEntriesEnvelope
}

type ppEntriesEnvelope struct {
	Writer string                `json:"writer"`
	At     time.Time             `json:"at"`
	Agent  string                `json:"agent"`
	Result appwire.EntriesResult `json:"result"`
}

// newPPRig builds two computers of one identity, a phone and a relay. relayArgs
// are extra flags for the relay (the storage switch is one), so a scenario can
// ask for the relay a person's account really has.
func newPPRig(t *testing.T, relayArgs ...string) *ppRig {
	t.Helper()
	root := ppNewShortRoot(t)
	model := startRCModel(t)
	relayURL, recorder := startPersonalPhoneRelay(t, root, relayArgs...)

	compA := ppComputer{name: ppNameA, home: filepath.Join(root, "a"), work: filepath.Join(root, "wa")}
	compB := ppComputer{name: ppNameB, home: filepath.Join(root, "b"), work: filepath.Join(root, "wb")}
	for _, comp := range []ppComputer{compA, compB} {
		if err := os.MkdirAll(comp.home, 0o700); err != nil {
			t.Fatal(err)
		}
		workspaceAt(t, comp.work, false)
		ppWriteConfig(t, comp.home)
	}

	id, err := identity.Ensure(compA.home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(compB.home, id, false); err != nil {
		t.Fatal(err)
	}
	t.Setenv(syncsetup.URLVar, relayURL)
	for _, comp := range []*ppComputer{&compA, &compB} {
		if _, err := devname.Set(comp.home, comp.name); err != nil {
			t.Fatal(err)
		}
		s, ok, err := syncsetup.Open(comp.home)
		if err != nil || !ok {
			t.Fatalf("syncsetup.Open(%s): ok=%v err=%v", comp.name, ok, err)
		}
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, comp.name); err != nil {
			t.Fatal(err)
		}
		comp.device = s.Device.ID()
	}
	// Reset the recorder after test fixture setup so recorded requests reflect
	// only what the running computers and phone send during the scenario.
	recorder.reset()

	phone, err := phonetest.New(phonetest.Config{Name: ppPhoneName})
	if err != nil {
		t.Fatal(err)
	}
	return &ppRig{
		t:           t,
		root:        root,
		model:       model,
		recorder:    recorder,
		relayURL:    relayURL,
		identity:    id,
		compA:       compA,
		compB:       compB,
		phone:       phone,
		lastEntries: map[string][]ppEntriesEnvelope{},
	}
}

func newPPSoloRig(t *testing.T) *ppRig {
	t.Helper()
	root := ppNewShortRoot(t)
	model := startRCModel(t)
	relayURL, recorder := startPersonalPhoneRelay(t, root)
	t.Setenv(syncsetup.URLVar, relayURL)

	compC := ppComputer{name: ppNameC, home: filepath.Join(root, "c"), work: filepath.Join(root, "wc")}
	if err := os.MkdirAll(compC.home, 0o700); err != nil {
		t.Fatal(err)
	}
	workspaceAt(t, compC.work, false)
	ppWriteConfig(t, compC.home)

	id, err := identity.EnsureSolo(compC.home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := devname.Set(compC.home, compC.name); err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(compC.home)
	if err != nil {
		t.Fatal(err)
	}
	compC.device = dev.ID()
	recorder.reset()

	phone, err := phonetest.New(phonetest.Config{Name: ppPhoneName})
	if err != nil {
		t.Fatal(err)
	}
	return &ppRig{
		t:           t,
		root:        root,
		model:       model,
		recorder:    recorder,
		relayURL:    relayURL,
		identity:    id,
		compC:       compC,
		phone:       phone,
		lastEntries: map[string][]ppEntriesEnvelope{},
	}
}

func (r *ppRig) addSharedComputer(name string) ppComputer {
	r.t.Helper()
	comp := ppComputer{
		name: name,
		home: filepath.Join(r.root, name),
		work: filepath.Join(r.root, "w-"+name),
	}
	if err := os.MkdirAll(comp.home, 0o700); err != nil {
		r.t.Fatal(err)
	}
	workspaceAt(r.t, comp.work, false)
	ppWriteConfig(r.t, comp.home)
	if _, err := identity.Adopt(comp.home, r.identity, false); err != nil {
		r.t.Fatal(err)
	}
	if _, err := devname.Set(comp.home, comp.name); err != nil {
		r.t.Fatal(err)
	}
	s, ok, err := syncsetup.Open(comp.home)
	if err != nil || !ok {
		r.t.Fatalf("syncsetup.Open(%s): ok=%v err=%v", comp.name, ok, err)
	}
	if err := syncsetup.PutOwnDevice(r.t.Context(), s.Dir, s.Identity, s.Device, comp.name); err != nil {
		r.t.Fatal(err)
	}
	comp.device = s.Device.ID()
	return comp
}

// ppNewShortRoot creates a throwaway directory under /tmp with a short prefix
// so engine unix sockets under CODEAF_HOME stay well below the 103-byte limit,
// and stops any engine daemons spawned under the root at test cleanup.
func ppNewShortRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "pp-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Cleanup(func() {
		pids := engineDaemonsUnder(root)
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
		deadline := time.Now().Add(10 * time.Second)
		for _, pid := range pids {
			for !terminalProcessExited(pid) && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if !terminalProcessExited(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	return root
}

// ppWriteConfig writes the keyless model configuration for one throwaway home.
func ppWriteConfig(t *testing.T, homeDir string) {
	t.Helper()
	rows := map[string]any{
		"api_key":            "stub-key",
		"model.talk":         automationStubModel,
		"memory.enabled":     false,
		"tools.approvalMode": "allow",
		"setup_seen_at":      time.Now().UTC().Format(time.RFC3339),
		config.KeyIcons:      config.IconsPlain,
	}
	for _, key := range []string{
		config.KeyTaskModel,
		config.KeyTierWorkerModel,
		config.KeyTierLowModel,
		config.KeyTierHighModel,
		config.KeyTierReflexModel,
		config.KeyTierMastermindModel,
	} {
		rows[key] = automationStubModel
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (r *ppRig) envFor(homeDir string) []string {
	return append(os.Environ(),
		config.APIKeyEnv+"=stub-key",
		"CODEAF_BASE_URL="+r.model.server.URL+"/api/v1",
		"CODEAF_PROFILE_DIR=",
		"CODEAF_HOME="+homeDir,
		syncsetup.URLVar+"="+r.relayURL,
		"CODEAF_SYNC_INTERVAL_MS=100",
		"CODEAF_TELEMETRY=off",
		"DO_NOT_TRACK=1",
		"CODEAF_NO_UPDATE_CHECK=1",
		"CODEAF_NO_AUTOMATIONS=1",
		"CODEAF_REMOTE_STUN=off",
	)
}

func (r *ppRig) runCLI(comp ppComputer, args ...string) (string, error) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(r.t.Context(), 30*time.Second)
	defer cancel()
	cmd := guardedCommand(r.t, ctx, comp.home, r.envFor(comp.home), binary(r.t), args...)
	cmd.Dir = comp.work
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *ppRig) pairPhoneAt(comp ppComputer) clientproto.ClientGrant {
	r.t.Helper()
	ctx := r.t.Context()
	link, err := r.phone.StartLink(ctx, r.relayURL)
	if err != nil {
		r.t.Fatalf("StartLink: %v", err)
	}
	out, err := r.runCLI(comp, "pair", "approve", link.Token, "--check", link.CheckDigits)
	if err != nil {
		r.t.Fatalf("pair approve on %s failed: %v\n%s", comp.name, err, out)
	}
	if !strings.Contains(out, pair.ApprovedPhoneLine(r.phone.Name())) {
		r.t.Fatalf("pair approve output on %s = %q, want %q", comp.name, out, pair.ApprovedPhoneLine(r.phone.Name()))
	}
	grant, err := r.phone.AwaitGrant(ctx, link)
	if err != nil {
		r.t.Fatalf("AwaitGrant: %v", err)
	}
	return grant
}

type ppSyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *ppSyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *ppSyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type ppServeProc struct {
	t      *testing.T
	comp   ppComputer
	cmd    *exec.Cmd
	cancel context.CancelFunc
	out    *ppSyncBuffer
	done   chan error
	once   sync.Once
	// began is when the process was started, and ready how long it took to print
	// its start line.
	began time.Time
	ready time.Duration
}

func (r *ppRig) startPhoneServe(comp ppComputer) *ppServeProc {
	r.t.Helper()
	ctx, cancel := context.WithCancel(r.t.Context())
	out := &ppSyncBuffer{}
	cmd := guardedCommand(r.t, ctx, comp.home, r.envFor(comp.home), binary(r.t), "phone", "serve")
	cmd.Dir = comp.work
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		cancel()
		r.t.Fatalf("start phone serve on %s: %v", comp.name, err)
	}
	proc := &ppServeProc{
		t:      r.t,
		comp:   comp,
		cmd:    cmd,
		cancel: cancel,
		out:    out,
		done:   make(chan error, 1),
	}
	go func() {
		proc.done <- cmd.Wait()
	}()
	r.t.Cleanup(proc.stop)

	began := time.Now()
	proc.began = began
	deadline := began.Add(ppServeStartWithin)
	for {
		if strings.Contains(out.String(), ppPhoneServeStartLine) {
			proc.ready = time.Since(began)
			r.t.Logf("phone serve on %s said it was serving after %s", comp.name, proc.ready.Round(time.Millisecond))
			return proc
		}
		select {
		case err := <-proc.done:
			r.t.Fatalf("phone serve on %s exited before printing start line (%v):\n%s", comp.name, err, out.String())
		default:
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("waited %s for phone serve on %s to print %q; output:\n%s",
				ppServeStartWithin, comp.name, ppPhoneServeStartLine, out.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (p *ppServeProc) stop() {
	p.once.Do(func() {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-p.done:
			p.cancel()
			return
		case <-time.After(5 * time.Second):
		}
		p.cancel()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-p.done
	})
}

// ── polling helpers ──────────────────────────────────────────────────────────

func ppWaitUntil(t *testing.T, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", within, what)
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func (r *ppRig) waitForServingWatch(deviceID string, within time.Duration) {
	r.t.Helper()
	ppWaitUntil(r.t, within, fmt.Sprintf("device %s to open GET /v1/dir/watch with serving=1", deviceID), func() bool {
		for _, c := range r.recorder.snapshot() {
			if c.device == deviceID && c.method == http.MethodGet && c.path == "/v1/dir/watch" && ppQueryHasServing(c.rawQuery) {
				return true
			}
		}
		return false
	})
}

func (r *ppRig) waitForDirListCount(deviceID string, want int, within time.Duration) {
	r.t.Helper()
	ppWaitUntil(r.t, within, fmt.Sprintf("device %s to send at least %d GET /v1/dir/list requests", deviceID, want), func() bool {
		n := 0
		for _, c := range r.recorder.snapshot() {
			if c.device == deviceID && c.method == http.MethodGet && c.path == "/v1/dir/list" {
				n++
			}
		}
		return n >= want
	})
}

func (r *ppRig) waitForComputers(within time.Duration, wantDevices ...string) []phonetest.Computer {
	r.t.Helper()
	var last []phonetest.Computer
	var lastErr error
	deadline := time.Now().Add(within)
	for {
		list, err := r.phone.Computers(r.t.Context())
		if err == nil {
			last = list
			have := map[string]bool{}
			for _, c := range list {
				if c.Serving {
					have[c.Device] = true
				}
			}
			allFound := true
			for _, want := range wantDevices {
				if !have[want] {
					allFound = false
					break
				}
			}
			if allFound {
				return list
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out after %s waiting for phone.Computers to list serving %v (last=%+v err=%v)",
				within, wantDevices, last, lastErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (r *ppRig) waitForCard(deviceID string, within time.Duration) clientproto.Card {
	r.t.Helper()
	var lastErr error
	deadline := time.Now().Add(within)
	for {
		card, err := r.phone.Card(r.t.Context(), deviceID)
		if err == nil {
			return card
		}
		if errors.Is(err, phonetest.ErrCardReplay) {
			if known, ok := r.phone.KnownCard(deviceID); ok {
				return known
			}
		}
		lastErr = err
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out after %s waiting for verified Card(%s): last err=%v", within, deviceID, lastErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (r *ppRig) waitOutcome(deviceID string, ref phonetest.CallRef, within time.Duration) clientproto.Outcome {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(r.t.Context(), within)
	defer cancel()
	out, err := r.phone.WaitOutcome(ctx, deviceID, ref, func() {
		time.Sleep(50 * time.Millisecond)
	})
	if err != nil {
		r.t.Fatalf("WaitOutcome(computer=%s, call=#%d %s) timed out after %s: %v",
			deviceID, ref.Number, ref.Method, within, err)
	}
	return out
}

func (r *ppRig) readLatestEntries(deviceID string) ([]ppEntriesEnvelope, bool, error) {
	plain, err := r.phone.Read(r.t.Context(), deviceID, "agents.entries")
	if err != nil {
		if errors.Is(err, phonetest.ErrSlotReplay) {
			r.entriesMu.Lock()
			cached, ok := r.lastEntries[deviceID]
			r.entriesMu.Unlock()
			return cached, ok, nil
		}
		if errors.Is(err, phonetest.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var envs []ppEntriesEnvelope
	if err := json.Unmarshal(plain.Payload, &envs); err != nil {
		return nil, false, fmt.Errorf("decode agents.entries.%s payload: %w", deviceID, err)
	}
	r.entriesMu.Lock()
	r.lastEntries[deviceID] = envs
	r.entriesMu.Unlock()
	return envs, true, nil
}

func (r *ppRig) waitForEntries(deviceID string, within time.Duration, what string, match func([]ppEntriesEnvelope) bool) []ppEntriesEnvelope {
	r.t.Helper()
	var last []ppEntriesEnvelope
	deadline := time.Now().Add(within)
	for {
		envs, ok, err := r.readLatestEntries(deviceID)
		if err != nil {
			r.t.Fatalf("Read(agents.entries.%s) while waiting for %s: %v", deviceID, what, err)
		}
		if ok {
			last = envs
			if match(envs) {
				return envs
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out after %s waiting for %s in agents.entries.%s (last=%+v)", within, what, deviceID, last)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type ppListEnvelope struct {
	Writer string               `json:"writer"`
	At     time.Time            `json:"at"`
	Agent  string               `json:"agent"`
	Result appwire.AgentsResult `json:"result"`
}

func ppDescribeList(envs []ppListEnvelope) string {
	var parts []string
	for _, env := range envs {
		var ids []string
		for _, a := range env.Result.Agents {
			ids = append(ids, a.ID+"("+a.State+")")
		}
		parts = append(parts, fmt.Sprintf("{agent:%s rows:%v}", env.Agent, ids))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// waitForList waits until the latest `agents.list` slot of a computer has an
// envelope for every wanted chat, each carrying that chat's row.
func (r *ppRig) waitForList(deviceID string, within time.Duration, what string, chats ...string) []ppListEnvelope {
	r.t.Helper()
	var last []ppListEnvelope
	deadline := time.Now().Add(within)
	for {
		plain, err := r.phone.Read(r.t.Context(), deviceID, "agents.list")
		switch {
		case err == nil:
			var envs []ppListEnvelope
			if jsonErr := json.Unmarshal(plain.Payload, &envs); jsonErr != nil {
				r.t.Fatalf("decode agents.list.%s: %v", deviceID, jsonErr)
			}
			last = envs
			have := map[string]bool{}
			for _, env := range envs {
				for _, a := range env.Result.Agents {
					if a.ID == env.Agent {
						have[a.ID] = true
					}
				}
			}
			all := true
			for _, want := range chats {
				all = all && have[want]
			}
			if all {
				return envs
			}
		case errors.Is(err, phonetest.ErrSlotReplay), errors.Is(err, phonetest.ErrNotFound):
		default:
			r.t.Fatalf("Read(agents.list.%s) while waiting for %s: %v", deviceID, what, err)
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out after %s waiting for %s in agents.list.%s (last=%s)", within, what, deviceID, ppDescribeList(last))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (r *ppRig) waitForAnyEntriesSlot(deviceID string, within time.Duration) []ppEntriesEnvelope {
	r.t.Helper()
	return r.waitForEntries(deviceID, within, "agents.entries slot published", func([]ppEntriesEnvelope) bool {
		return true
	})
}

func (r *ppRig) waitForNoNewMatchingRequests(within, quietWindow time.Duration, what string, match func(ppRecordedCall) bool) {
	r.t.Helper()
	deadline := time.Now().Add(within)
	lastCount := r.recorder.countMatching(match)
	quietSince := time.Now()
	for {
		nowCount := r.recorder.countMatching(match)
		if nowCount != lastCount {
			lastCount = nowCount
			quietSince = time.Now()
		} else if time.Since(quietSince) >= quietWindow {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out after %s waiting for %s to stay quiet for %s (count=%d, calls=%s)",
				within, what, quietWindow, nowCount, r.recorder.summary())
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func (r *ppRig) assertQuietFor(window time.Duration, what string, match func(ppRecordedCall) bool) {
	r.t.Helper()
	before := r.recorder.countMatching(match)
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		if now := r.recorder.countMatching(match); now != before {
			r.t.Fatalf("recorder saw %d new requests for %s during %s quiet window (calls: %s)",
				now-before, what, window, r.recorder.summary())
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func ppEnvelopesHaveChat(envs []ppEntriesEnvelope, chatID string) bool {
	for _, env := range envs {
		if env.Agent == chatID {
			return true
		}
	}
	return false
}

func ppChatExistsInHome(homeDir, chatID string) bool {
	matches, _ := filepath.Glob(filepath.Join(homeDir, "v3", "projects", "*", chatID))
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func ppCountChatsInHome(homeDir string) int {
	matches, _ := filepath.Glob(filepath.Join(homeDir, "v3", "projects", "*", "*"))
	n := 0
	for _, m := range matches {
		base := filepath.Base(m)
		if strings.HasPrefix(base, ".") {
			continue
		}
		if info, err := os.Stat(m); err == nil && info.IsDir() {
			n++
		}
	}
	return n
}

func ppTombstoneHas(homeDir, clientID string) bool {
	raw, err := os.ReadFile(filepath.Join(homeDir, "clients", "tombstones.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), clientID)
}

func ppQueryHasServing(rawQuery string) bool {
	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		return false
	}
	return vals.Get(clientproto.ServingParam) == "1"
}

func ppIsForbiddenSoloPath(p string) bool {
	for _, prefix := range []string{
		"/v1/dir/cells",
		"/v1/dir/vault",
		"/v1/store",
		"/v1/cluster",
		"/v1/raft",
		"/v1/teams",
	} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return strings.HasSuffix(p, "/acquire")
}

// ppIsAllowedSoloApproveRequest is what `codeaf pair approve` on a solo computer may send for
// a phone: the phone's enrolment and registration, the approval of its request, and the
// reads and own-device write the approval is built from. It deliberately has no storage,
// cell, vault, lease or cluster route.
func ppIsAllowedSoloApproveRequest(c ppRecordedCall, phoneID string) bool {
	switch {
	case c.method == http.MethodPut && c.path == "/v1/clients/"+phoneID+"/enroll":
		return true
	case c.method == http.MethodPut && c.path == "/v1/clients/"+phoneID:
		return true
	case c.method == http.MethodPost && strings.HasPrefix(c.path, "/v1/dir/requests/") && strings.HasSuffix(c.path, "/approve"):
		return true
	case c.method == http.MethodGet && c.path == "/v1/dir/requests":
		return true
	case c.method == http.MethodGet && strings.HasPrefix(c.path, "/v1/dir/requests/"):
		return true
	}
	return false
}

// ppIsAllowedSoloRevokeRequest is what `codeaf devices revoke <phone>` on a solo computer may
// send: the device list and presence reads that find the phone by name, and the revoke
// of that one phone.
func ppIsAllowedSoloRevokeRequest(c ppRecordedCall, phoneID string) bool {
	switch {
	case c.method == http.MethodGet && (c.path == "/v1/dir/list" || c.path == "/v1/dir/presence"):
		return true
	case c.method == http.MethodPost && c.path == "/v1/dir/devices/"+phoneID+"/revoke":
		return true
	}
	return false
}

// ppIsAllowedSoloServingRequest checks whether a request sent by solo computer
// C is one of the exact routes listed in contract P13:
//   - GET /v1/dir/list
//   - GET /v1/dir/watch with serving=1
//   - PUT /v1/dir/devices/<C's own id>
//   - GET /v1/clients
//   - under /v1/clients/<id>/: queue, queue/<n>/claim, queue/<n>, slots/<name>
func ppIsAllowedSoloServingRequest(c ppRecordedCall, selfDevice, phoneID string) bool {
	switch {
	case c.method == http.MethodGet && c.path == "/v1/dir/list":
		return true
	case c.method == http.MethodGet && c.path == "/v1/dir/watch":
		return ppQueryHasServing(c.rawQuery)
	case c.method == http.MethodPut && c.path == "/v1/dir/devices/"+selfDevice:
		return true
	case c.method == http.MethodGet && c.path == "/v1/clients":
		return true
	}
	prefix := "/v1/clients/" + phoneID + "/"
	rest, ok := strings.CutPrefix(c.path, prefix)
	if !ok {
		return false
	}
	if rest == "queue" {
		return c.method == http.MethodGet
	}
	if afterQueue, ok := strings.CutPrefix(rest, "queue/"); ok {
		if num, isClaim := strings.CutSuffix(afterQueue, "/claim"); isClaim {
			return c.method == http.MethodPost && ppPositiveDigits(num)
		}
		return c.method == http.MethodDelete && ppPositiveDigits(afterQueue)
	}
	if slotName, ok := strings.CutPrefix(rest, "slots/"); ok {
		return slotName != "" && !strings.Contains(slotName, "/") && (c.method == http.MethodPut || c.method == http.MethodGet)
	}
	return false
}

func ppPositiveDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != "0"
}

// ── the recording relay proxy ────────────────────────────────────────────────

type ppRecordedCall struct {
	at         time.Time
	method     string
	path       string
	rawQuery   string
	device     string
	identityID string
	signerPub  string
	signed     bool
	sigValid   bool
}

func (c ppRecordedCall) describe() string {
	uri := c.path
	if c.rawQuery != "" {
		uri += "?" + c.rawQuery
	}
	if c.device != "" {
		return fmt.Sprintf("%s %s (device=%s)", c.method, uri, c.device)
	}
	return fmt.Sprintf("%s %s (open)", c.method, uri)
}

type ppRelayRecorder struct {
	mu    sync.Mutex
	calls []ppRecordedCall
}

func startPersonalPhoneRelay(t *testing.T, root string, relayArgs ...string) (string, *ppRelayRecorder) {
	t.Helper()
	upstreamURL := startRelayAt(t, filepath.Join(root, "relay"), relayArgs...)
	upstream, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	rec := &ppRelayRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		call := ppRecordedCall{
			at:       time.Now(),
			method:   r.Method,
			path:     r.URL.Path,
			rawQuery: r.URL.RawQuery,
		}
		if rawCertB64 := r.Header.Get(reqsign.HeaderCert); rawCertB64 != "" || r.Header.Get(reqsign.HeaderSig) != "" {
			call.signed = true
			if rawCert, err := base64.RawURLEncoding.DecodeString(rawCertB64); err == nil {
				var cert identity.Cert
				if json.Unmarshal(rawCert, &cert) == nil {
					call.device = cert.DeviceID()
					call.signerPub = cert.Device
				}
			}
			if idID, devID, err := reqsign.Verify(r, body, time.Now()); err == nil {
				call.sigValid = true
				call.identityID = idID
				call.device = devID
			}
		}
		rec.mu.Lock()
		rec.calls = append(rec.calls, call)
		rec.mu.Unlock()
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL, rec
}

func (r *ppRelayRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

func (r *ppRelayRecorder) snapshot() []ppRecordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

func (r *ppRelayRecorder) callsFrom(deviceID string) []ppRecordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ppRecordedCall
	for _, c := range r.calls {
		if c.device == deviceID {
			out = append(out, c)
		}
	}
	return out
}

func (r *ppRelayRecorder) countMatching(match func(ppRecordedCall) bool) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if match(c) {
			n++
		}
	}
	return n
}

func (r *ppRelayRecorder) summary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var parts []string
	for _, c := range r.calls {
		parts = append(parts, c.describe())
	}
	return strings.Join(parts, "; ")
}

func (r *ppRelayRecorder) summaryFor(deviceID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var parts []string
	for _, c := range r.calls {
		if c.device == deviceID {
			parts = append(parts, c.describe())
		}
	}
	return strings.Join(parts, "; ")
}
