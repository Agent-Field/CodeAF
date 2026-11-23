package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestCheckoutsRoundTripAndFullNameLookup(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if m, err := st.Checkouts(); err != nil || len(m) != 0 {
		t.Fatalf("empty store: %v %v", m, err)
	}
	if err := st.SetRepos([]string{"acme/ledger"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCheckout("acme/ledger", "/src/ledger"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCheckout("ledger", "/src/ledger"); err != nil {
		t.Fatal(err)
	}
	m, _ := st.Checkouts()
	if len(m) != 1 || m["acme/ledger"] != "/src/ledger" {
		t.Fatalf("got %v", m)
	}
	for _, name := range []string{"ledger", "acme/ledger"} {
		if got := st.CheckoutDir(name); got != "/src/ledger" {
			t.Fatalf("%s -> %q", name, got)
		}
	}
	if st.CheckoutDir("other") != "" {
		t.Fatal("unknown repo answered a folder")
	}
	if st.SetCheckout("", "/x") == nil {
		t.Fatal("empty repo accepted")
	}
}

func TestCheckoutIdentityAndStoredJobMigration(t *testing.T) {
	for _, key := range []string{"local-alias", "ledger", "acme/ledger"} {
		t.Run(key, func(t *testing.T) {
			st, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			when := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
			alias := key
			if key == "acme/ledger" {
				alias = "local-alias"
			}
			it, err := st.Create(factory.Item{Repo: alias, Places: []string{alias}, Title: "existing", State: factory.StateQueued, Changed: when})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for _, args := range [][]string{{"init", dir}, {"-C", dir, "remote", "add", "origin", "git@github.com:acme/ledger.git"}} {
				if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
					t.Fatalf("git: %v %s", err, out)
				}
			}
			if err := st.SetCheckout(key, dir); err != nil {
				t.Fatal(err)
			}
			if err := st.SetRepos([]string{"acme/ledger", "other/ledger"}); err != nil {
				t.Fatal(err)
			}
			// A full key does not need origin, but the old alias does. Keep the
			// legacy alias in the file to exercise migration after an upgrade.
			if key == "acme/ledger" {
				if err := st.SetCheckout("local-alias", dir); err != nil {
					t.Fatal(err)
				}
			}
			checkouts, err := st.Checkouts()
			if err != nil || !reflect.DeepEqual(checkouts, map[string]string{"acme/ledger": dir}) {
				t.Fatalf("checkouts = %v, %v", checkouts, err)
			}
			items, err := st.List()
			if err != nil || len(items) != 1 || items[0].Repo != "acme/ledger" || !reflect.DeepEqual(items[0].Places, []string{"acme/ledger"}) || !items[0].Changed.Equal(when) {
				t.Fatalf("loaded = %+v, %v", items, err)
			}
			d, err := st.read(st.ItemPath(it.ID))
			if err != nil || d.Item.Repo != "acme/ledger" || d.Revision != 2 {
				t.Fatalf("stored = %+v, %v", d, err)
			}
			if _, err := st.Get(it.ID); err != nil {
				t.Fatal(err)
			}
			if rev, _ := st.Revision(it.ID); rev != 2 {
				t.Fatalf("migration repeated: revision %d", rev)
			}
			made, err := st.Create(factory.Item{Repo: alias, Title: "new"})
			if err != nil || made.Repo != "acme/ledger" {
				t.Fatalf("new = %+v, %v", made, err)
			}
		})
	}
}

func TestCheckoutConfigFallbackDoesNotCrossOwners(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/ledger", "other/ledger"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCheckout("acme/ledger", "/acme"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCheckout("other/ledger", "/other"); err != nil {
		t.Fatal(err)
	}
	if st.CheckoutDir("ledger") != "" || st.CheckoutDir("acme/ledger") != "/acme" || st.CheckoutDir("other/ledger") != "/other" || st.CheckoutDir("third/ledger") != "" {
		t.Fatal("checkout lookup crossed owners or accepted an ambiguous alias")
	}
	if err := os.WriteFile(filepath.Join(st.Root(), "checkouts.json"), []byte(`{"schema":1,"checkouts":{"ledger":"/legacy"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if dirs, err := st.Checkouts(); err != nil || len(dirs) != 0 {
		t.Fatalf("ambiguous checkouts = %v, %v", dirs, err)
	}
	if err := st.SetRepos(nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCheckout("acme/ledger", "/acme"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCheckout("other/ledger", "/other"); err != nil {
		t.Fatal(err)
	}
	if st.CheckoutDir("ledger") != "" {
		t.Fatal("ambiguous checkout records selected an owner without origin or config")
	}
}

func TestGetMigratesShortRepositoryFromWatchedConfig(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.Create(factory.Item{Repo: "ledger", Title: "old job", State: factory.StateRunning})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/ledger"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCheckout("ledger", "/missing-clone"); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.Get(job.ID)
	if err != nil || loaded.Repo != "acme/ledger" || loaded.State != job.State || !loaded.Changed.Equal(job.Changed) {
		t.Fatalf("Get = %+v, %v", loaded, err)
	}
	if dirs, err := st.Checkouts(); err != nil || !reflect.DeepEqual(dirs, map[string]string{"acme/ledger": "/missing-clone"}) {
		t.Fatalf("config fallback = %v, %v", dirs, err)
	}
}

func TestGitHubCheckoutRemoteForms(t *testing.T) {
	for _, remote := range []string{"https://github.com/Acme/Ledger.git", "git@github.com:Acme/Ledger.git", "ssh://git@github.com/Acme/Ledger.git"} {
		if got := githubRemoteName(remote); got != "acme/ledger" {
			t.Errorf("%s = %q", remote, got)
		}
	}
	for _, remote := range []string{"https://github.com.example.com/acme/ledger.git", "https://elsewhere/acme/ledger.git", "/local/repo"} {
		if got := githubRemoteName(remote); got != "" {
			t.Errorf("foreign remote %s = %q", remote, got)
		}
	}
}
