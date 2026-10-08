package store

import (
	"testing"
	"time"
)

// THE RAIL LIVES IN THE META RECORD BESIDE THE SOURCES, and neither write
// loses the other.
func TestRailKeepsTheSourcesAndTheyKeepIt(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if r, err := st.Rail(); err != nil || r != 0 {
		t.Fatalf("a new store's rail = %v, %v", r, err)
	}
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	if err := st.SetSourceMeta("github", SourceMeta{Polled: now}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRail(60); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSourceMeta("github", SourceMeta{Polled: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.Rail(); r != 60 {
		t.Fatalf("a source write lost the rail: %v", r)
	}
	if m, _ := st.SourceMeta("github"); !m.Polled.Equal(now.Add(time.Minute)) {
		t.Fatalf("the rail write lost the source: %+v", m)
	}
	if err := st.SetRail(-5); err == nil {
		t.Fatal("a rail below nothing was kept")
	}
}
