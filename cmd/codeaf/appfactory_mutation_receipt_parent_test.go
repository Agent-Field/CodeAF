package main

import (
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestLinkRevokeRequiresACompletedHTTPReceipt(t *testing.T) {
	for _, status := range []int{201, 202, 206, 299} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newLinkFactory(t)
			f.rows = []api.AccessLink{{ID: "tok_receipt", Status: "open", MaxUses: 1}}
			f.del = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }
			out, err := f.revoke("tok_receipt")
			linkWantCode(t, err, appwire.CodeUncertain)
			if out.Revoked || f.deletes() != 1 {
				t.Fatal("incomplete reply claimed revocation or repeated it")
			}
		})
	}
}

func TestAdminRequiresACompletedHTTPReceiptAndKeepsTheCredential(t *testing.T) {
	for _, action := range []string{"sign_out", "revoke"} {
		for _, status := range []int{201, 202, 206, 299} {
			t.Run(action+http.StatusText(status), func(t *testing.T) {
				b, _, conn := adminStub(t, meBody("session_receipt", true), func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
				out, err := admin(b, action, conn.ID)
				if err == nil || wireCodeOf(t, err) != appwire.CodeUncertain {
					t.Fatal("incomplete receipt was treated as completed")
				}
				if out.SignedOut || b.factoryHub().tokenFor(conn.ID) != adminHeld {
					t.Fatal("unconfirmed mutation dropped the held credential")
				}
			})
		}
	}
}
