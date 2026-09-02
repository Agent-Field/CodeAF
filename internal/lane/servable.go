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
	folded := bare
	if servable.resolve != nil {
		if answer := BareModel(servable.resolve(bare)); answer != "" {
			folded = answer
		}
	}
	if servable.memo == nil {
		servable.memo = make(map[string]string, 4)
	}
	servable.memo[bare] = folded
	return folded
}
