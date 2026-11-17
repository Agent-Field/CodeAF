package cellsync

import "testing"

// The record's size is the bytes of the frames its plan names: a new record
// starts it, a known size grows, and a plan begun before sizes were kept stays
// unknown instead of counting only its latest frames.
func TestPlanSize(t *testing.T) {
	put := []FrameFile{{Bytes: 300}, {Bytes: 200}}
	for _, c := range []struct {
		name string
		d    Driving
		want uint64
	}{
		{"a new record", Driving{}, 500},
		{"a known plan grows", Driving{Fence: 3, Frames: []string{"f"}, Size: 1000}, 1500},
		{"a plan from before sizes", Driving{Fence: 3, Frames: []string{"f"}}, 0},
		{"a held record with no plan yet", Driving{Fence: 3}, 0},
	} {
		if got := planSize(c.d, put); got != c.want {
			t.Errorf("%s: planSize = %d, want %d", c.name, got, c.want)
		}
	}
}
