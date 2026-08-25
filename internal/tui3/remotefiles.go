package tui3

import (
	"errors"
	"path"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/cas"
	"github.com/Agent-Field/aforge-v2/internal/filedoor"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE FILES ON THE OTHER MACHINE, AS THINGS THIS ONE CAN OPEN.
//
// A hosted session's workspace is on somebody else's disk, and until this file
// existed that was the end of the sentence: pathlink.go turned linking off
// entirely over a connection (its own header says why, and it was right to),
// /files listed what THIS laptop had made, and a picture the model produced was
// a path nobody sitting here could reach. The conversation crossed the wire and
// the things it made did not.
//
// ── THE HONESTY RULE IS UNCHANGED; ONLY WHO ANSWERS IT MOVED ────────────────
//
// pathlink.go's law is that NOTHING BECOMES A LINK UNTIL IT HAS BEEN FOUND ON
// DISK, and the stat is what decides. Over a connection the disk is the
// engine's, so the stat is the engine's too: [remote.Client.StatPaths] is the
// same question asked over the wire, answered under the same two-roots law
// [remote.handOver] applies to everything else that crosses (internal/remote's
// file.go). A word the engine did not confirm stays plain text, exactly as a
// word this machine could not stat stays plain text at home.
//
// ── ONE CALL PER BURST, NEVER ONE PER WORD ──────────────────────────────────
//
// A stat at home is a syscall and costs nothing worth counting. A stat over an
// ssh pipe is a round trip, and a paragraph of prose can hold thirty pathish
// words — so the render pass COLLECTS candidates ([remoteFiles.confirm]) and
// the frame clock sends them in one batch of at most [statBatchMax], which is
// internal/remote's own statPathsMax and the number over which the engine
// refuses the call rather than trimming it silently.
//
// THE BATCH IS ASKED OFF THE RENDER PATH, as a tea.Cmd, because a frame that
// waited on a wire would be a surface that froze every time the model named a
// file. Nothing links on the frame the word first appears; the answer arrives,
// the rows are marked stale, and the same words are doors on the next frame.
// That is the same one-frame lateness the local pass has when a turn's last
// tool call creates the file its first sentence named.
//
// A YES IS ASKED ONCE PER SESSION AND A NO IS NOT. The local memo is emptied at
// every turn boundary (app.settle's clear of pathSeen) because a name that was
// not a file may have become one, and this table cannot be emptied that way —
// doing it would put every word in the transcript back on the wire once a turn.
// So the two answers are kept on different terms: a CONFIRMED path never
// changes its mind about being a path and is remembered for the session, while
// an ABSENT one is stamped with the moment it was answered and asked again
// after [remoteAbsentFor]. "I'll write dist/app" is a sentence the model says
// BEFORE the file exists, and a table with no expiry is a table in which that
// word is plain text for the rest of the conversation no matter what gets
// built. [app.prefetchWritten] catches the common case sooner — the model
// writing a file is the event that makes a plain word a door — but it only sees
// the `write` tool, and a file that appeared under `bash` or under a task has
// nothing but the clock to find it.
//
// ── THE LINK IS A CAPABILITY, NOT A PATH ────────────────────────────────────
//
// A confirmed file's anchor points at the door's /f/<id> and never at file://,
// for the reason pathlink.go turned links off in the first place:
// `file:///app/main.go` handed to the terminal in front of you means THIS
// machine's /app/main.go, which is either nothing or a stranger's file. The
// door is a loopback listener this surface owns, its ids are minted per path
// and die with it, and every byte it serves came back through [Source.Fetch] —
// which is the engine's law speaking (internal/filedoor's header).
//
// A CONFIRMED DIRECTORY IS NOT LINKED in this wave. The door serves one file
// per id; a directory would need the browse page pointed at it, and pointing
// somebody at a listing when they clicked a folder name is a different gesture
// than the one this pass is about.
//
// ── THE DOOR OPENS ON FIRST NEED AND NOT BEFORE ─────────────────────────────
//
// A healthy hosted session that never names a file runs no listener at all: the
// door is opened by the first confirmed link, the first prefetch, or the first
// /files, and closed with the surface ([app.quit]). It is a capability that
// costs a port and a goroutine, and a conversation that has no use for one
// should not be paying.
//
// ── THE ONE WRITE, AND WHY IT IS SILENT ─────────────────────────────────────
//
// Everything above reads. The browse page's drag-drop lane is the single thing
// on this seam that puts a byte on the far machine, and what bounds it is not
// this surface's good behavior: [remote.MethodDepositFile] lands a file in that
// session's attachments/ folder and has no argument for a directory, so there
// is no path this side could name even if it wanted one. It opens NO TURN,
// which is the whole reason it is a door of its own rather than SubmitFiles —
// [hostSource.Deposit] says the rest.

const (
	// statBatchMax is how many candidate paths one call carries. It is
	// internal/remote's statPathsMax by construction: over that number the
	// engine refuses the whole call rather than trimming it, so a surface that
	// asked for more would be asking for a refusal.
	statBatchMax = 64

	// prefetchMax is the largest file the surface will fetch SPECULATIVELY.
	//
	// It is far below the wire's own 16MB ceiling (internal/remote's
	// maxFetchBytes) because the two numbers answer different questions. That
	// one is what a person may ASK for and wait on; this one is what the surface
	// spends on a click that has not happened yet. Two megabytes covers the
	// things a model actually writes — a source file, a CSV, a chart, a page of
	// JSON — and stops well short of turning "the model wrote a file" into a
	// connection saturated on the chance somebody looks at it.
	prefetchMax = 2 << 20

	// remoteFactsMax bounds the table, on pathSeenMax's reasoning exactly: a
	// conversation that has named four thousand distinct paths is one whose
	// oldest rows are far off screen, and dropping the lot costs one batched
	// call the next time any of them is drawn.
	remoteFactsMax = 4096

	// prefetchAtOnce is how many speculative fetches may be on the wire at the
	// same moment.
	//
	// SPECULATIVE WORK MUST NEVER OUTRUN THE PERSON'S NEXT MESSAGE. A turn that
	// writes forty files is an ordinary turn — a generator, a build, a batch of
	// charts — and without a cap that is forty stats and forty transfers fanned
	// out on ONE ssh pipe, ahead of a click nobody has made, in front of the
	// frames and the fetches somebody is actually waiting on. Three at a time
	// keeps the pipe busy without owning it.
	//
	// AND WHAT DOES NOT FIT IS DROPPED RATHER THAN QUEUED. A queue of
	// speculative work outlives the reason for it: the fortieth file's turn
	// would come round long after the turn that wrote it, and the cost of not
	// having prefetched is one round trip on a click that will probably never
	// happen.
	prefetchAtOnce = 3

	// remoteAbsentFor is how long this surface believes a NO.
	//
	// It is the expiry the local pass gets for free from the turn boundary, and
	// this file's header says why the boundary itself cannot be used here. The
	// number is longer than the burst of frames one reply draws — so a
	// paragraph full of names that are not files is one batch and not a batch a
	// second — and shorter than the time a person spends reading that reply, so
	// a file the turn actually built is a door by the time they reach for it.
	remoteAbsentFor = 20 * time.Second

	// remoteStatQuiet is how long the surface stops asking after a call did not
	// get through at all.
	//
	// A DROPPED CALL LEAVES ITS WORDS RE-ASKABLE ([remoteFiles.forget]), and
	// without this that is a question every frame for as long as the link is
	// down — the render pass would re-queue what the failure released, on the
	// next frame, forever. Two seconds is long enough that a link being
	// redialled (hostlink.go) has a chance to come back before anybody asks it
	// anything, and short enough that nobody notices the wait once it has.
	remoteStatQuiet = 2 * time.Second

	// remoteOpenQuiet is how long a fetch may take before the surface says
	// anything at all about it. THE EMPTINESS LAW APPLIED TO A WAIT: a file that
	// arrives in a fifth of a second and a file that was already in the cache
	// look the same to a person, and a note about either would be the surface
	// narrating itself.
	remoteOpenQuiet = 300 * time.Millisecond
)

// ── the seams, by shape ─────────────────────────────────────────────────────

// pathStater is the honesty rule over the wire: which of these candidates are
// real files under the engine's own law.
//
// It is asserted rather than required, for the reason attach.go's fileSubmitter
// is: a surface must not demand of every agent a method only one kind of agent
// can have. The argument and the answer are internal/remote's own types because
// a Go method set is matched on the exact type — a look-alike declared here
// would be a seam nothing satisfies.
type pathStater interface {
	StatPaths(paths []string) ([]remote.PathFact, error)
}

// remoteLister is one directory of the engine's, and the surface asks it for
// exactly one thing prose cannot: how big a file is BEFORE its bytes cross.
type remoteLister interface {
	ListDir(path string) (remote.DirListing, error)
}

// remoteFetcher is the bytes themselves.
type remoteFetcher interface {
	FetchFile(path string) (remote.FetchedFile, error)
}

// depositor is the one door on this seam that WRITES, and the whole of what it
// may write is a file into the far session's attachments folder — never a path
// of this side's choosing (internal/remote's [remote.MethodDepositFile] states
// the law it obeys over there). It opens no turn, which is what makes a drag
// onto a web page something other than a message nobody said.
type depositor interface {
	DepositFile(name, mime string, data []byte) (string, error)
}

// remoteWire is the four of them together, which is [remote.Client] by shape
// and is what a test fakes in one struct.
type remoteWire interface {
	pathStater
	remoteLister
	remoteFetcher
	depositor
}

// clientBearer is how the surface reaches the wire from the agent it was handed.
// [remote.Agent] is a conversation and the three questions above are about the
// SESSION's disk rather than about the conversation, so they live on the client
// under it — and this is the one method that crosses between them.
type clientBearer interface {
	Client() *remote.Client
}

// doorLinker is the door as this file uses it, which is the whole of
// [filedoor.Door] minus its construction. It is an interface so that a test can
// watch what would have been minted without a listener appearing on the machine
// running it — the same seam, and the same reason, as opener.go's
// [processOpener].
type doorLinker interface {
	FileURL(path string) (string, error)
	BrowseURL() string
	Close() error
}

// openDoor is where this file reaches out of the program and starts a listener.
var openDoor = func(source filedoor.Source) (doorLinker, error) { return filedoor.Open(source) }

// ── what the surface knows about the far disk ───────────────────────────────

// remoteFact is the engine's word on one path, plus what this surface did with
// it. The zero value is "not a file this session may link", which is what every
// word starts as and what most of them stay.
type remoteFact struct {
	// file says the engine confirmed a regular file under its two-roots law.
	file bool
	// dir says it confirmed a directory, which this wave does not link. It is
	// kept apart from absence because they are different facts and a later wave
	// pointing the browse page at a folder will want the true one.
	dir bool
	// uri is the door's capability URL for this path, minted once. Empty while
	// the door is not open, which is the same as not linked.
	uri string
	// size is what the engine said, or zero for unknown — the emptiness law's
	// own reading, and the reason a slow-fetch note sometimes carries a weight
	// and sometimes does not.
	size int64
	// asOf is when this was answered, and it is written for ABSENCE ONLY. A
	// path the engine confirmed is a path for good; a path it did not is a
	// claim with a clock on it, because most of the names a model writes before
	// it has made them are names it is about to make ([remoteAbsentFor]).
	asOf time.Time
}

// remoteFiles is everything this surface holds about the other machine's disk:
// what it has asked, what it has been told, the door it hands out, and the
// content-addressed cache that keeps it from asking twice.
//
// IT IS ONE STRUCT AND NOT FIELDS ON [app] because half of it is touched from
// the tea loop and half from the door's own HTTP goroutines, and the boundary
// between those two is the thing most worth being able to read in one place.
// The facts table and the want list belong to the LOOP — the render pass writes
// them, exactly as it writes pathlink.go's memo — and the blob cache belongs to
// EVERYBODY, so it is the only part behind a mutex.
type remoteFiles struct {
	// host is the machine as the person typed it, which is the browse page's
	// title and the mirror's own directory name.
	host string
	// wire is the four doors. Never nil: a session whose agent bears no
	// client gets no [remoteFiles] at all, which is the absence law rather than
	// a surface holding a seam that answers errors.
	wire remoteWire

	// ── the loop's half ──
	facts map[string]remoteFact
	// want is the batch being assembled, in the order the words were drawn, and
	// wanted is the set that keeps one word out of two batches.
	want   []string
	wanted map[string]bool
	// asking is the debounce: one call in flight at a time, so a burst of rows
	// is one round trip rather than one per frame.
	asking bool
	// quietUntil is the moment the surface may ask the far disk anything again
	// after a call that did not get through ([remoteStatQuiet]).
	quietUntil time.Time
	// opening is the open flow's in-flight set, read by the slow-note tick to
	// decide whether there is still anything to say (openRemotePath).
	opening map[string]bool
	// prefetching is the same guard for the speculative half: what is already
	// being fetched on the chance somebody wants it, so that a turn writing one
	// file six times is one fetch and a turn writing forty is three at a time
	// ([prefetchAtOnce]).
	prefetching map[string]bool
	door        doorLinker
	// doorFailed is remembered so that a door which could not open is not
	// retried on every frame. The sentence was said once; the second attempt
	// would say it again.
	doorFailed bool

	// now is the clock, which is time.Now everywhere but in a test. Two rules in
	// this file are ages — how long a NO is believed and how long a dead call
	// buys quiet — and proving either one by sleeping would be a suite that
	// takes twenty seconds to learn what a fake clock says instantly.
	now func() time.Time

	// ── everybody's half ──
	mu    sync.Mutex
	blobs *cas.Store
	refs  map[string]remoteBlob
}

// clock is [remoteFiles.now] with a default, so that a zero-value struct — the
// one a test of the door's own seam builds — still has a time.
func (r *remoteFiles) clock() time.Time {
	if r == nil || r.now == nil {
		return time.Now()
	}
	return r.now()
}

// remoteBlob is one remote file this surface is already holding: the digest its
// bytes live under, the two things the door has to say about them on the way
// back out, and WHAT THE FAR FILE WAS WHEN THEY CROSSED. THE NAME AND THE TYPE
// ARE THE ENGINE'S ANSWER and are kept rather than re-derived, for the reason
// [remote.FetchedFile.Name] gives: the surface is not on the machine that path
// is true on and must not have to guess.
type remoteBlob struct {
	ref  cas.Ref
	name string
	mime string
	// size and mtime are the far file's own numbers at the moment these bytes
	// were fetched, and they are the whole of the freshness rule
	// ([remoteBlob.fresh]). Without them a path→digest map is a cache that can
	// never be wrong out loud: the far file gets rewritten, nothing here
	// notices, and the click serves last week's bytes for the rest of the
	// session.
	size  int64
	mtime int64
}

// newRemoteFiles builds the surface's remote-file side, or nothing at all.
//
// NOTHING AT ALL IS THE COMMON CASE and it is the honest one: a local session
// has no far disk, and a hosted session whose agent hands over no client is a
// build that cannot do this — so it gets no links, no /files door and no
// prefetch, rather than a set of seams that fail one at a time in front of
// somebody (the absence law, and host.go's whole table).
func newRemoteFiles(host string, agent Agent) *remoteFiles {
	if strings.TrimSpace(host) == "" || agent == nil {
		return nil
	}
	bearer, ok := agent.(clientBearer)
	if !ok {
		return nil
	}
	client := bearer.Client()
	if client == nil {
		return nil
	}
	return newRemoteFilesOver(host, client)
}

// newRemoteFilesOver is [newRemoteFiles] with the wire handed in, which is what
// a test builds and what keeps every rule in this file readable without a
// connection.
func newRemoteFilesOver(host string, wire remoteWire) *remoteFiles {
	if wire == nil {
		return nil
	}
	return &remoteFiles{
		host:        strings.TrimSpace(host),
		wire:        wire,
		facts:       make(map[string]remoteFact, 128),
		wanted:      make(map[string]bool, 128),
		opening:     map[string]bool{},
		prefetching: map[string]bool{},
		refs:        map[string]remoteBlob{},
		now:         time.Now,
	}
}

// confirm is the linker's whole question, asked of a path already resolved
// against the engine's workspace: is this a file I may draw a door on?
//
// It answers "" for everything it has not been told about, AND REMEMBERS THAT
// IT WAS ASKED, which is what turns a render pass into a batch. That is the
// honesty rule in one line: the first frame a word appears on, this surface has
// no idea, so it draws no link and goes and finds out.
//
// A NO IS BELIEVED FOR [remoteAbsentFor] AND THEN ASKED AGAIN, which is this
// file's header's rule and the one thing that lets a name become a door on the
// machine that made it: the model says where it is going to put something long
// before it puts it there, and a word answered "not there" at the top of a turn
// is a file by the end of one.
func (r *remoteFiles) confirm(target string) string {
	if r == nil || target == "" {
		return ""
	}
	if fact, known := r.facts[target]; known {
		switch {
		case fact.file:
			return target
		case fact.dir:
			return ""
		case r.clock().Sub(fact.asOf) < remoteAbsentFor:
			return ""
		}
		// THE NO HAS AGED OUT. Both memos are cleared, because the fact and the
		// "already asked" mark are one memory in two maps and dropping either
		// alone leaves the word stuck: a fact with nothing queued behind it, or
		// a question that will be answered into a table that still says no.
		delete(r.facts, target)
		delete(r.wanted, target)
	}
	r.wantPath(target)
	return ""
}

// wantPath puts one path in the next batch, once.
func (r *remoteFiles) wantPath(target string) {
	if r.wanted[target] {
		return
	}
	// NOTHING IS QUEUED WHILE THE WIRE IS BEING LEFT ALONE. A call that did not
	// get through releases its words rather than recording them as absent
	// ([remoteFiles.forget]), and this is what stops that release from becoming
	// a question every frame for as long as the link is down.
	if r.clock().Before(r.quietUntil) {
		return
	}
	// The want list is bounded by the same reasoning the facts table is: a
	// render that produced four thousand unasked words has already produced far
	// more than the batches can carry, and the tail of it will be asked on the
	// frames after the head comes back.
	if len(r.want) >= remoteFactsMax {
		return
	}
	r.wanted[target] = true
	r.want = append(r.want, target)
}

// url is the door's capability URL for a path, or "" — which reads as "draw no
// link", the same answer an unsafe URI gets from opener.go's [linkOpen].
func (r *remoteFiles) url(target string) string {
	if r == nil {
		return ""
	}
	return r.facts[target].uri
}

// learn writes one fact down, keeping the table bounded.
func (r *remoteFiles) learn(target string, fact remoteFact) {
	if r == nil || target == "" {
		return
	}
	if len(r.facts) >= remoteFactsMax {
		clear(r.facts)
		clear(r.wanted)
		r.want = nil
	}
	// THE STAMP IS PUT ON HERE AND IN NO CALLER, so that there is one place
	// where absence acquires its clock and no way for a caller to write a NO
	// that never expires ([remoteFact.asOf]).
	if !fact.file && !fact.dir {
		fact.asOf = r.clock()
	}
	r.facts[target] = fact
}

// forget puts a batch back the way it was before it was asked: no fact written
// and every word free to be queued again.
//
// A CALL THAT DID NOT GET THROUGH IS NOT AN ANSWER. Writing absence for it
// would be this surface deciding, on the strength of a broken pipe, that up to
// [statBatchMax] real files are not there — a claim about a disk nobody
// consulted, and one that would hold every one of those words plain for a whole
// [remoteAbsentFor] on the day the link blinked. The two cases are told apart
// because they are different sentences: "there is nothing at that path" is the
// engine speaking, and a dead pipe is nobody speaking.
//
// SO NOTHING IS LEARNED AND THE WIRE IS LEFT ALONE FOR A MOMENT. The words go
// back to being unasked, which would be a question every frame while the link
// is down if the quiet window ([remoteStatQuiet]) did not stand in front of
// them.
func (r *remoteFiles) forget(asked []string) {
	if r == nil {
		return
	}
	for _, target := range asked {
		delete(r.wanted, target)
	}
	// AND THE QUEUE BEHIND IT GOES TOO. What was waiting to be asked was waiting
	// on the same wire, so sending it now is a second failure and holding it is
	// a frame clock that goes on turning for the whole quiet window with nothing
	// to do. Nothing was learned about those words, so releasing them costs
	// exactly one render pass to collect again.
	for _, target := range r.want {
		delete(r.wanted, target)
	}
	r.want = nil
	r.quietUntil = r.clock().Add(remoteStatQuiet)
}

// take pulls the next batch off the want list, at most n of it, and arms the
// debounce. An empty answer means there is nothing to ask or a call is already
// out.
func (r *remoteFiles) take(n int) []string {
	if r == nil || r.asking || len(r.want) == 0 || n <= 0 {
		return nil
	}
	if n > len(r.want) {
		n = len(r.want)
	}
	batch := append([]string(nil), r.want[:n]...)
	r.want = append(r.want[:0], r.want[n:]...)
	r.asking = true
	return batch
}

// waiting reports whether anything is still owed an answer. The frame clock
// asks it so that a surface with words in flight goes on turning until they
// land — otherwise a batch discovered on the last frame of a turn would sit
// there until something else woke the clock ([app.paint]).
func (r *remoteFiles) waiting() bool {
	return r != nil && (r.asking || len(r.want) > 0 || len(r.opening) > 0)
}

// ── the batch, and the answer ───────────────────────────────────────────────

// remoteFactsMsg is one StatPaths coming back. It carries what was ASKED as
// well as what was answered, because a path the engine said nothing about is
// still a path this surface must stop asking about — see [app.remoteFactsBack].
type remoteFactsMsg struct {
	asked []string
	facts []remote.PathFact
	err   error
}

// remoteStatKick sends the next batch, or nothing. It is called from the frame
// clock and from nowhere else, which is what makes the debounce true: one call
// per frame at most, and one call in flight at any moment.
func (a *app) remoteStatKick() tea.Cmd {
	r := a.rfiles
	batch := r.take(statBatchMax)
	if len(batch) == 0 {
		return nil
	}
	wire := r.wire
	return func() tea.Msg {
		facts, err := wire.StatPaths(batch)
		return remoteFactsMsg{asked: batch, facts: facts, err: err}
	}
}

// remoteFactsBack writes one answer into the table and turns the rows it
// touched back into frames.
//
// EVERY PATH THE ENGINE ANSWERED ABOUT IS WRITTEN DOWN, including the ones it
// refused. The alternative is a word that is asked about on every frame
// forever, which is the one failure mode a batched wire question must not have
// — and the fact written for a word the engine had nothing to say about is
// ABSENCE, which is what the surface already draws for it: plain text.
//
// A CALL THAT NEVER GOT THERE IS NOT AN ANSWER AND IS NOT WRITTEN DOWN. It used
// to be folded in with the rest, and folding it in meant one dropped frame
// recorded up to [statBatchMax] real files as ABSENT on the strength of nothing
// at all — see [remoteFiles.forget] for the whole of it.
func (a *app) remoteFactsBack(msg remoteFactsMsg) tea.Cmd {
	r := a.rfiles
	if r == nil {
		return nil
	}
	r.asking = false
	if msg.err != nil {
		r.forget(msg.asked)
		a.touch()
		return a.wake()
	}
	answered := make(map[string]remote.PathFact, len(msg.facts))
	for _, fact := range msg.facts {
		answered[fact.Path] = fact
	}
	linked := false
	for i, asked := range msg.asked {
		fact, ok := answered[asked]
		// THE ANSWER MAY NAME THE PATH AS THE ENGINE RESOLVED IT rather than as
		// it was asked, and the wire does not promise which. So the reply is
		// matched by name first and BY POSITION second, which is true whenever
		// the engine answered one fact per candidate — and when neither holds,
		// the candidate is recorded absent, which is the honest reading of an
		// answer this surface could not attribute.
		if !ok && len(msg.facts) == len(msg.asked) {
			fact, ok = msg.facts[i], true
		}
		switch {
		case !ok || !fact.Exists:
			r.learn(asked, remoteFact{})
		case fact.Dir:
			r.learn(asked, remoteFact{dir: true})
		default:
			r.learn(asked, remoteFact{file: true, size: fact.Size, uri: a.mintRemoteURL(asked)})
			linked = true
		}
	}
	if linked {
		// A row already drawn is a row already cached, and what changed is not
		// the text but whether a door was written under it (adaptive.go marks
		// its own rows the same way when the paint changes beneath them).
		a.restyleEntries()
	}
	a.touch()
	// THE NEXT BATCH IS NOT SENT FROM HERE. [app.paint] is the only place a
	// question about the far disk goes out, which is what makes the debounce one
	// rule instead of two — and the clock goes on turning while anything is still
	// owed an answer ([remoteFiles.waiting]), so the tail of a long burst is one
	// call per frame until it is done.
	return a.wake()
}

// restyleEntries marks every block for a redraw. It is used where a fact the
// rows were rendered under has changed rather than the rows themselves.
func (a *app) restyleEntries() {
	for i := range a.entries {
		a.entries[i].stale = true
	}
}

// ── the door ────────────────────────────────────────────────────────────────

// fileDoor is the door, opened on first need.
//
// IT IS OPENED FROM THE LOOP AND NOWHERE ELSE. Starting a listener is a side
// effect with a port in it, and the render pass — which is the thing that
// discovers a confirmed link — must stay a pure reading of state. So the
// callers are the three moments that are already messages: a batch coming back,
// a prefetch landing, and /files being typed.
func (a *app) fileDoor() (doorLinker, error) {
	r := a.rfiles
	if r == nil {
		return nil, errors.New("this conversation is not on another machine")
	}
	if r.door != nil {
		return r.door, nil
	}
	if r.doorFailed {
		return nil, errors.New(filesDoorWord)
	}
	door, err := openDoor(&hostSource{files: r})
	if err != nil || door == nil {
		// REMEMBERED, so the sentence is said once. A door that could not open
		// will not open on the next frame either, and a surface that retried on
		// every confirmed word would say the same line thirty times a second.
		//
		// AND THE SENTENCE IS THIS SURFACE'S OWN. What comes back from a
		// listener is `listen tcp 127.0.0.1:0: socket: too many open files`,
		// which is machinery vocabulary in something a person reads — and the
		// ways a loopback listener can fail are few and none of them are
		// anybody's business here.
		r.doorFailed = true
		return nil, errors.New(filesDoorWord)
	}
	r.door = door
	return door, nil
}

// mintRemoteURL is the door's capability URL for one confirmed path, or "" when
// there is no door to mint one — which the linker reads as "no link", because
// that is exactly what it is.
func (a *app) mintRemoteURL(target string) string {
	door, err := a.fileDoor()
	if err != nil || door == nil {
		return ""
	}
	uri, err := door.FileURL(target)
	if err != nil {
		return ""
	}
	return uri
}

// closeFileDoor takes the listener down with the surface. Every id and the
// browse token die with it, which is the whole of the capability bargain: a URL
// that outlived the conversation it was minted in would be a door onto somebody
// else's disk with nobody left holding it.
func (a *app) closeFileDoor() {
	r := a.rfiles
	if r == nil || r.door == nil {
		return
	}
	_ = r.door.Close()
	r.door = nil
}

// ── prefetch on write ───────────────────────────────────────────────────────

// remotePrefetchedMsg is one speculative fetch coming home. It carries no error
// on purpose: a prefetch that failed is a click that will fetch later, and there
// is nothing to say about it (this file's silence law).
type remotePrefetchedMsg struct {
	target string
	blob   remoteBlob
	size   int64
	ok     bool
}

// prefetchWritten is the surface getting eager, and it is the whole of what the
// wave changed about MOVEMENT: the wire did not grow a push, the engine does not
// know this is happening, and the only difference is that the click which used
// to wait on a round trip usually does not.
//
// THE TRIGGER IS THE MODEL WRITING A FILE, which is the one moment a surface can
// be confident somebody is about to want it — the reply that follows is going to
// name the thing it just made. A read is not a trigger (the model has the bytes
// and the person did not ask for them) and neither is an edit in this wave: an
// edit's target usually existed before the turn, so it is already a link.
//
// IT IS SILENT AND BEST-EFFORT. Every failure here — a refusal, a dead link, a
// file that grew past the ceiling between the stat and the fetch — is dropped
// without a word, because nothing on the screen ever promised this happened.
//
// A SECOND WRITE TO THE SAME PATH IS A SECOND PREFETCH. It has to be: the whole
// point of the trigger is that the bytes on that machine just changed, and a
// guard that skipped a path because the surface already held SOMETHING for it
// would be the guard that pins the stale copy in place. What is deduplicated is
// concurrency and not repetition — one fetch of one path at a time — and
// [remoteFiles.fetch] is what makes the repeat cheap, because a path whose size
// and modification time have not moved is answered out of the cache after one
// small stat.
func (a *app) prefetchWritten(ev session.Event) tea.Cmd {
	r := a.rfiles
	if r == nil || ev.Kind != session.EventToolEnd || ev.Tool != "write" {
		return nil
	}
	target := a.remoteTarget(toolTarget(ev.Tool, ev.Args, ev.Hint))
	if target == "" {
		return nil
	}
	// THE PIPE BELONGS TO THE PERSON AND NOT TO THE GUESS ([prefetchAtOnce]).
	// The same path twice over is one fetch, and a turn that wrote more files
	// than the cap allows simply does not prefetch the rest — a click on one of
	// those is a round trip, which is what a click was before this existed.
	if r.prefetching[target] || len(r.prefetching) >= prefetchAtOnce {
		return nil
	}
	r.prefetching[target] = true
	wire := r.wire
	return func() tea.Msg {
		// THE SIZE IS ASKED BEFORE THE BYTES ARE, which is what makes
		// [prefetchMax] a ceiling rather than a receipt. A fetch-then-measure
		// would have already spent the connection on the file it then threw
		// away, and the whole point of the number is not to spend it. The same
		// answer carries the freshness numbers the cache is about to be judged
		// on, so the stat is one round trip doing two jobs.
		now, told := farStat(wire, target)
		if !told || !now.exists || now.dir || now.size > prefetchMax {
			return remotePrefetchedMsg{target: target, size: now.size}
		}
		blob, _, err := r.fetchAsOf(target, now, told)
		if err != nil {
			return remotePrefetchedMsg{target: target, size: now.size}
		}
		return remotePrefetchedMsg{target: target, blob: blob, size: now.size, ok: true}
	}
}

// remotePrefetched files what a prefetch found. A file that landed in the cache
// is ALSO a file the engine has just confirmed exists, so the fact is written
// here and its row becomes a door without anybody asking StatPaths about it —
// which is what makes the common case of this file's expiry rule never have to
// wait for the clock.
func (a *app) remotePrefetched(msg remotePrefetchedMsg) tea.Cmd {
	r := a.rfiles
	if r == nil {
		return nil
	}
	// THE IN-FLIGHT MARK COMES OFF WHICHEVER WAY IT WENT. A prefetch that
	// failed and left its path marked would be a path this session never
	// prefetches again, which is the same blackhole the fact table used to have
	// in the other lane.
	delete(r.prefetching, msg.target)
	if !msg.ok {
		return nil
	}
	r.setRef(msg.target, msg.blob)
	r.learn(msg.target, remoteFact{file: true, size: msg.size, uri: a.mintRemoteURL(msg.target)})
	a.restyleEntries()
	a.touch()
	return a.wake()
}

// remoteTarget turns a path a tool wrote — relative to the engine's workspace,
// as everything in a hosted conversation is — into the absolute path the wire
// takes. An empty answer is a target this surface will not reason about.
//
// THE ENGINE'S SEPARATOR IS '/' AND NOT THIS MACHINE'S. --host is an ssh
// connection to a machine with a Unix path layout, and a surface running on
// Windows joining a remote path with a backslash would produce a name that
// exists on neither end. So the joining here is [path] and never [filepath],
// which is the same decision internal/remote makes about every path on the wire.
func (a *app) remoteTarget(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "\x00") {
		return ""
	}
	if strings.HasPrefix(name, "/") {
		return path.Clean(name)
	}
	root := strings.TrimSpace(a.workspace)
	if root == "" {
		return ""
	}
	joined := path.Join(root, name)
	// CONTAINMENT, and it is pathlink.go's reason rather than a security one:
	// the engine refuses what leaves its two roots regardless. A relative name
	// in a reply MEANS "in the workspace", and one that climbs out of it with
	// `../..` has stopped meaning that.
	if joined != root && !strings.HasPrefix(joined, strings.TrimSuffix(root, "/")+"/") {
		return ""
	}
	return joined
}

