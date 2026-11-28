package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/fabricclient"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

type recordingPhoneUnpairer struct {
	unpaired []string
}

func (r *recordingPhoneUnpairer) Unpair(_ context.Context, id string) error {
	r.unpaired = append(r.unpaired, id)
	return nil
}

type testPhoneDir struct {
	directory.Client
	phoneID string
}

func (d testPhoneDir) List(ctx context.Context) (directory.Listing, error) {
	l, err := d.Client.List(ctx)
	if err != nil {
		return l, err
	}
	if dev, ok := l.Devices[d.phoneID]; ok {
		dev.Kind = clientproto.ClientLinkClass
		l.Devices[d.phoneID] = dev
	}
	return l, nil
}

// U2: `codeaf devices` lists a phone after computers with `phone` and
// `starts chats, answers`; `permit` on a phone says `this is set on the phone`
// and changes nothing; `rename` works; `revoke` removes the phone with the
// phone removal wording and notifies an in-process runner via Unpair.
func TestU2DevicesCLIPhoneListPermitRenameAndRevoke(t *testing.T) {
	celltest.Available(t)
	homeDir := t.TempDir()
	t.Setenv("CODEAF_HOME", homeDir)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")

	chats, rawDir := chatsWith(t, "laptop")
	chats.identity = "id_test"
	phoneID := "dev_cccccccccccccccccccccccccccccccc"
	cellKey := bytes.Repeat([]byte{7}, 32)
	sealedPhone, err := directory.SealName(directory.MetadataKey(cellKey), "alpha-phone")
	if err != nil {
		t.Fatal(err)
	}
	if err := rawDir.PutDevice(devicesCtx, phoneID, directory.Device{
		V:    1,
		Name: sealedPhone,
		Kind: clientproto.ClientLinkClass,
	}); err != nil {
		t.Fatal(err)
	}
	dir := testPhoneDir{Client: rawDir, phoneID: phoneID}
	chats.dir = dir
	unpairer := &recordingPhoneUnpairer{}
	chats.unpair = unpairer
	kinds := []deviceKind{chats}

	// 1. Listing shows phone with "phone" and "starts chats, answers", sorted after computers.
	printed := listed(t, kinds...)
	for _, want := range []string{"desk", "laptop", "alpha-phone", "phone", "starts chats, answers"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("codeaf devices output lacked %q:\n%s", want, printed)
		}
	}
	deskAt := strings.Index(printed, "desk")
	laptopAt := strings.Index(printed, "laptop")
	phoneAt := strings.Index(printed, "alpha-phone")
	if !(deskAt < laptopAt && laptopAt < phoneAt) {
		t.Fatalf("alpha-phone did not sort after computers (desk=%d laptop=%d phone=%d):\n%s", deskAt, laptopAt, phoneAt, printed)
	}

	// 2. `codeaf devices permit` on a phone says "this is set on the phone" and changes nothing.
	said, err := captureStdout(t, func() error {
		return runDevicesPermit(kinds, []string{"alpha-phone", "--watch", "off", "--drive", "on"})
	})
	if err != nil {
		t.Fatalf("permit alpha-phone: %v", err)
	}
	if strings.TrimSpace(said) != "this is set on the phone" {
		t.Fatalf("permit alpha-phone = %q, want %q", strings.TrimSpace(said), "this is set on the phone")
	}
	if _, class := pair.Permits().For("id_test", phoneID); class == pair.ClassExplicit {
		t.Fatalf("permit on phone wrote to permit book")
	}

	// 3. `codeaf devices rename <name>` works.
	if _, err := identity.Ensure(homeDir); err != nil {
		t.Fatal(err)
	}
	renamedOut, err := captureStdout(t, func() error {
		return runDevicesRename([]string{"studio-desk"})
	})
	if err != nil {
		t.Fatalf("devices rename: %v", err)
	}
	if !strings.Contains(renamedOut, "studio-desk") || devname.Name(homeDir) != "studio-desk" {
		t.Fatalf("devices rename output=%q name=%q", renamedOut, devname.Name(homeDir))
	}

	// 4. `codeaf devices revoke <phone>` revokes at the directory, calls Unpair, and prints the phone removal sentence.
	revokedOut, err := captureStdout(t, func() error {
		return stopDevice(kinds, []string{"alpha-phone"})
	})
	if err != nil {
		t.Fatalf("devices revoke alpha-phone: %v", err)
	}
	wantRevoked := "alpha-phone was removed — it can no longer reach your chats."
	if strings.TrimSpace(revokedOut) != wantRevoked {
		t.Fatalf("devices revoke alpha-phone = %q, want %q", strings.TrimSpace(revokedOut), wantRevoked)
	}
	if len(unpairer.unpaired) != 1 || unpairer.unpaired[0] != phoneID {
		t.Fatalf("unpairer calls = %v, want [%s]", unpairer.unpaired, phoneID)
	}
	listing, err := dir.List(devicesCtx)
	if err != nil || !listing.Devices[phoneID].Revoked {
		t.Fatalf("phone not marked revoked in directory: %+v (%v)", listing.Devices[phoneID], err)
	}
}

