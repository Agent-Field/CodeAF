package keys

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
)

func TestEqualStampFieldsConvergeRegardlessOfMergeDirection(t *testing.T) {
	aHome, bHome := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(aHome)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.Save(bHome, id); err != nil {
		t.Fatal(err)
	}
	a, err := Open(aHome)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(bHome)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1700000000, 0)
	for v, value := range map[*Vault]string{a: `["x"]`, b: `["y"]`} {
		if err := v.PutAt("home:keys:teams/alpha/order", Entry{Name: "alpha/order", Scope: "home:keys:teams", Value: value}, at); err != nil {
			t.Fatal(err)
		}
	}
	aRaw, err := a.Export()
	if err != nil {
		t.Fatal(err)
	}
	bRaw, err := b.Export()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Merge(bRaw); err != nil {
		t.Fatal(err)
	}
	if err := b.Merge(aRaw); err != nil {
		t.Fatal(err)
	}
	left, err := a.Get("home:keys:teams/alpha/order")
	if err != nil {
		t.Fatal(err)
	}
	right, err := b.Get("home:keys:teams/alpha/order")
	if err != nil || left != right {
		t.Fatalf("equal clock fields diverged: %+v %+v %v", left, right, err)
	}
	// Embedded separators must not make two distinct records compare equal.
	leftRecord := record{Entry: Entry{Scope: "home:keys:teams", Name: "b", Value: "c\x00d"}, Updated: at.UnixMilli()}
	rightRecord := record{Entry: Entry{Scope: "home:keys:teams", Name: "b\x00c", Value: "d"}, Updated: at.UnixMilli()}
	doc := &vaultDoc{}
	if supersedes(leftRecord, doc, rightRecord, doc) == supersedes(rightRecord, doc, leftRecord, doc) {
		t.Fatal("distinct equal-time records have no deterministic winner")
	}

}

func TestExistingScopesKeepTheirEqualStampMergeAnswer(t *testing.T) {
	doc := &vaultDoc{}
	for _, scope := range []string{"home:keys:preferences", "home:keys:provider-keys", "home:memories", "credentials", "project", "home:keys:teams-future"} {
		for _, deleted := range []bool{false, true} {
			a := record{Entry: Entry{Scope: scope, Name: "key", Value: "z"}, Updated: 100, Deleted: deleted}
			b := record{Entry: Entry{Scope: scope, Name: "key", Value: "a"}, Updated: 100}
			if got, want := supersedes(a, doc, b, doc), deleted; got != want {
				t.Fatalf("scope %s: got %v want %v", scope, got, want)
			}
		}
	}
}