// ── how the far file stands right now ───────────────────────────────────────

// farFact is one path as the engine sees it at the moment it was asked: whether
// there is anything there, and the two numbers that say whether it is still the
// thing this surface already has a copy of.
type farFact struct {
	exists bool
	dir    bool
	size   int64
	mtime  int64
}

// farStat asks the engine about exactly one path, and its second answer is
// whether the QUESTION GOT THROUGH — which is a different thing from a file
// that is not there. The first means this surface knows nothing and must not
// act on its ignorance; the second is a fact it may act on.
//
// IT IS A STAT AND NOT A LISTING ANY MORE. [remote.PathFact] carries the size
// and the modification time now (internal/remote's wire.go), so the questions a
// speculative fetch and a cache each have — how heavy is it, and is it still
// the one I hold — are one small frame instead of a listing of the whole parent
// directory, which on the `out/` of a turn that generated things is two
// thousand rows of JSON to read one number.
func farStat(wire pathStater, target string) (farFact, bool) {
	facts, err := wire.StatPaths([]string{target})
	if err != nil || len(facts) != 1 {
		return farFact{}, false
	}
	fact := facts[0]
	return farFact{exists: fact.Exists, dir: fact.Dir, size: fact.Size, mtime: fact.ModTime}, true
}

// ── the source the door serves from ─────────────────────────────────────────