func testPhoneRequest(t *testing.T, rig *pairRig, phoneName string) (token, code, check string, linkKey []byte, phoneSignPub ed25519.PublicKey) {
	t.Helper()
	var err error
	linkKey, err = pair.NewLinkKey()
	if err != nil {
		t.Fatal(err)
	}
	phoneSignPub, _, err = ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	phoneBoxPub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sealedName, err := pair.SealDeviceName(linkKey, phoneName)
	if err != nil {
		t.Fatal(err)
	}
	mac := clientproto.BindMAC(linkKey, phoneSignPub, phoneBoxPub[:])
	bind := clientproto.Bind(mac)
	check = clientproto.CheckDigits(mac)

	reqs := directory.NewLinkHTTP(rig.url, http.DefaultClient)
	opened, err := reqs.CreateRequest(context.Background(), directory.NewRequest{
		Pubkey:     base64.RawURLEncoding.EncodeToString(phoneSignPub),
		X25519:     base64.RawURLEncoding.EncodeToString(phoneBoxPub[:]),
		NameSealed: sealedName,
		Platform:   "ios",
		Class:      clientproto.ClientLinkClass,
		Bind:       base64.RawURLEncoding.EncodeToString(bind),
	})
	if err != nil {
		t.Fatal(err)
	}
	code = opened.Code
	token = (pair.LinkRef{Code: code, Key: linkKey}).Token()
	return token, code, check, linkKey, phoneSignPub
}

