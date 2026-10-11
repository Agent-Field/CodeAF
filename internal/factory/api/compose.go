package api

import (
	"reflect"

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
