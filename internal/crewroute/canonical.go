package crewroute

import (
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/pool/index"
)

// ONE MODEL, MANY SPELLINGS — WHAT IS THE SAME MODEL ACROSS PROVIDERS.
//
// Evidence and quality belong to a MODEL; a price, a limit and a send id belong
// to a ROUTE to it. OpenRouter spells GLM `z-ai/glm-5.3-flash`, Fireworks
// `accounts/fireworks/models/glm-5p3-flash`, a Hugging Face mirror
// `zai-org/GLM-5.3-Flash`, OpenRouter's free pool `z-ai/glm-5.3-flash:free`,
// and a local Ollama pull `glm-5.3-flash:q4_k_m`. The router must read those
// as one model with five routes — or, for the local pull, as a distinct
// VARIANT of the one model — or it scores a model once and prices it five
// times as five strangers.
//
// [Canonical] is that reading. It is PURE AND FAST — string operations, no
// pattern engine, no network — because it runs for every catalog row of every
// decision. It composes, in order:
//
//  1. the provider namespaces a re-hosting provider puts in front of a model
//     (`openrouter/`, `accounts/<account>/models/`), taken off;
//  2. the route suffixes that name a way of serving the same weights
//     (`:free`, `:nitro`, `:floor`), taken off — a route is not a model;
//  3. a local quantisation tag (`:q4_k_m`, `:fp8`, `-gguf`), taken off into a
//     VARIANT: the same weights squeezed, which inherit the model's evidence
//     at a discount ([quantDiscount]) but are never the same model;
//  4. spelling: lowercase, `_` read as `-`, and a version written with a `p`
//     between two digits (`5p3`) read as the dot it stands for;
//  5. the owner: a Hugging Face organisation read as the catalog's vendor
//     ([vendorOrgs]), and a bare model name given the vendor its family
//     belongs to ([familyVendors]);
//  6. the pool index's own alternates (internal/pool/index), and the small
//     table of known mismatches below ([modelAliases]);
//  7. [Lineage]'s rules: a thinking level, `-latest` and a dated snapshot.
//
// AN UNKNOWN SPELLING STAYS ITS OWN MODEL. Nothing here merges two ids because
// they look alike: every rule is a fact about how a provider spells an id, and
// a model no rule recognises is read on its own catalog figures, the way
// every model is.

// Canon is a model's canonical identity: the id evidence is kept under, and
// the quantisation variant when the id names a squeezed local copy of it.
type Canon struct {
	ID      string
	Variant string
}

// String is the identity as one key: the id, and the variant after `@` when
// there is one — so a quantised copy is never the model it came from.
func (c Canon) String() string {
	if c.Variant == "" {
		return c.ID
	}
	return c.ID + "@" + c.Variant
}

// quantDiscount is how much of a model's quality a quantised local copy is
// credited with. Quantisation costs something on agentic work — tool calls
// are where a squeezed model slips first — so the credit is a conservative
// share: a local copy sits a seat when it is free AND nearly as good, never
// because its model's score was borrowed whole.
const quantDiscount = 0.85

// routeSuffixes are the `:word` endings that name a way of serving a model
// rather than a model: OpenRouter's free pool, its throughput and price
// variants.
var routeSuffixes = map[string]bool{"free": true, "nitro": true, "floor": true}

// thinkingSuffixes are the `:word` endings that name how hard a model thinks
// on a call — the same weights, asked differently.
var thinkingSuffixes = map[string]bool{"low": true, "medium": true, "high": true, "minimal": true, "max": true}

// vendorOrgs are Hugging Face organisations whose models the catalog lists
// under another vendor word. A TABLE OF FACTS: each is the organisation that
// publishes the weights and the vendor word OpenRouter spells for it.
var vendorOrgs = map[string]string{
	"zai-org":     "z-ai",
	"deepseek-ai": "deepseek",
	"moonshot":    "moonshotai",
	"meta":        "meta-llama",
}

