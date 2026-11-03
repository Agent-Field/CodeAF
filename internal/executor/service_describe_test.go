package executor

import (
	"reflect"
	"testing"
)

func TestDescribeInspectsEachMemberOnce(t *testing.T) {
	members := []int{10, 11, 12}
	wantArgv := []string{"postgres", "-D", "data"}
	reads := make(map[int]int)
	ports := func(pid int) []int {
		reads[pid]++
		return map[int][]int{11: {8080, 5432}, 12: {8080}}[pid]
	}
	argv := func(pid int) []string {
		if pid != 11 {
			t.Fatalf("argv read for member %d, want first listener 11", pid)
		}
		return wantArgv
	}
	got := describeWith(ExecRequest{Argv: []string{"sh"}}, "/w", 10, members, ports, argv)
	for _, pid := range members {
		if reads[pid] != 1 {
			t.Errorf("member %d inspected %d times, want once", pid, reads[pid])
		}
	}
	if got.Name != "postgres" || !reflect.DeepEqual(got.Argv, wantArgv) ||
		!reflect.DeepEqual(got.Ports, []int{5432, 8080}) || got.DataDir != "data" || got.PGID != 10 {
		t.Fatalf("service = %+v", got)
	}

	t.Run("no listeners", func(t *testing.T) {
		for _, fallback := range []bool{false, true} {
			reads := make(map[int]int)
			req := ExecRequest{Argv: []string{"/bin/request", "argument"}}
			wantArgv := []string{"/bin/sleep", "30"}
			wantName := "sleep"
			if fallback {
				wantArgv, wantName = req.Argv, "request"
			}
			got := describeWith(req, "/w", 10, members, func(pid int) []int {
				reads[pid]++
				return nil
			}, func(pid int) []string {
				if pid != members[0] {
					t.Fatalf("argv read for member %d, want first member %d", pid, members[0])
				}
				if fallback {
					return nil
				}
				return wantArgv
			})
			for _, pid := range members {
				if reads[pid] != 1 {
					t.Errorf("member %d inspected %d times, want once", pid, reads[pid])
				}
			}
			if got.Name != wantName || !reflect.DeepEqual(got.Argv, wantArgv) || len(got.Ports) != 0 {
				t.Fatalf("fallback=%t: service = %+v", fallback, got)
			}
		}
	})
}