// hostSource is [filedoor.Source] over this connection: the door asks, the
// engine answers, and this type is only the translation between two packages'
// shapes for the same three questions.
//
// IT IS CALLED FROM THE DOOR'S OWN GOROUTINES, which is why it touches nothing
// on [app] but the fields that never change after construction and nothing on
// [remoteFiles] but the blob cache, which is the half behind the mutex.
type hostSource struct {
	files *remoteFiles
}

// The door's contract, asserted here so that a change to it is a compile error
// in this package rather than a nil handed to [filedoor.Open] at runtime.
var _ filedoor.Source = (*hostSource)(nil)

func (s *hostSource) Host() string { return s.files.host }

// List is one directory, under the engine's own law and with its own resolved
// path in the answer — never one this side derived (internal/remote's
// [remote.DirListing] says why).
func (s *hostSource) List(dir string) (string, []filedoor.Entry, bool, error) {
	listing, err := s.files.wire.ListDir(dir)
	if err != nil {
		return "", nil, false, err
	}
	entries := make([]filedoor.Entry, 0, len(listing.Entries))
	for _, row := range listing.Entries {
		entry := filedoor.Entry{Name: row.Name, Dir: row.Dir, Size: row.Size, MIME: row.MIME}
		if row.ModTime > 0 {
			entry.ModTime = time.Unix(row.ModTime, 0)
		}
		entries = append(entries, entry)
	}
	return listing.Path, entries, listing.Truncated, nil
}