// familyVendors give a bare model name — a provider that hosts it without an
// owner in the id, as Fireworks and Ollama do — the vendor its family belongs
// to. A TABLE OF FACTS about which company names its models this way; a name
// no entry starts is left without a vendor and stays its own model.
var familyVendors = []struct{ prefix, vendor string }{
	{"glm-", "z-ai"},
	{"kimi-", "moonshotai"},
	{"deepseek-", "deepseek"},
	{"qwen", "qwen"},
	{"gpt-oss", "openai"},
	{"llama-", "meta-llama"},
	{"mistral-", "mistralai"},
	{"devstral", "mistralai"},
	{"codestral", "mistralai"},
	{"minimax-", "minimax"},
	{"gemma-", "google"},
}

// modelAliases are known mismatches no rule above reads: a provider's own name
// for a model the catalog lists under another. A TABLE OF FACTS, each entry
// added when a provider was seen to spell a model this way — never a guess
// that two names probably mean one model.
var modelAliases = map[string]string{
	// Together serves Llama 3.3 70B under a "-turbo" name for the same
	// instruct weights the catalog lists.
	"meta-llama/llama-3.3-70b-instruct-turbo": "meta-llama/llama-3.3-70b-instruct",
}

var (
	poolIndexOnce sync.Once
	poolIndex     *index.Index
)

// poolAliases is the embedded pool index, read once; nil when it will not
// parse, which leaves its alternates unread and nothing else changed.
func poolAliases() *index.Index {
	poolIndexOnce.Do(func() {
		if x, err := index.SeedIndex(); err == nil {
			poolIndex = x
		}
	})
	return poolIndex
}

// Canonical is the id a model's evidence is kept under, across every
// provider's spelling of it ([CanonicalOf] without the variant).
func Canonical(id string) string { return CanonicalOf(id).ID }

// canonMemo holds what [CanonicalOf] has read, because a decision asks the
// same few hundred ids several times each and the answer never changes. It
// is bounded: past canonMemoMax ids it stops remembering new ones.
var canonMemo = struct {
	sync.RWMutex
	ids map[string]Canon
}{ids: map[string]Canon{}}

const canonMemoMax = 8192

// CanonicalOf reads one id as the model it names — see the file comment for
// the rules, in order.
func CanonicalOf(id string) Canon {
	canonMemo.RLock()
	c, ok := canonMemo.ids[id]
	canonMemo.RUnlock()
	if ok {
		return c
	}
	c = canonicalOf(id)
	canonMemo.Lock()
	if len(canonMemo.ids) < canonMemoMax {
		canonMemo.ids[id] = c
	}
	canonMemo.Unlock()
	return c
}

// canonicalOf is [CanonicalOf] without the memo.
func canonicalOf(id string) Canon {
	s := strings.ToLower(strings.TrimSpace(id))
	s = strings.TrimPrefix(s, "~")
	s = strings.TrimPrefix(s, "openrouter/")
	if strings.HasPrefix(s, "accounts/") {
		// accounts/<account>/models/<name>: Fireworks' namespace for a model it
		// hosts, with no owner in it.
		if at := strings.Index(s, "/models/"); at >= 0 {
			s = s[at+len("/models/"):]
		}
	}
	var variant string
	// The `:word` endings, last first: a route, a thinking level, a quant tag.
	for {
		at := strings.LastIndex(s, ":")
		if at <= 0 {
			break
		}
		tail := s[at+1:]
		switch {
		case routeSuffixes[tail], thinkingSuffixes[tail], tail == "latest":
			s = s[:at]
			continue
		case quantTag(tail):
			variant, s = tail, s[:at]
			continue
		}
		break
	}
	if at := strings.LastIndex(s, "-"); at > 0 && (s[at+1:] == "gguf" || quantTag(s[at+1:])) && variant == "" {
		variant, s = s[at+1:], s[:at]
	}
	vendor, name := "", s
	if slash := strings.Index(s, "/"); slash >= 0 {
		vendor, name = s[:slash], s[slash+1:]
	}
	name = versionDots(strings.ReplaceAll(name, "_", "-"))
	if mapped, ok := vendorOrgs[vendor]; ok {
		vendor = mapped
	}
	if vendor == "" {
		for _, f := range familyVendors {
			if strings.HasPrefix(name, f.prefix) {
				vendor = f.vendor
				break
			}
		}
	}
	out := name
	if vendor != "" {
		out = vendor + "/" + name
	}
	if alias, ok := modelAliases[out]; ok {
		out = alias
	}
	if x := poolAliases(); x != nil {
		out = strings.ToLower(x.Canonical(out))
	}
	return Canon{ID: lineageTail(out), Variant: variant}
}

