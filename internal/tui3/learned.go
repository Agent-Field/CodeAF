package tui3

// WHAT `open` AND `tick` LEARNED, SO THAT `body` NEVER HAS TO ASK THE DISK.
//
// ARCHITECTURE.md's fourth law is one sentence — `open` and `tick` may read the
// disk and `body` may not — and until this file it was true by habit. Habit lost
// twice in the same shape, and both times the cost was one syscall per visible
// thing per frame at thirty frames a second:
//
//   - every visible picture was stat'd from inside [app.View], because the stat
//     WAS the preview cache's key (imagepreview.go);
//   - the model cache was read off disk on every frame that had to name a model,
//     because the list resolved through [CachedModelsFor] with nothing in front
//     of it (models.go).
//
// Both are the SAME SHAPE — "a fact read off the disk under a name, kept until
// something says the bytes moved" — so there is ONE mechanism for it and both
// callers use it. A third caller with a file to read under a name belongs here
// too; a third ad-hoc map with its own staleness rule is the spaghetti this
// replaces.
//
// ── THE LAW, AS THREE DOORS ──
//
//   - [learned.of] IS THE FRAME'S DOOR AND IT READS NOTHING. It answers the memo
//     and, on a miss, writes the name down as asked-for. A frame that meets a
//     name nobody has read yet draws what it draws for a fact it does not have,
//     and the loop catches up before the next frame.
//
//     WHAT THAT IS, IS THE CALLER'S OWN FALLBACK AND NOT ALWAYS ABSENCE. A
//     picture nobody has stat'd draws the nothing it already drew for a file it
//     could not stat. A model list nobody has read falls to the rung BELOW it,
//     which is [BuiltinModels] — five names rather than none — so a frame that
//     met that miss shows different rows, not an empty box. Both are the answer
//     the caller already had for "the list is not here"; neither is a blank
//     where something used to be.
//
//   - [learned.learn] IS THE LOOP'S DOOR. It reads the name NOW, on the calling
//     goroutine, and files what came back. Every caller of it is `open`, a tick,
//     or an arrival — the moment the picture's own call finished, the moment the
//     person attached the file, the moment the catalog was rewritten — which is
//     exactly where the fourth law says a read belongs. One stat at the end of a
//     tool call is nothing beside the tool call; one stat per frame is a syscall
//     storm.
//
//   - [learned.refresh] IS THE TICK'S DOOR: read again, under every name it
//     holds, so a file somebody overwrote behind this surface's back is noticed on
//     the beat rather than never. The beat is the pulse's ten seconds
//     (pulsebeat.go), which is what this surface already pays to read the world.
//
// [learned.catchUp] is the seam between the first two: the loop, once a message,
// reads whatever the last frame asked for. It is the backstop that makes the
// arrival hooks a matter of LATENCY rather than of correctness — a picture whose
// arrival nobody hooked is drawn one message late instead of never.

// learned is a memo of facts read off the disk under a name.
//
// The zero value is not usable: a memo with no reader has nothing to learn, so
// [newLearned] is the only way to make one.
type learned[T any] struct {
	// read is the ONE reading. It must be safe to call with nothing held and
	// answer the zero value for a name that cannot be read — an absent file is a
	// fact like any other, and a memo that refused to hold it would ask the disk
	// about it again on the next frame, which is the whole of what this file
	// exists to stop.
	read func(name string) T
	// facts is what has been read, by name.
	facts map[string]T
	// asked is what the FRAME wanted and the memo could not answer. It is drained
	// by [learned.catchUp] on the loop.
	asked map[string]bool
}

func newLearned[T any](read func(name string) T) learned[T] {
	return learned[T]{read: read}
}

// of is THE FRAME'S DOOR: the fact under this name, and false when nobody has
// read it yet. IT NEVER TOUCHES THE DISK. A miss is written down so that the
// loop reads it before the next frame ([learned.catchUp]).
func (l *learned[T]) of(name string) (T, bool) {
	if fact, known := l.facts[name]; known {
		return fact, true
	}
	var none T
	if name == "" || l.read == nil {
		return none, false
	}
	if l.asked == nil {
		l.asked = make(map[string]bool, 4)
	}
	l.asked[name] = true
	return none, false
}