// Fetch is the bytes, through the cache. A file the surface already holds by
// content never crosses the wire twice — see [remoteFiles.fetch].
func (s *hostSource) Fetch(target string) (filedoor.File, error) {
	_, file, err := s.files.fetch(target)
	return file, err
}

// Deposit is a file dropped on the browse page, landing in the far session's
// attachments folder — and landing there WITHOUT SAYING ANYTHING.
//
// A DRAG ONTO A WEB PAGE IS NOT A SENTENCE ANYBODY SAID, which is the whole
// reason this goes through [remote.MethodDepositFile] and not through
// SubmitFiles. That door writes into the same folder and then SUBMITS, so a
// drop here would start a model turn nobody at this end asked for, spend
// somebody's money on it, and stream its answer into a channel this surface is
// not reading. The deposit door keeps the bytes and stops: no turn, no event,
// nothing in the transcript. The conversation learns of the file when a person
// mentions it, and /attach is still the lane for a file WITH a person's own
// sentence on it.
//
// WHERE IT LANDS AND WHAT REFUSES IT ARE BOTH THE ENGINE'S. The path comes back
// from over there and is never derived here ([remote.DepositedFile] says why),
// and a name that is really a path, or a file over the ceiling, is refused by
// the machine that owns the disk, in its own words.
//
// The MIME is empty because the door does not carry one: [filedoor.Source]
// hands over a name and bytes, and the engine treats the type as a hint it can
// do without — an attachment is stored, not decoded.
func (s *hostSource) Deposit(name string, data []byte) (string, error) {
	if s.files == nil || s.files.wire == nil {
		return "", errors.New(depositUnreachableWord)
	}
	return s.files.wire.DepositFile(name, "", data)
}

