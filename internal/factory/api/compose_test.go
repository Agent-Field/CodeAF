package api_test

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// marked is a seam whose every door answers by recording who it belongs to,
// so a composed seam can be asked which half each of its doors came from.
func marked(who string, said *string) factory.Seam {
	var s factory.Seam
	v := reflect.ValueOf(&s).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() != reflect.Func {
			continue
		}
		t := f.Type()
		f.Set(reflect.MakeFunc(t, func([]reflect.Value) []reflect.Value {
			*said = who
			out := make([]reflect.Value, t.NumOut())
			for j := range out {
				out[j] = reflect.Zero(t.Out(j))
			}
			return out
		}))
	}
	return s
}

// call presses door with zero arguments.
func call(door reflect.Value) {
	args := make([]reflect.Value, door.Type().NumIn())
	for i := range args {
		args[i] = reflect.Zero(door.Type().In(i))
	}
	door.Call(args)
}

// TestComposeTakesWiredDoorsFromRemoteAndTUIOnlyFromLocal walks every Seam
// field, so a door added to the Seam fails here until it is put in api.Verbs
// or api.TUIOnly, and one put in the wrong table answers from the wrong half.
func TestComposeTakesWiredDoorsFromRemoteAndTUIOnlyFromLocal(t *testing.T) {
	var said string
	composed := api.Compose(marked("remote", &said), marked("local", &said))
	wired := map[string]bool{}
	for _, v := range api.Verbs {
		for _, d := range v.Seam {
			wired[d] = true
		}
	}
	v := reflect.ValueOf(composed)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		_, only := api.TUIOnly[name]
		want := map[bool]string{true: "remote", false: "local"}[wired[name]]
		if !wired[name] && !only {
			t.Errorf("%s is in neither api.Verbs nor api.TUIOnly", name)
			continue
		}
		if v.Field(i).IsNil() {
			t.Errorf("%s is nil after Compose; want the %s door", name, want)
			continue
		}
		said = ""
		call(v.Field(i))
		if said != want {
			t.Errorf("%s answered from %q; want %q", name, said, want)
		}
	}
}

// TestThroughFollowsTheCurrentSeam asks a door after the seam behind it has
// changed: the call reaches the new one, and TUIOnly doors stay nil.
func TestThroughFollowsTheCurrentSeam(t *testing.T) {
	var said string
	now := marked("first", &said)
	through := api.Through(func() factory.Seam { return now })
	if through.Talk != nil {
		t.Fatal("Through filled a TUIOnly door; Compose owns those")
	}
	now = marked("second", &said)
	if _, err := through.Load(); err != nil || said != "second" {
		t.Fatalf("Load answered %q, %v; want the seam current() answers now", said, err)
	}
	now = factory.Seam{}
	if err := through.Launch(1); err == nil {
		t.Fatal("a door the current seam lacks answered no error")
	}
}

func TestComposeJobsKeepsRemoteJobDoorsOffTheLocalFloor(t *testing.T) {
	var said string
	local, remote := marked("local floor", &said), marked("remote", &said)
	here := false
	workers := map[int]factory.Seam{7: marked("worker mirror", &said)}
	seam := api.ComposeJobs(remote, local, func() bool { return here }, func(id int) factory.Seam { return workers[id] })
	// EVERY JOB-ID DOOR IS CHECKED, including doors added after this test.
	for _, id := range []int{0, 1, 7} {
		v := reflect.ValueOf(seam.Job(id))
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if _, only := api.TUIOnly[f.Name]; !only || f.Name == "ForJob" {
				continue
			}
			job := false
			for j := 0; j < min(f.Type.NumIn(), 2); j++ {
				job = job || f.Type.In(j).Kind() == reflect.Int
			}
			if !job {
				continue
			}
			if got := v.Field(i).IsNil(); got != (id != 7) {
				t.Errorf("job %d: %s nil = %v", id, f.Name, got)
			}
		}
	}
	if err := seam.Job(1).Launch(1); err != nil || said != "remote" {
		t.Fatalf("wired launch = %q, %v", said, err)
	}
	if err := seam.Job(7).SetCap(7, 8); err != nil || said != "worker mirror" {
		t.Fatalf("held cap = %q, %v", said, err)
	}
	cap := seam.Job(7).SetCap
	delete(workers, 7)
	said = ""
	if err := cap(7, 9); err == nil || said != "" {
		t.Fatalf("released cap = %q, %v; want a refusal without a local call", said, err)
	}
	here = true
	if err := seam.Job(1).SetCap(1, 10); err != nil || said != "local floor" {
		t.Fatalf("loopback cap = %q, %v", said, err)
	}
	here = false
	said = ""
	if err := cap(1, 11); err == nil || said != "" {
		t.Fatalf("a captured door crossed floors: %q, %v", said, err)
	}
}
