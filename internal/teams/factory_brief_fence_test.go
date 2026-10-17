package teams

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"
)

func TestFactoryStageBriefFenceIsUnderTheStoreLock(t *testing.T) {
	dir := briefTeam(t)
	must(t, Update(dir, func(f *File) error { f.Teams[0].Members[1].FactoryStage = true; return nil }))
	before, err := os.ReadFile(Path(dir))
	must(t, err)
	trafficBefore, err := ReadTraffic(dir, "t1", "", 100)
	must(t, err)
	_, err = revise(dir, "krev", FromYou, "", "change stage brief", "", time.Time{})
	if !errors.Is(err, ErrBriefFactoryStage) {
		t.Fatalf("stage edit = %v", err)
	}
	after, err := os.ReadFile(Path(dir))
	must(t, err)
	if !bytes.Equal(before, after) {
		t.Fatal("refused edit rewrote team store")
	}
	trafficAfter, err := ReadTraffic(dir, "t1", "", 100)
	must(t, err)
	if len(trafficAfter) != len(trafficBefore) {
		t.Fatal("refused edit appended traffic")
	}
	if len(briefsOf(t, dir, "krev").Briefs) != 0 {
		t.Fatal("refused edit persisted a revision")
	}
}