// depositUnreachableWord is what a drop is told when this window has no wire to
// put it on — a state the door should never be open in, because a session with
// no client gets no [remoteFiles] and therefore no door (newRemoteFiles). It is
// one sentence because it is read inside a browser tab with no room for a
// paragraph, and it says what is wrong rather than what the person did.
const depositUnreachableWord = "this window is not connected to that machine any more"

// filesDoorWord is the door failing to open. The listener is loopback and
// OS-chosen, so the ways it can fail are few and none of them are the person's
// fault or the person's business.
const filesDoorWord = "the file door did not open on this machine"

// ── the browse page ─────────────────────────────────────────────────────────

// openBrowse is /files on a hosted session: the far workspace, as a page.
//
// THE HANDOFF AND THE WRITTEN LINE ARE NOT AN EITHER-OR (opener.go's header
// states the whole law, and connect.go's [app.openConnectFlow] is the other
// caller written under it). The browser is opened, and the address is written
// into the transcript whether or not that worked — a headless box, a machine
// with no xdg-open, a person who wants the URL in a different browser. What must
// never happen is a page nobody can reach.
//
// THE LINE IS PLAIN AND NOT AN OSC 8 ANCHOR, which is the one place this parts
// from that law, and it parts on the "where the sequence is safe" clause rather
// than on the principle. A note is WRAPPED at render time (render.go's
// [wrap]), and an anchor broken across a wrap leaves an opener with no close —
// which is the single failure of OSC 8 a person actually sees, because
// everything after it to the end of the screen becomes one link. A loopback URL
// is short, selectable, and linkified by most terminals on their own; a
// corrupted screen is not worth the difference.
func (a *app) openBrowse() {
	door, err := a.fileDoor()
	if err != nil {
		a.note(strings.TrimSpace(err.Error()))
		return
	}
	link := door.BrowseURL()
	if link == "" {
		a.note(filesDoorWord)
		return
	}
	if err := processOpener(link); err != nil {
		// The platform could not do it. That is not a failed /files — the line
		// below is still a way through — so it is said once and the address goes
		// up as it would have anyway.
		a.note(strings.TrimSpace(err.Error()))
	}
	a.note(a.host + " · " + link)
}

