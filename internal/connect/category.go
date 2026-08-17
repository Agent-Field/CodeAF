package connect

// The word each catalog service is filed under, written down by hand.
//
// ── WHY THIS IS A LIST AND NOT A LOOKUP ──
//
// The catalog (catalog.go) knows where a service answers, how its key rides on
// a request and what it is called, and it knows NOTHING about what it is for:
// the only label it carries is `experimental`. So the one fact a person browsing
// two hundred services actually navigates by — is this a helpdesk or a payment
// processor — has to be written here, once, by somebody who knows.
//
// That makes this file DATA, and it is kept in a file of its own for the reason
// catalog.go is kept away from everything else: a list of facts about other
// people's products is a thing you edit, argue about and regenerate, and the
// code that reads it should never have to move when it changes.
//
// ── THE WORDS ARE A CLOSED LIST ──
//
// Thirteen of them, below, and a category outside that list is a mistake caught
// by a test rather than a fourteenth heading that appears on somebody's screen
// because a line here was typed in the plural. They are the words a person would
// use — "billing", "calls & meetings" — not a taxonomy: nobody looking for
// Stripe thinks "financial services", and a heading nobody thinks in is a
// heading they read past.
//
// A service is filed under WHAT SOMEBODY OPENS IT FOR, not under what it is
// built on. Aircall and Fireflies are both "calls & meetings" though one is a
// phone system and the other writes notes, because a person reaching for either
// is reaching for the same half hour of their week.
//
// ── AND WHY THERE IS NO "other" HERE ──
//
// Every service the catalog hands us has a word in this file, and a test says
// so. The readers (internal/tui3) file an uncategorized service under "other"
// because a build whose catalog outran this list must still draw something
// honest — but an "other" heading in front of a person is this file being out of
// date, not a category, so nothing here is ever deliberately left blank.

// The thirteen words. They are constants so that a line below cannot quietly
// invent a fourteenth heading by adding an s.
const (
	categoryCRM           = "crm"
	categorySupport       = "support"
	categoryBilling       = "billing"
	categoryAccounting    = "accounting"
	categoryMarketing     = "marketing"
	categoryOutreach      = "sales & outreach"
	categoryCalls         = "calls & meetings"
	categoryAnalytics     = "analytics"
	categoryHR            = "hr & recruiting"
	categoryDeveloper     = "developer"
	categoryProductivity  = "productivity"
	categoryCommunication = "communication"
	categoryCommerce      = "e-commerce"
)

// categories is every word this build files a service under, for the test that
// walks the catalog and for anybody adding a line to the map below.
var categories = []string{
	categoryCRM, categorySupport, categoryBilling, categoryAccounting,
	categoryMarketing, categoryOutreach, categoryCalls, categoryAnalytics,
	categoryHR, categoryDeveloper, categoryProductivity, categoryCommunication,
	categoryCommerce,
}

// serviceCategories is every service the catalog opens with a key, filed.
//
// THE KEYS ARE THE CATALOG'S OWN SPELLING — `accuLynx`, `ringOverEU` — because
// that is what a person adding a line reads off the catalog, and a folded key
// would be a second spelling of an id this package already has one reading of.
var serviceCategories = map[string]string{
	"accuLynx":              categoryCRM,
	"activeCampaign":        categoryMarketing,
	"aircall":               categoryCalls,
	"amplemarket":           categoryOutreach,
	"amplitude":             categoryAnalytics,
	"anthropic":             categoryDeveloper,
	"apollo":                categoryOutreach,
	"ashby":                 categoryHR,
	"avoma":                 categoryCalls,
	"bird":                  categoryCommunication,
	"blueshift":             categoryMarketing,
	"blueshiftEU":           categoryMarketing,
	"braintree":             categoryBilling,
	"braze":                 categoryMarketing,
	"breakcold":             categoryCRM,
	"breezy":                categoryHR,
	"brevo":                 categoryMarketing,
	"callRail":              categoryCalls,
	"chargeOver":            categoryBilling,
	"chargebee":             categoryBilling,
	"chartMogul":            categoryAnalytics,
	"chilipiper":            categoryCalls,
	"chorus":                categoryCalls,
	"clari":                 categoryOutreach,
	"cloudTalk":             categoryCalls,
	"coda":                  categoryProductivity,
	"connectWise":           categorySupport,
	"copper":                categoryCRM,
	"crunchbase":            categoryOutreach,
	"customerDataPipelines": categoryMarketing,
	"customerJourneysApp":   categoryMarketing,
	"customerJourneysTrack": categoryMarketing,
	"delighted":             categorySupport,
	"dixa":                  categorySupport,
	"dovetail":              categoryAnalytics,
	"emailBison":            categoryOutreach,
	"fastSpring":            categoryCommerce,
	"fathom":                categoryCalls,
	"fireflies":             categoryCalls,
	"flatfile":              categoryDeveloper,
	"freshchat":             categorySupport,
	"freshdesk":             categorySupport,
	"freshsales":            categoryCRM,
	"freshservice":          categorySupport,
	"front":                 categoryCommunication,
	"g2":                    categoryMarketing,
	"geckoboard":            categoryAnalytics,
	"gladly":                categorySupport,
	"gladlyQA":              categorySupport,
	"granola":               categoryCalls,
	"greenhouseJobBoard":    categoryHR,
	"guru":                  categoryProductivity,
	"happyfox":              categorySupport,
	"heyreach":              categoryOutreach,
	"hightouch":             categoryDeveloper,
	"hive":                  categoryProductivity,
	"housecallPro":          categoryCRM,
	"hunter":                categoryOutreach,
	"insightly":             categoryCRM,
	"instantly":             categoryOutreach,
	"instantlyAI":           categoryOutreach,
	"iterable":              categoryMarketing,
	"jotform":               categoryProductivity,
	"jump":                  categoryCalls,
	"justCall":              categoryCalls,
	"kaseyaVSAX":            categorySupport,
	"lemlist":               categoryOutreach,
	"livestorm":             categoryCalls,
	"loxo":                  categoryHR,
	"mailgun":               categoryCommunication,
	"maxio":                 categoryBilling,
	"mixmax":                categoryOutreach,
	"mixpanel":              categoryAnalytics,
	"monaco":                categoryCRM,
	"monday":                categoryProductivity,
	"nutshell":              categoryCRM,
	"odoo":                  categoryAccounting,
	"openAI":                categoryDeveloper,
	"outplay":               categoryOutreach,
	"paddle":                categoryBilling,
	"paddleSandbox":         categoryBilling,
	"pipeliner":             categoryCRM,
	"pylon":                 categorySupport,
	"rebilly":               categoryBilling,
	"recurly":               categoryBilling,
	"revenueCat":            categoryBilling,
	"ringOverEU":            categoryCalls,
	"ringOverUS":            categoryCalls,
	"salesfinity":           categoryCalls,
	"salesflare":            categoryCRM,
	"segment":               categoryAnalytics,
	"sendGrid":              categoryCommunication,
	"smartlead":             categoryOutreach,
	"solarWindsServiceDesk": categorySupport,
	"stripe":                categoryBilling,
	"superSend":             categoryOutreach,
	"vtiger":                categoryCRM,
	"wealthbox":             categoryCRM,
	"whereby":               categoryCalls,
}

// categoryOf files one service, and answers with NOTHING for a service nobody
// has filed — the emptiness law, and the honest reading of a catalog that has
// moved on since this file was last read: an invented word here would put a
// service under a heading a person then cannot find it under again.
func categoryOf(id string) string { return serviceCategories[id] }
