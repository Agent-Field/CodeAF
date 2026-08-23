package substore

import (
	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// Build turns a loaded bundle into the thing that runs it. It is the runtime
// lane's door into this package and the only one.
//
// THE STORE DOES NOT KNOW WHAT JAVASCRIPT IS. PRD §3 says there is ONE generic
// Go runner parameterized by a bundle, not one runner per bundle, and this
// function type is that sentence written as a signature: the store's whole job
// is to hand it a [Bundle], and everything about goja, fuel, guards and the
// deopt is on the other side of it.
type Build func(Bundle) (exec.Runner, error)

// Source is this store seen as a place the registry loads bundles from —
// exec.BundleSource, and the door in is Registry.UseBundles(layer, source).
//
// A STORE WITH NO RUNTIME IS NOT A SOURCE AT ALL, which is why this returns a
// nil interface when handed no builder rather than a source whose every lookup
// fails. That is the codebase's law about a capability that cannot work stated
// at the earliest possible place: a build with no JavaScript runtime in it
// offers no bundles, so a person is shown nothing about them instead of a list
// of names that refuse to run. Registry.UseBundles takes a nil source as a
// no-op, so the wiring reads the same either way:
//
//	registry.UseBundles(exec.LayerHome, substore.Home().Source(runtime.Build))
func (s *Store) Source(build Build) exec.BundleSource {
	if s == nil || build == nil {
		return nil
	}
	return &source{store: s, build: build}
}

// source is the registry's view of one store.
type source struct {
	store *Store
	build Build
}

// Runner loads one subharness's head version and builds it.
//
// IT IS ASKED, NEVER SCANNED, and it holds no cache — which is exec.BundleSource's
// own account of why it is shaped this way, and it is the right trade here: a
// store that had cached a listing would run a version the person had just
// replaced. Not found is (nil, false) and is not an error; it is the next
// layer's turn. A bundle that is THERE and does not load is also (nil, false),
// with a line in the journal, because a person who typed a name is better served
// by "no subharness by that name" than by a runner that fails at its first host
// call.
func (b *source) Runner(name string) (exec.Runner, bool) {
	head, err := b.store.Head(name)
	if err != nil {
		return nil, false
	}
	bundle, err := b.store.Load(name, head)
	if err != nil {
		b.store.notice("%v — leaving it out", err)
		return nil, false
	}
	runner, err := b.build(bundle)
	if err != nil || runner == nil {
		b.store.notice("%s v%d could not be built: %v — leaving it out", name, head, err)
		return nil, false
	}
	return runner, true
}

// Manifests is every subharness in this store that a person could actually run.
//
// A BUNDLE THAT FAILS VALIDATION IS ABSENT FROM THIS LIST and says so once in
// the journal. That is not the store being quiet about a problem: a list is not
// where somebody learns their disk is broken, and a row that cannot be picked is
// worse than no row, because it is a promise the surface behind it cannot keep.
// The journal line is what makes the absence findable.
//
// A store that cannot be read at all answers nothing, which exec.BundleSource
// asks for by name: a registry is not worth failing a launch over.
func (b *source) Manifests() []exec.Manifest {
	names, err := b.store.Names()
	if err != nil {
		b.store.notice("%s could not be read: %v", b.store.Dir(), err)
		return nil
	}
	manifests := make([]exec.Manifest, 0, len(names))
	for _, name := range names {
		head, err := b.store.Head(name)
		if err != nil {
			continue
		}
		manifest, err := b.store.Manifest(name, head)
		if err != nil {
			b.store.notice("%v — leaving it off the list", err)
			continue
		}
		manifests = append(manifests, manifest)
	}
	return manifests
}
