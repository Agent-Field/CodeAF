package connect

// The catalog: a few hundred services aforge can reach the moment a person
// pastes a key, taken from Ampersand's connectors project.
//
// ── THIS FILE IS THE ONLY PLACE THAT NAME APPEARS ──
//
// Everything past this file sees plugs, [Service] values and plain strings.
// Nothing else in this package, and nothing at all outside it, imports the
// catalog or names one of its types. That is the whole point of a wrapper: the
// catalog is a list of facts about other people's services, and a list of facts
// is a thing you swap — for a newer pin, for a hand-written file, for a
// different project entirely — without touching a line of the code that uses
// it. A catalog type that leaked into a plug interface, a store entry or a tool
// description would make that swap a rewrite.
//
// ── WHAT IS TAKEN AND WHAT IS LEFT ──
//
// Only the services a KEY opens: the ones the catalog marks as taking a key on
// a header or a query parameter, and the ones that take the older name-and-
// password shape. The services that need a trip through a browser are left
// where they are — that trip needs an application registered in somebody's
// developer console first, which is a thing a person does once and deliberately
// (google.go is the one this build ships), not a thing a catalog can hand out.
//
// A service the catalog cannot describe well enough to use is skipped rather
// than half-built: no name to call it by, no address that parses, no rule for
// where the key goes, or more than one blank in its address than one answer can
// fill. Skipping is quiet. A menu that offered a service and then failed at the
// first call would be worse than a menu that never mentioned it.

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	amp "github.com/amp-labs/connectors/providers"

	"github.com/Agent-Field/aforge-v2/internal/approval"
)

// blankPattern is the one shape the catalog writes a missing piece of an
// address in: a name in double braces, as a template would.
var blankPattern = regexp.MustCompile(`{{\s*\.([A-Za-z0-9_]+)\s*}}`)

// blank is the one piece of a service's address the catalog cannot know,
// because it is a fact about the person's own account rather than about the
// service — the workspace in https://<workspace>.freshdesk.com.
type blank struct {
	// name is what the address calls it: "workspace", "region", "subdomain".
	name string
	// label is what a person calls it — "Domain", "Site name" — as the
	// catalog spells it for its own screens. It falls back to name.
	label string
}

// the three places a key rides on a request, which is the whole of what differs
// between one keyed service and the next.
const (
	carryHeader = "header"
	carryQuery  = "query"
	carryBasic  = "basic"
)

// carry is how one service wants its key attached.
type carry struct {
	// kind is one of the three above.
	kind string
	// name is the header's name or the query parameter's name.
	name string
	// prefix goes in front of the key inside a header — "Bearer", "Token".
	prefix string
	// format shapes a name-and-password pair out of a single key, with the
	// key going where the %s is. It is empty for the services that simply
	// want the key as the name.
	format string
}

// probe is the cheap read that proves a key works: an address that changes
// nothing, and the answers that count as yes. A service the catalog names none
// for has an empty one, and a key for it is believed until the first real call.
type probe struct {
	address string
	method  string
	accepts []int
}

// keyPlug is one catalog service as a plug.
//
// It sits on the SAME registry google.go sits on. There is one registry, one
// [Manager.Services], one store file and one [Manager.Disconnect], and a
// service from the catalog is not a second kind of thing the rest of the
// program has to learn about — it is a plug whose [Service] says [AuthKey].
type keyPlug struct {
	service Service
	// base is the address as the catalog gives it, with the blank still in
	// it. The finished address is [keyPlug.address].
	base  string
	blank blank
	carry carry
	probe probe
}

// init registers every catalog service as a plug AND as two sentences a person
// can say yes, ask or off to.
//
// ── ONE TOOL, TWO CAPABILITIES ──
//
// A key service brings exactly one tool — the raw call (key.go's
// [Manager.Request]) — and that one tool is both halves of the pair depending on
// the verb it is handed: a GET reads, and everything else writes at the far end
// in the person's name. The map below can only point a tool at one capability,
// so it points at the STRICTER half, and the seam that judges a call picks the
// other one when the verb is a read (internal/session's consent.go). Pointing at
// `read` instead would have made the safe answer the one that needs the extra
// step, which is the wrong way round for a table somebody may read in a hurry.
func init() {
	for _, plug := range catalogPlugs() {
		id := plug.Service().ID
		Register(plug)
		RegisterGenericCapabilities(id, map[string]string{serviceRequestTool(id): CapabilityAct})
	}
}

// serviceRequestTool is what a key service's one tool is called — its own id and
// the suffix the consent policy matches these calls on.
//
// THE SUFFIX IS READ FROM ITS OWNER rather than spelled again here. Three
// packages have to agree about this name: internal/session builds it when it
// arms the tool, internal/approval matches it when it judges a call, and this
// file declares what it may be used for. Two of them already read the constant;
// a third spelling of "_request" is a rename waiting to break exactly one of
// them silently. internal/approval is policy with no dependencies of its own, so
// naming it here costs this package nothing it did not already have.
func serviceRequestTool(id string) string { return id + approval.ServiceRequestSuffix }

