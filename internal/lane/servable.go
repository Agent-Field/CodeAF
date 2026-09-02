// ── ONE MODEL, ONE NAME IN THE LEDGER ───────────────────────────────────────
//
// A belief is only worth keeping if the next session asks for it under the same
// name. The tier suffix was the first way that broke and [BareModel] is the
// answer to it; the FLOATING ALIAS is the second, and it is worse, because the
// shipped default is one.
//
// `~deepseek/deepseek-v4-flash-latest` is what the config carries, what the
// operator sees and what goes on the wire; OpenRouter resolves it on its side,
// per request, and publishes the endpoints page under the concrete model it
// currently points at. So on 2026-09-01 the sighting side filed beliefs under
// `deepseek/deepseek-v4-flash-latest`, the beat asked for a sheet under
// `~deepseek/deepseek-v4-flash-latest`, and the machines that answered were
// `deepseek/deepseek-v4-flash-0731`'s. One model, three names, and the model
// every install routes by default therefore had a ledger with nothing in it and
// a chooser with no opinion — the exact condition this package exists to end.
//
// THE FOLD IS INJECTED AND NOT IMPORTED, for the reason `internal/profile`
// states above its own: this package holds an opinion about lanes and must not
// acquire a network-backed discovery service to hold it. A surface that has a
// catalog installs its answer once at launch; a surface that has none keeps the
// bare-model normalisation alone, which is exactly what every caller had before
// this existed. Degrading to today is the requirement — never to nothing.
package lane

import "sync"

// Servable turns one spelling of a model into the id the router will actually
// serve it under. It must be pure, cheap and non-blocking: it is read on the
// send path as well as at launch, and a fold that went and looked something up
// would be a fetch in front of a request, which is the law this package opens
// with.
//
// AN EMPTY ANSWER MEANS "I CANNOT SAY YET", and it is a required answer rather
// than a rude one. The catalog behind this seam warms in the background, and a
// fold that answered the id as written while it was still in flight would be
// indistinguishable from a fold that had looked and found nothing to move —
// which [LedgerModel] would then remember for the life of the process. A
// spelling nobody can resolve yet is not a spelling anybody may file under.
//
// NON-BLOCKING IS NOT LOCK-FREE, and the difference is worth stating here
// because a comment in this package once got it wrong the other way round.
// [LedgerModel] takes an ordinary in-process mutex around its memo — one
// uncontended lock and a map lookup, held for no I/O — which is a different
// thing from the EXCLUSIVE FILE lock the store takes to compact. That one is
// no longer taken in front of a stream: it belongs to the writer goroutine
// alone ([ledger.Persist]), and every lock in this package is now asked for
// without waiting (issue #264, store.go). What this seam promises is that
// resolving a name never waits on a disk, a network or another process.
type Servable func(model string) string

var servable struct {
	sync.Mutex
	resolve Servable
	memo    map[string]string
}

// UseServable installs the catalog-backed fold for this process. It is the same
// shape as profile.UseIdentity, installed at the same place and the same moment
// by the surface that owns the catalog.
//
// Installing clears the memo, so a process that installs late is consistent
// from that point on rather than carrying an answer it gave before it could.
func UseServable(resolve Servable) {
	servable.Lock()
	defer servable.Unlock()
	servable.resolve, servable.memo = resolve, nil
}

// LedgerModel is the one name a model's beliefs and its sheet are filed under.
//
// Two layers, and only the first is always there: the tier suffix comes off
// because a tier is not a deployment (see [BareModel]), and then whatever fold
// was installed is applied over that. An empty id stays empty, and a fold that
// answers nothing is ignored rather than obeyed — a blank ledger key would file
// every model's beliefs together.
//
// The answer is memoised per spelling for the life of the process, for the
// reason profile.Identity memoises: the installed fold reads a catalog that
// warms in the background, and asked before it lands and again after it would
// honestly give two answers. A ledger key that moved halfway through a run
// would split a history inside one session rather than across two. First answer
// wins.
//
// ONLY AN ANSWER IS REMEMBERED. A fold that cannot say yet ([Servable]) hands
// back nothing, and nothing is used for this one call and forgotten — because
// the alternative is the bug this file exists to end, made permanent: a process
// that asked one moment before its catalog landed would key its entire run on
// the alias and never fold again. So the cost of asking early is one unfolded
// call, never a session.
//
// IT IS IDEMPOTENT, and it has to be: the same name reaches this from a config
// slot, from a wire answer and from a row already on disk, and a key that moved
// on the second application would re-split what the first folded together.
func LedgerModel(model string) string {
	bare := BareModel(model)
	if bare == "" {
		return ""
	}
	servable.Lock()
	defer servable.Unlock()
	if folded, ok := servable.memo[bare]; ok {
		return folded
	}
	if servable.resolve == nil {
		return remember(bare, bare)
	}
	answer := BareModel(servable.resolve(bare))
	if answer == "" {
		return bare
	}
	return remember(bare, answer)
}

// remember files one spelling's fold for the life of the process and hands it
// back. It is called with the lock held.
func remember(bare, folded string) string {
	if servable.memo == nil {
		servable.memo = make(map[string]string, 4)
	}
	servable.memo[bare] = folded
	return folded
}
