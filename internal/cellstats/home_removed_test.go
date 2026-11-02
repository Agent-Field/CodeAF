package cellstats

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

// SPEC-3 item 2: late settlement cannot recreate a removed or replaced home.
func TestFixRound2StatsSettleRefusesRemovedHome(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "replaced"}[replaced], func(t *testing.T) {
			h := filepath.Join(t.TempDir(), "home")
			if err := os.Mkdir(h, 0700); err != nil {
				t.Fatal(err)
			}
			home.Witness(h)
			r := NewRecorder(h, "chat", MeterFunc(func() Counts { return Counts{Puts: 1} }))
			if err := os.Rename(h, h+".old"); err != nil {
				t.Fatal(err)
			}
			if replaced {
				if err := os.Mkdir(h, 0700); err != nil {
					t.Fatal(err)
				}
			}
			r.Settle()
			if r.Err() == nil {
				t.Fatal("settled into removed home")
			}
			if replaced {
				entries, err := os.ReadDir(h)
				if err != nil || len(entries) != 0 {
					t.Fatalf("replacement changed: %v %v", entries, err)
				}
			} else if _, err := os.Stat(h); !os.IsNotExist(err) {
				t.Fatalf("home recreated: %v", err)
			}
		})
	}
}