// learn is THE LOOP'S DOOR: read this name now and file what came back. It is
// called from `open`, from a tick and from an arrival, and from nowhere that a
// frame can reach.
func (l *learned[T]) learn(name string) T {
	var none T
	if name == "" || l.read == nil {
		return none
	}
	fact := l.read(name)
	l.lay(name, fact)
	return fact
}

// memoMax is how many names one memo keeps. It is a bound on THE BEAT rather
// than on memory — [learned.refresh] reads everything held, so an unbounded memo
// is a tick that grows for the life of the window — and it is sized the way
// [pictureCacheMax] is sized, against one frame's working set with room over it:
// sixteen times the preview cache, because a name costs a map entry where a
// preview costs its painted rows.
//
// Past it the memo is dropped WHOLE rather than evicted, for [pictureCacheMax]'s
// reason: an eviction order is more machinery than the problem has, and the cost
// of being wrong is that the names still on screen are read again — once, on the
// loop, by [learned.catchUp].
const memoMax = 16 * pictureCacheMax

// lay files one reading under its name.
func (l *learned[T]) lay(name string, fact T) {
	if len(l.facts) >= memoMax {
		l.facts = nil
	}
	if l.facts == nil {
		l.facts = make(map[string]T, 8)
	}
	l.facts[name] = fact
	delete(l.asked, name)
}

// forget drops one name, for the one caller who KNOWS the bytes under it moved
// and will not wait for the beat to find out.
func (l *learned[T]) forget(name string) {
	delete(l.facts, name)
	delete(l.asked, name)
}

// catchUp reads whatever the last frame asked about and could not be told. It is
// the loop's, once a message, and it is a BACKSTOP: every name this surface can
// see coming is learned at its arrival instead, and this is what keeps the ones
// nobody hooked to one message late rather than to never.
//
// It answers whether it read anything, because a fact that arrived after the
// frame that wanted it is a frame the surface owes itself again.
func (l *learned[T]) catchUp() bool {
	read := false
	for name := range l.asked {
		delete(l.asked, name)
		if _, known := l.facts[name]; known {
			continue
		}
		l.lay(name, l.read(name))
		read = true
	}
	return read
}

// refresh reads every name this memo holds, again. It is the TICK's door, and it
// is how a file that changed behind this surface's back is noticed at all:
// nothing on the machine tells a terminal that a png was overwritten or that
// another window rewrote the model cache, so the beat asks.
//
// IT RE-READS EVERYTHING RATHER THAN ONLY WHAT IS ON SCREEN. Re-reading only the
// visible names would need a second set to keep, and it would let a name go
// stale in a way nothing else in this file can — a fact laid in by `open` that no
// frame has drawn yet would never be asked about again.
//
// WHAT A BEAT COSTS IS THE READER'S OWN COST, AND THE TWO READERS ARE NOT ALIKE.
// A picture's is a warm stat, about a microsecond, so a session holding [memoMax]
// of them pays about a millisecond every ten seconds against the three hundred
// and sixty syscalls a second the frame was paying. A model list's is an
// os.ReadFile and a JSON parse of the whole catalog — tens of kilobytes — and
// that one rides the beat only because another window running `codeaf models
// --refresh` is the one writer this process cannot be told about; every writer
// INSIDE it already calls [app.forgetModelList]. A reader more expensive than
// that does not belong on a memo without a cheaper way to ask whether anything
// moved.
func (l *learned[T]) refresh() {
	for name := range l.facts {
		l.facts[name] = l.read(name)
	}
	l.catchUp()
}

// memo is a learned memo with its type forgotten, so the app can hold every one
// of them in a list and drive them in one place rather than naming each at every
// beat. [app.catchUpLearning] and [app.refreshLearning] are the two drivers.
type memo interface {
	catchUp() bool
	refresh()
}
