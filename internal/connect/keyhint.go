package connect

// Where a person goes to find their key, written down by hand for the services
// whose vendor does not say.
//
// ── WHY THIS IS A LIST AND NOT A LOOKUP ──
//
// The catalog (catalog.go) carries a docs address for most of the services a key
// opens — the vendor's own page about their keys — and that address is the one
// used wherever it exists, because it comes from the vendor and is regenerated
// with the catalog. About thirty of them say nothing at all, and for those the
// question a person is stuck on ("where IS my key") has no answer anywhere in
// the program. So the answer is written here, once, by somebody who went and
// looked.
//
// That makes this file DATA, kept beside category.go for the same reason
// category.go is kept beside catalog.go: a list of facts about other people's
// products is a thing you edit, argue about and regenerate, and the code that
// reads it should never have to move when it changes.
//
// ── WHAT IS WRITTEN HERE AND WHAT IS NOT ──
//
// A LINK NOBODY CONFIRMED IS NOT SHIPPED. A wrong address sends a person hunting
// through a vendor's site for a page that does not exist, which is strictly
// worse than the nothing they had before — so where the exact page was not
// certain, what is written down is the vendor's own developer or documentation
// root, which is a place their search starts rather than a promise about a page.
// A service nobody could confirm anything for gets no line at all and renders
// nothing.
//
// ── AND THE HANDFUL THAT OVERRIDE THE CATALOG ──
//
// A few household names are listed here even though the catalog has an address
// for them, and each of those is the same trade: the catalog points at a page
// EXPLAINING keys, and the line below points at the page the key is actually ON.
// A person who has opened this box does not want to read about authentication —
// they want the screen with the key on it, in one click.
var serviceKeyHints = map[string]string{
	// ── the dashboards, which beat any page about them ──────────────────────
	"anthropic": "https://console.anthropic.com/settings/keys",
	"coda":      "https://coda.io/account",
	"openAI":    "https://platform.openai.com/api-keys",
	"sendGrid":  "https://app.sendgrid.com/settings/api_keys",
	"stripe":    "https://dashboard.stripe.com/apikeys",

	// ── and the ones the catalog says nothing about ─────────────────────────
	"amplitude":             "https://amplitude.com/docs/apis",
	"ashby":                 "https://developers.ashbyhq.com",
	"blueshift":             "https://developer.blueshift.com",
	"blueshiftEU":           "https://developer.blueshift.com",
	"braintree":             "https://developer.paypal.com/braintree/docs",
	"chargeOver":            "https://developers.chargeover.com",
	"chargebee":             "https://apidocs.chargebee.com",
	"chartMogul":            "https://dev.chartmogul.com",
	"chilipiper":            "https://help.chilipiper.com",
	"chorus":                "https://www.chorus.ai",
	"connectWise":           "https://developer.connectwise.com",
	"delighted":             "https://delighted.com/docs/api",
	"emailBison":            "https://emailbison.com",
	"freshchat":             "https://developers.freshchat.com",
	"freshdesk":             "https://developers.freshdesk.com",
	"freshservice":          "https://api.freshservice.com",
	"geckoboard":            "https://developer.geckoboard.com",
	"gladly":                "https://developer.gladly.com",
	"gladlyQA":              "https://developer.gladly.com",
	"guru":                  "https://developer.getguru.com",
	"heyreach":              "https://heyreach.io",
	"insightly":             "https://api.insightly.com",
	"kaseyaVSAX":            "https://developer.kaseya.com",
	"maxio":                 "https://developers.maxio.com",
	"mixpanel":              "https://developer.mixpanel.com",
	"nutshell":              "https://developers.nutshell.com",
	"outplay":               "https://outplayhq.com",
	"pipeliner":             "https://developers.pipelinersales.com",
	"recurly":               "https://developers.recurly.com",
	"solarWindsServiceDesk": "https://documentation.solarwinds.com/en/success_center/swsd",
}

// curatedKeyHint is what this file says about one service, and nothing for a
// service it says nothing about — the emptiness law, and the honest reading of a
// catalog that has moved on since anybody last read this file.
func curatedKeyHint(id string) string { return serviceKeyHints[id] }