// ── the blob cache ──────────────────────────────────────────────────────────

// blobStore opens the content-addressed store this surface keeps remote files
// in, once.
//
// IT IS ROOTED WHERE EVERY OTHER CAS IN THIS TREE IS ROOTED: a `cas` directory
// under the state root aforge owns (internal/home, and internal/store's own
// cas.New(filepath.Join(dir, "cas"))). It is under `v3/remote/` rather than
// beside the session store because these blobs are not this machine's work —
// they are copies of somebody else's disk, kept so a click does not become a
// round trip, and one directory is one gesture to delete them all.
func (r *remoteFiles) blobStore() (*cas.Store, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.blobs != nil {
		return r.blobs, nil
	}
	store, err := cas.New(home.Join("v3", "remote", "cas"))
	if err != nil {
		return nil, err
	}
	r.blobs = store
	return store, nil
}

// ref and setRef are the path→content map, which is what makes a second click
// free. It is keyed by PATH and holds a DIGEST — and, since the path is the key,
// IT CANNOT NOTICE ANYTHING ON ITS OWN. A digest only changes when somebody
// fetches, so a map from path to digest with nothing else in it will hand back
// the same bytes for a file that has been rewritten twenty times on the machine
// that owns it. What makes it honest is the size and the modification time
// stored beside the digest and checked against the engine before the bytes are
// served ([remoteBlob.fresh], [remoteFiles.fetch]).
func (r *remoteFiles) ref(target string) (remoteBlob, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	found, ok := r.refs[target]
	return found, ok
}

func (r *remoteFiles) setRef(target string, blob remoteBlob) {
	if blob.ref == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refs[target] = blob
}
