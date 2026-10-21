package cellstore

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/lawcheck"
)

var _ = lawcheck.UpdateFlag()

// The WAL record is unexported, so its golden rows live here; the exported
// schemas are in internal/lawcheck.
func TestGoldenWALRecords(t *testing.T) {
	intent := Intent{V: 1, Tool: "bash", ArgsHash: "d1f0", Started: 1759049981200, SideEffect: "external"}
	done := Executed{
		Call:     Call{Tool: "bash", ArgsHash: "d1f0", Started: 1759049981200, Ended: 1759049981940, StdoutHash: "0a1b", StderrHash: "e3b0", SideEffect: "external"},
		Services: []ServiceRec{{PID: 4711, Argv: []string{"postgres"}, Ports: []int{5432}}},
		Exact:    false, Trigger: Setup, Changed: []string{"var/pg"}, Stdout: []byte("ok"), Stderr: []byte("warn"),
	}
	intentV2 := Intent{V: 2, Tool: "bash", ArgsHash: "d1f0", Started: 1759049981200, SideEffect: "local", Brief: "sleep 150"}
	group := GroupRec{PGID: 4711, Start: 377568552, Owner: 4700, OwnerStart: 377567646}
	for name, rec := range map[string]record{
		"wal.intent.v2": {V: 2, Op: opIntent, Intent: intentV2},
		"wal.group":     {V: 2, Op: opGroup, Intent: intentV2, Group: &group},
		"wal.intent":    {V: 1, Op: opIntent, Intent: intent},
		"wal.done":      {V: 1, Op: opDone, Intent: intent, Done: &done},
		"wal.resolved":  {V: 1, Op: opResolved, Intent: intent},
	} {
		t.Run(name, func(t *testing.T) {
			lawcheck.Golden(t, filepath.Join("testdata", name+".golden.json"), rec)
		})
	}
}