// catalogPlugs reads the catalog once, at start-up, and turns every service a
// key opens into a plug of ours. The order is the catalog's names sorted, so
// that two builds of the same binary register the same list in the same order.
func catalogPlugs() []Plug {
	names := amp.AllNames()
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	plugs := make([]Plug, 0, len(names))
	for _, name := range names {
		info, err := amp.ReadInfo(name)
		if err != nil || info == nil {
			continue
		}
		if plug, ok := catalogPlug(string(name), info); ok {
			plugs = append(plugs, plug)
		}
	}
	return plugs
}

// catalogPlug turns one catalog entry into a plug, or says it cannot.
func catalogPlug(id string, info *amp.ProviderInfo) (*keyPlug, bool) {
	id = strings.TrimSpace(id)
	name := strings.TrimSpace(info.DisplayName)
	if id == "" || name == "" {
		// Nothing to call it. A service nobody can name is a service nobody
		// can ask for, and a row on a menu reading "" helps no one.
		return nil, false
	}
	how, ok := catalogCarry(info)
	if !ok {
		return nil, false
	}
	base, hole, ok := catalogAddress(info)
	if !ok {
		return nil, false
	}
	display := shown(base, hole)
	return &keyPlug{
		service: Service{
			ID:   id,
			Name: name,
			// The word this service is browsed by, from the one place that
			// knows it: category.go, because the catalog itself says nothing
			// about what any of these are for.
			Category: categoryOf(id),
			Blurb:    catalogBlurb(name, display, hole),
			KeyAsk:   catalogAsk(hole),
			KeyHint:  catalogKeyHint(id, info),
			Auth:     AuthKey,
			Address:  display,
		},
		base:  base,
		blank: hole,
		carry: how,
		probe: catalogProbe(info),
	}, true
}

// catalogCarry reads where this service wants the key put. It is the one piece
// of catalog knowledge the signing transport (key.go) needs, and it is copied
// out into our own small value here so that the transport never sees a catalog
// type.
func catalogCarry(info *amp.ProviderInfo) (carry, bool) {
	switch info.AuthType {
	case amp.ApiKey:
		opts := info.ApiKeyOpts
		if opts == nil {
			return carry{}, false
		}
		switch opts.AttachmentType {
		case amp.Header:
			if opts.Header == nil || strings.TrimSpace(opts.Header.Name) == "" {
				return carry{}, false
			}
			return carry{
				kind:   carryHeader,
				name:   strings.TrimSpace(opts.Header.Name),
				prefix: strings.TrimSpace(opts.Header.ValuePrefix),
			}, true
		case amp.Query:
			if opts.Query == nil || strings.TrimSpace(opts.Query.Name) == "" {
				return carry{}, false
			}
			return carry{kind: carryQuery, name: strings.TrimSpace(opts.Query.Name)}, true
		}
		return carry{}, false
	case amp.Basic:
		how := carry{kind: carryBasic}
		if info.BasicOpts != nil && info.BasicOpts.ApiKeyAsBasicOpts != nil {
			how.format = strings.TrimSpace(info.BasicOpts.ApiKeyAsBasicOpts.KeyFormat)
		}
		return how, true
	}
	return carry{}, false
}

// catalogAddress works out where the service answers, and what — if anything —
// it cannot know until it asks the person.
//
// A BLANK THE CATALOG CAN FILL ITSELF IS FILLED HERE. Some addresses carry a
// region or a subdomain with an ordinary value written down beside it; those are
// filled in now and never mentioned again, because a question with a known
// answer is a question nobody should be asked. What is left is at most one
// blank, and a service with two of them is skipped: one answer fills one blank,
// and inventing a syntax for two would be inventing a syntax a person has to
// learn.
func catalogAddress(info *amp.ProviderInfo) (string, blank, bool) {
	base := strings.TrimSpace(info.BaseURL)
	if base == "" {
		return "", blank{}, false
	}
	var open []blank
	for _, name := range blanksIn(base) {
		label, value := catalogInput(info, name)
		if value != "" {
			base = fill(base, name, value)
			continue
		}
		open = append(open, blank{name: name, label: label})
	}
	if len(open) > 1 {
		return "", blank{}, false
	}
	var hole blank
	if len(open) == 1 {
		hole = open[0]
	}
	// The address has to be a real one once the blank is filled. A catalog
	// entry that is a scheme with nothing after it — and there is one — is
	// not something a call can be made against.
	if !addressable(fill(base, hole.name, "example")) {
		return "", blank{}, false
	}
	return strings.TrimRight(base, "/"), hole, true
}

