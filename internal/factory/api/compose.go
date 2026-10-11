package api

import (
	"reflect"
	"sync"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// wiredDoors is every Seam field some verb in [Verbs] calls: the doors the
// wire carries.
func wiredDoors() map[string]bool {
	wired := make(map[string]bool)
	for _, v := range Verbs {
		for _, door := range v.Seam {
			wired[door] = true
		}
	}
	return wired
}

// Compose is the seam the desk is handed: every door the wire carries
// ([Verbs]) from remote, the factory the window is attached to, and every
// [TUIOnly] door from local, this machine's own seam.
//
// WHY TWO HALVES: the floor is the factory's, wherever it runs, but a chat
// session file and a checkout live on the person's own machine, so the doors
// that answer those stay this machine's. A field in neither table is left nil;
// the structural test names it, so a new door is decided rather than guessed.
func Compose(remote, local factory.Seam) factory.Seam {
	wired := wiredDoors()
	var out factory.Seam
	o, r, l := reflect.ValueOf(&out).Elem(), reflect.ValueOf(remote), reflect.ValueOf(local)
	for i := 0; i < o.NumField(); i++ {
		name := o.Type().Field(i).Name
		switch _, only := TUIOnly[name]; {
		case wired[name]:
			o.Field(i).Set(r.Field(i))
		case only:
			o.Field(i).Set(l.Field(i))
		}
	}
	return out
}

// ComposeJobs keeps job-id doors on the machine that holds the job. Here
// answers whether the factory is loopback; worker answers a local mirror's
// seam, or the zero seam for a job this machine does not hold. Both selectors
// are reads of cached facts, so drawing a key never does disk or network I/O.
// Calls reselect the door because a lease may end after the key was drawn.
func ComposeJobs(remote, local factory.Seam, here func() bool, worker func(int) factory.Seam) factory.Seam {
	selectLocal := func(id int) factory.Seam {
		if here() {
			return local
		}
		if worker != nil {
			return worker(id)
		}
		return factory.Seam{}
	}
	var mu sync.Mutex
	cache := map[string]factory.Seam{}
	compose := func(id int) factory.Seam {
		selected := reflect.ValueOf(selectLocal(id))
		mask := make([]byte, selected.NumField())
		for i := range mask {
			if jobDoor(selected.Type().Field(i).Name, selected.Field(i).Type()) && !selected.Field(i).IsNil() {
				mask[i] = 1
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if out, ok := cache[string(mask)]; ok {
			return out
		}
		out := Compose(remote, local)
		o, l := reflect.ValueOf(&out).Elem(), selected
		for i := 0; i < o.NumField(); i++ {
			name := o.Type().Field(i).Name
			if !jobDoor(name, o.Field(i).Type()) {
				continue
			}
			if l.Field(i).IsNil() {
				o.Field(i).Set(reflect.Zero(o.Field(i).Type()))
				continue
			}
			o.Field(i).Set(reflect.MakeFunc(o.Field(i).Type(), func(args []reflect.Value) []reflect.Value {
				at := 0
				if args[at].Kind() != reflect.Int {
					at++
				}
				door := reflect.ValueOf(selectLocal(int(args[at].Int()))).Field(i)
				if door.IsNil() {
					return refused(door.Type())
				}
				return door.Call(args)
			}))
		}
		out.ForJob = nil
		// MANY ROWS SHARE THE SAME CAPABILITIES. Their dispatchers select by
		// the call's job id, so a paint need not rebuild reflection closures.
		cache[string(mask)] = out
		return out
	}
	out := compose(0)
	out.ForJob = compose
	return out
}

// jobDoor recognizes the TUI-only doors whose first non-context argument is
// a job id. Repository and floor doors keep their local meaning.
func jobDoor(name string, t reflect.Type) bool {
	if _, only := TUIOnly[name]; !only || name == "ForJob" {
		return false
	}
	for i := 0; i < min(t.NumIn(), 2); i++ {
		if t.In(i).Kind() == reflect.Int {
			return true
		}
	}
	return false
}

// Through is a seam whose wired doors call current() at the moment of the
// call, so a window can move to another factory (the one serving it went away)
// without the desk being handed a new seam. A door is present exactly when it
// is present on the seam current() answers now, which is what the desk reads
// to draw its keys; [TUIOnly] doors are left nil for [Compose] to fill.
func Through(current func() factory.Seam) factory.Seam {
	wired := wiredDoors()
	var out factory.Seam
	o, now := reflect.ValueOf(&out).Elem(), reflect.ValueOf(current())
	for i := 0; i < o.NumField(); i++ {
		name := o.Type().Field(i).Name
		if !wired[name] || now.Field(i).IsNil() {
			continue
		}
		o.Field(i).Set(reflect.MakeFunc(o.Field(i).Type(), func(args []reflect.Value) []reflect.Value {
			door := reflect.ValueOf(current()).Field(i)
			if door.IsNil() {
				return refused(door.Type())
			}
			return door.Call(args)
		}))
	}
	return out
}

// errGone is what a door answers when the factory now in front of the window
// has no such door.
type errGone struct{}

func (errGone) Error() string { return "the factory this window is attached to has no such door" }

// refused is zero values with the last result set to [errGone], which is the
// shape of every wired door: its last result is an error.
func refused(t reflect.Type) []reflect.Value {
	out := make([]reflect.Value, t.NumOut())
	for i := range out {
		out[i] = reflect.Zero(t.Out(i))
	}
	if n := len(out); n > 0 {
		e := reflect.New(t.Out(n - 1)).Elem()
		e.Set(reflect.ValueOf(errGone{}))
		out[n-1] = e
	}
	return out
}