// quantTag is whether a word names a quantisation: q4_k_m, q8_0, iq3_xs,
// fp8, fp16, bf16, int4, int8, awq, gptq.
func quantTag(word string) bool {
	switch {
	case word == "awq" || word == "gptq" || word == "bf16" || word == "gguf":
		return true
	case strings.HasPrefix(word, "fp") && allDigits(word[2:]):
		return true
	case strings.HasPrefix(word, "int") && allDigits(word[3:]):
		return true
	case strings.HasPrefix(word, "iq") && len(word) > 2 && word[2] >= '0' && word[2] <= '9':
		return true
	case strings.HasPrefix(word, "q") && len(word) > 1 && word[1] >= '0' && word[1] <= '9':
		return true
	}
	return false
}

// versionDots reads a version written with a `p` between two digits as the
// dot it stands for: `glm-5p3-flash` is `glm-5.3-flash`.
func versionDots(name string) string {
	if !strings.Contains(name, "p") {
		return name
	}
	b := []byte(name)
	for i := 1; i+1 < len(b); i++ {
		if b[i] == 'p' && b[i-1] >= '0' && b[i-1] <= '9' && b[i+1] >= '0' && b[i+1] <= '9' {
			b[i] = '.'
		}
	}
	return string(b)
}

// DomainTuned is whether a model's own id says it was tuned for one domain
// other than code and general work — finance, medicine, law, a single subject,
// role-play — by a tag among the words of its name. Such a model is a poor
// seat for software work whatever its figures, and a rescue that must take a
// model it knows little about takes a general or a code model first.
func DomainTuned(id string) bool {
	name := strings.ToLower(ShortModel(strings.TrimSuffix(id, ":free")))
	for _, word := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ':' || r == '/'
	}) {
		if domainTags[word] {
			return true
		}
	}
	return false
}

// domainTags are the name words that mark a model tuned for one domain.
var domainTags = map[string]bool{
	"fin": true, "finance": true, "financial": true, "med": true, "medical": true, "medicine": true,
	"bio": true, "biomed": true, "clinical": true, "health": true, "legal": true, "law": true,
	"math": true, "maths": true, "chem": true, "chemistry": true, "roleplay": true, "rp": true,
	"story": true, "storytelling": true, "novel": true, "creative": true, "translate": true,
	"translation": true, "guard": true, "safety": true, "sante": true, "medic": true,
	"pharma": true, "doctor": true, "bank": true, "banking": true, "trading": true, "tax": true,
	"legalbench": true, "romance": true, "companion": true, "erp": true,
}

// Tiny is whether a model's own id gives it fewer parameters than a crew seat
// can use — `lfm-2.5-2.6b`, `gemma-3n-e4b` — read off the largest size tag in
// its name (`30b-a3b` is thirty billion, three active). A name that gives no
// size is not called tiny.
func Tiny(id string) bool {
	name := strings.ToLower(ShortModel(strings.TrimSuffix(id, ":free")))
	largest := 0.0
	for _, word := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == ':' || r == '/'
	}) {
		word = strings.TrimPrefix(word, "e")
		if !strings.HasSuffix(word, "b") {
			continue
		}
		if n, err := strconv.ParseFloat(strings.TrimSuffix(word, "b"), 64); err == nil && n > largest {
			largest = n
		}
	}
	return largest > 0 && largest < tinyBelow
}

// tinyBelow is the fewest billions of parameters a crew seat is given to.
const tinyBelow = 8.0