// U2: `codeaf pair approve <token> [--check <n>]` approves a phone and prints
// the complete line, refuses a wrong --check without enrolling or granting,
// and refuses a keyless 10-character code with ErrLinkKeyless.
func TestU2PairApprovePhoneCLICheckedAndKeyless(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")

	if !strings.Contains(pairUsage, "codeaf pair approve <link-or-token> [--check <n>]") ||
		!strings.Contains(approveUsage, "codeaf pair approve <link-or-token> [--check <n>]") ||
		!strings.Contains(usageForCommand("pair"), "codeaf pair approve <link-or-token> [--check <n>]") {
		t.Fatalf("pair usage lines did not include [--check <n>]:\npairUsage=%s\napproveUsage=%s\nusageForCommand=%s",
			pairUsage, approveUsage, usageForCommand("pair"))
	}

	rig := newPairRig(t)

	// 1. Keyless 10-character code prints ErrLinkKeyless.
	token, code, check, linkKey, phoneSignPub := testPhoneRequest(t, rig, "iPhone")
	_, err := rig.approveWith(t, []string{"approve", code}, "")
	if !errors.Is(err, pair.ErrLinkKeyless) || err.Error() != "that link has no key; copy the whole link from the phone" {
		t.Fatalf("keyless 10-char code err = %v, want %q", err, pair.ErrLinkKeyless)
	}

	// 2. Wrong --check refuses with ErrCheckNumberMismatch and writes nothing.
	wrongCheck := "0000"
	if check == wrongCheck {
		wrongCheck = "9999"
	}
	_, err = rig.approveWith(t, []string{"approve", token, "--check", wrongCheck}, "")
	if !errors.Is(err, pair.ErrCheckMismatch) || err.Error() != "that check number does not match. Nothing was approved." {
		t.Fatalf("wrong --check err = %v, want ErrCheckNumberMismatch", err)
	}

	// 3. Matching --check approves the phone and prints the complete line.
	out, err := rig.approveWith(t, []string{"approve", token, "--check", check}, "")
	if err != nil {
		t.Fatalf("approve --check %s: %v", check, err)
	}
	wantComplete := "iPhone is now paired as a phone. It can start chats and answer on every computer you have paired. Remove it in /devices."
	if !strings.Contains(out.String(), wantComplete) {
		t.Fatalf("approve output lacked %q:\n%s", wantComplete, out.String())
	}

	// Verify the grant at the relay opens under the link key and contains no identity secrets.
	decided, err := directory.NewLinkHTTP(rig.url, http.DefaultClient).GetRequest(context.Background(), code, 0)
	if err != nil || decided.State != directory.RequestApproved || decided.Grant == nil {
		t.Fatalf("decided request = %+v, err = %v", decided, err)
	}
	rawGrant, err := base64.RawURLEncoding.DecodeString(*decided.Grant)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := clientproto.OpenGrant(rawGrant, linkKey)
	if err != nil {
		t.Fatalf("OpenGrant: %v", err)
	}
	if err := grant.Check(clientproto.DeviceIDOf(phoneSignPub), code, phoneSignPub); err != nil {
		t.Fatalf("grant.Check: %v", err)
	}

	// 4. Interactive `codeaf pair approve <token>` (typing a\n) shows the phone prompt and approves.
	token2, _, check2, _, _ := testPhoneRequest(t, rig, "Pixel")
	out2, err := rig.approveWith(t, []string{"approve", token2}, "a\n")
	if err != nil {
		t.Fatalf("interactive phone approve: %v", err)
	}
	for _, want := range []string{
		"A phone wants to connect.",
		"It can start chats and answer on all your computers. It never gets your keys or your files.",
		"Check number " + check2 + ". Approve only if the phone shows the same number.",
		"Pixel is now paired as a phone. It can start chats and answer on every computer you have paired. Remove it in /devices.",
	} {
		if !strings.Contains(out2.String(), want) {
			t.Fatalf("interactive phone approve lacked %q:\n%s", want, out2.String())
		}
	}

	// 5. approvalsDoor lists both phones, and Revoke notifies unpairer.
	door := rig.approvalsAt(rig.homeA)
	unpairer := &recordingPhoneUnpairer{}
	door.unpair = unpairer
	rows, err := door.Devices(context.Background())
	if err != nil {
		t.Fatalf("door.Devices: %v", err)
	}
	var phoneRowID string
	for _, r := range rows {
		if r.Name == "iPhone" && r.Client && r.Kind == clientproto.ClientLinkClass {
			phoneRowID = r.ID
		}
	}
	if phoneRowID == "" {
		t.Fatalf("door.Devices did not mark iPhone as a client phone row: %+v", rows)
	}
	if err := door.Revoke(context.Background(), phoneRowID); err != nil {
		t.Fatalf("door.Revoke phone: %v", err)
	}
	if len(unpairer.unpaired) != 1 || unpairer.unpaired[0] != phoneRowID {
		t.Fatalf("door.Revoke did not call unpairer: %v", unpairer.unpaired)
	}
}

func TestU2PhoneCLIStringsContainNoForbiddenWords(t *testing.T) {
	stringsToCheck := []string{
		errNoPhoneServer.Error(),
		pair.PhoneWantsToConnectLine,
		pair.PhoneAccessLine,
		pair.PhoneCheckQuestion("1234"),
		pair.ApprovedPhoneLine("iPhone"),
		pair.PhoneWord,
		pair.PhoneAccessWord,
		pair.PhonePermitSetLine,
		pair.PhoneRevokedLine("iPhone"),
		pair.ErrLinkKeyless.Error(),
		pair.ErrBindMismatch.Error(),
		pair.ErrCheckNumberMismatch.Error(),
		pair.ErrSetupLinkShape.Error(),
		pair.ErrNoExecutorCard.Error(),
		pair.ErrNoFabricClient.Error(),
		fabricclient.ErrStale.Error(),
		fabricclient.ErrExists.Error(),
		fabricclient.ErrBadRequest.Error(),
		fabricclient.ErrUnauthorized.Error(),
		fabricclient.ErrRevoked.Error(),
		fabricclient.ErrNotEnrolled.Error(),
		fabricclient.ErrAlreadyDecided.Error(),
		fabricclient.ErrFull.Error(),
	}
	// "relay" is the repository's word for the server, so it is allowed here; internal/wordlaw
	// decides where it may appear. The machinery words stay banned.
	forbidden := []string{"auditor", "verdict", "verified", "refuted"}
	for _, s := range stringsToCheck {
		lower := strings.ToLower(s)
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Fatalf("person-facing string %q contains forbidden word %q", s, bad)
			}
		}
	}
}