// blanksIn lists the distinct blanks in an address, in the order they appear.
func blanksIn(address string) []string {
	var names []string
	seen := make(map[string]bool)
	for _, found := range blankPattern.FindAllStringSubmatch(address, -1) {
		if name := found[1]; !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// fill puts value in every place the named blank appears. An empty name fills
// nothing, which is what a service with no blank wants.
func fill(address, name, value string) string {
	if name == "" {
		return address
	}
	return blankPattern.ReplaceAllStringFunc(address, func(match string) string {
		if found := blankPattern.FindStringSubmatch(match); found != nil && found[1] == name {
			return value
		}
		return match
	})
}

// addressable reports whether a finished address is one a request can be made
// against: a web scheme and a host, nothing more demanded.
func addressable(address string) bool {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// catalogInput finds what the catalog says about one blank: the words it puts
// on its own screens, and the ordinary value if it has one.
func catalogInput(info *amp.ProviderInfo, name string) (string, string) {
	label := name
	if info.Metadata == nil {
		return label, ""
	}
	for _, input := range info.Metadata.Input {
		if input.Name != name {
			continue
		}
		if display := strings.TrimSpace(input.DisplayName); display != "" {
			label = display
		}
		return label, strings.TrimSpace(input.DefaultValue)
	}
	return label, ""
}

// catalogKeyHint is where a person goes to find this service's key.
//
// ── THE HAND-WRITTEN LINE WINS, AND THE CATALOG IS THE FLOOR ──
//
// The catalog carries the vendor's own docs address for most of these services
// and says nothing for about thirty of them, so the two are read in the order
// that answers the person's question best: keyhint.go first, because a line
// written there is either a page the catalog does not know about or the actual
// screen the key is on rather than a page describing one; then whatever the
// catalog has; then nothing at all.
//
// THE ADDRESS IS CHECKED BEFORE IT IS SHIPPED. A catalog entry carrying a
// fragment, a placeholder or something that is not a web address would become a
// link on a screen that goes nowhere, and a link that goes nowhere is worse than
// the emptiness it replaced.
func catalogKeyHint(id string, info *amp.ProviderInfo) string {
	written := strings.TrimSpace(curatedKeyHint(id))
	if written == "" {
		written = strings.TrimSpace(catalogDocs(info))
	}
	if !addressable(written) {
		return ""
	}
	return written
}

// catalogDocs is the vendor's own page about their keys, from whichever half of
// the catalog entry describes how this service is signed.
func catalogDocs(info *amp.ProviderInfo) string {
	if info.ApiKeyOpts != nil && strings.TrimSpace(info.ApiKeyOpts.DocsURL) != "" {
		return info.ApiKeyOpts.DocsURL
	}
	if info.BasicOpts != nil {
		return info.BasicOpts.DocsURL
	}
	return ""
}

// catalogProbe reads the cheap authenticated check the catalog names, if it
// names one. Very few do.
func catalogProbe(info *amp.ProviderInfo) probe {
	check := info.AuthHealthCheck
	if check == nil {
		return probe{}
	}
	address := strings.TrimSpace(check.Url)
	if !addressable(address) {
		return probe{}
	}
	return probe{
		address: address,
		method:  strings.ToUpper(strings.TrimSpace(check.Method)),
		accepts: append([]int(nil), check.SuccessStatusCodes...),
	}
}

// catalogBlurb is the one line a person reads next to the service's name.
//
// It says where the calls go and what it takes to start, and it is built out of
// what the catalog actually knows rather than out of a sentence somebody wrote
// per service — there are hundreds of these, and a hand-written line for each
// would be hundreds of lines nobody maintains.
func catalogBlurb(name, display string, hole blank) string {
	line := "Reach your " + name + " account"
	if host := hostOf(display); host != "" {
		line += " at " + host
	}
	line += ", with a key you already hold."
	if ask := catalogAsk(hole); ask != "" {
		line += " " + ask
	}
	return line
}

// catalogAsk is the instruction a service with a blank in its address needs a
// person to have read, and nothing at all for the rest.
//
// IT IS BUILT HERE AND SPENT TWICE: it is the tail of the blurb on a list, and
// it is the line a screen puts over the box while somebody is answering it
// ([Service.KeyAsk]). Two spellings of one instruction is the kind of pair that
// drifts, and the half that drifts is the half telling somebody what to type.
func catalogAsk(hole blank) string {
	if hole.name == "" {
		return ""
	}
	return "Give the " + strings.ToLower(hole.label) + " and then the key, one space between them."
}

// shown is the address as a person should read it: the blank left as a plain
// word in angle brackets rather than as the catalog's braces, which are
// machinery and say nothing to anybody.
func shown(base string, hole blank) string {
	if hole.name == "" {
		return base
	}
	return fill(base, hole.name, "<"+strings.ToLower(hole.label)+">")
}

// hostOf is the bare host of an address, for a sentence that wants to name
// where something lives without printing a whole path.
//
// It reads the SHOWN address, blank and all, and it is cut by hand rather than
// parsed: a service whose whole address is the person's own reads as "at
// <domain>", which is true, and parsing would have had to put a made-up host
// there to get an answer at all.
func hostOf(display string) string {
	host := display
	if _, rest, found := strings.Cut(host, "://"); found {
		host = rest
	}
	if index := strings.IndexAny(host, "/?#"); index >= 0 {
		host = host[:index]
	}
	return host
}
