package lane

import (
	"fmt"
	"testing"
	"time"
)

func TestWorkloadEvidenceMergesAcrossProcessesAndSurvivesCompaction(t *testing.T) {
	path := sharedFile(t)
	first := newLedger()
	first.keepIn(newStore().at(path))
	first.NoteWorkload(Workload{Model: "test/model", Class: "tools", Hidden: 100, At: noon})
	second := newLedger()
	second.keepIn(newStore().at(path))
	second.NoteWorkload(Workload{Model: "test/model", Class: "tools", Hidden: 300, At: noon})
	first.reload()
	third := newLedger()
	third.keepIn(newStore().at(path))
	for _, reader := range []*ledger{first, third} {
		visible, hidden, known := reader.Workload("test/model", "tools", noon)
		if !known || visible != 0 || hidden != 200 {
			t.Fatalf("shared completed calls were lost or counted twice: %d/%d, known=%v", visible, hidden, known)
		}
	}
}

func TestWorkloadEvidenceAgesAndSeparatesDifferentWork(t *testing.T) {
	sharedFile(t)
	l := newLedger()
	l.NoteWorkload(Workload{Model: "test/model", Class: "tools", Hidden: 100, At: noon})
	l.NoteWorkload(Workload{Model: "test/model", Class: "tools", Hidden: 400, At: noon.Add(HalfLife)})
	_, hidden, known := l.Workload("test/model", "tools", noon.Add(HalfLife))
	if !known || hidden != 300 {
		t.Fatalf("recent work did not outweigh old work: %d, known=%v", hidden, known)
	}
	for _, pair := range [][2]string{{"test/other", "tools"}, {"test/model", "prose"}} {
		if _, _, known := l.Workload(pair[0], pair[1], noon); known {
			t.Fatalf("unrelated work inherited this forecast: %v", pair)
		}
	}
	if _, _, known := l.Workload("test/model", "tools", noon.Add(10*HalfLife)); known {
		t.Fatal("stale history displaced evidence in the current request")
	}
}

func TestWorkloadReplayIsOrderedAndBounded(t *testing.T) {
	sharedFile(t)
	l := newLedger()
	l.NoteWorkload(Workload{Model: "test/model", Class: "tools", Hidden: 400, At: noon.Add(HalfLife)})
	l.NoteWorkload(Workload{Model: "test/model", Class: "tools", Hidden: 100, At: noon})
	_, hidden, _ := l.Workload("test/model", "tools", noon.Add(HalfLife))
	if hidden != 300 {
		t.Fatalf("an older receipt moved the forecast backwards: %d", hidden)
	}
	l.foldWorkload(Workload{Model: "test/model", Class: "tools", Hidden: -100, At: noon})
	if _, hidden, _ := l.Workload("test/model", "tools", noon); hidden != 300 {
		t.Fatal("an invalid journal entry changed the forecast")
	}
	for n := range workloadLimit {
		l.NoteWorkload(Workload{Model: fmt.Sprintf("test/%d", n), Class: "tools", Hidden: 10,
			At: noon.Add(2*HalfLife + time.Duration(n)*time.Second)})
	}
	if len(l.workloads) != workloadLimit {
		t.Fatalf("retained %d classes, limit %d", len(l.workloads), workloadLimit)
	}
	if _, _, known := l.Workload("test/model", "tools", noon); known {
		t.Fatal("eviction kept the oldest class instead of recent work")
	}
}
