package appwire

import (
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"testing"
)

func TestFactoryMeNeedsGitHubAttestationNotAnOpaqueCredentialID(t *testing.T) {
	for _, id := range []string{"tok_parentScriptGrant_8fa12b", "session_parentLegacyDashboard_b081ac"} {
		got := NewFactoryMe(api.Me{SessionID: id, Profile: api.Profile{Handle: "host-owner", Name: "Host Owner"}, Operator: true})
		if got.GitHubSignedIn {
			t.Fatal("an opaque credential ID without GitHub attestation was presented as a person's GitHub sign-in")
		}
	}
}

func TestFactoryMeDistinguishesVerifiedIdentityFromRevocableSession(t *testing.T) {
	for _, tc := range []struct {
		id       string
		attested bool
		can      bool
	}{
		{"tok_verifiedLegacyBoundGrant", true, false},
		{"session_legacyCookie", false, false},
		{"session_verifiedCaller", true, true},
		{"session_", true, false},
		{"session_bad/path", true, false},
		{"", true, false},
	} {
		got := NewFactoryMe(api.Me{SessionID: tc.id, GitHubSignedIn: tc.attested})
		if got.GitHubSignedIn != tc.attested || got.CanSignOut != tc.can {
			t.Fatal("identity and sign-out capability were conflated")
		}
	}
}

// A password person may sign out, and is not said to have signed in with
// GitHub.
func TestAPasswordPersonCanSignOutWithoutAGitHubSignIn(t *testing.T) {
	got := NewFactoryMe(api.Me{SessionID: "session_x", SignInKind: "password"})
	if got.GitHubSignedIn || !got.CanSignOut || got.SignInKind != "password" {
		t.Fatalf("password person = %+v", got)
	}
}
