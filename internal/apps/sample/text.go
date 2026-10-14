package sample

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

var names = []string{"Priya Shah", "Alex Chen", "Mona Patel", "Sam Rivera", "Jordan Lee", "Casey Morgan", "Noah Wilson", "Amara Okafor", "Sofia Garcia", "Theo Martin", "Avery Kim", "Leila Hassan"}

// Organisation names pair a neutral place or nature word with a business
// suffix; neither list leans on any one industry.
var orgStems = []string{"Harbor", "Maple", "Summit", "Bright Path", "Copper", "Riverside", "Lumen", "Oakline", "Silverline", "Fairview", "Kestrel", "Granite"}
var orgSuffixes = []string{"Group", "Co.", "Partners", "Collective", "Trading", "Studio"}

var cities = []string{"Toronto", "Austin", "Lisbon", "Melbourne", "Denver", "Dublin", "Portland", "Montreal", "Edinburgh", "Seattle", "Barcelona", "Chicago"}
var roles = []string{"Operations Lead", "Product Manager", "Account Executive", "Designer", "Office Manager", "Finance Director", "Marketing Lead", "Support Specialist", "Engineer", "Founder", "Coordinator", "Analyst"}

// Notes are short, neutral status sentences; longtext joins one to the title.
var notes = []string{
	"Kicked off last week and the first draft is with the team.",
	"Waiting on a reply before the next step.",
	"On track; check the timeline again on Friday.",
	"Needs one more pass on the details before sign-off.",
	"Shared with the team for feedback.",
	"Moved up after the last check-in.",
	"Blocked until the open questions are answered.",
	"Wrapped up the main work; only small follow-ups remain.",
}

func label(display, name string) string {
	if strings.TrimSpace(display) != "" {
		return display
	}
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	return strings.Join(words, " ")
}

func hasWord(text string, words ...string) bool {
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) }) {
		for _, word := range words {
			if token == word {
				return true
			}
		}
	}
	return false
}

func personLike(text string) bool {
	return hasWord(text, "person", "people", "contact", "candidate", "member", "employee", "staff", "teacher", "instructor", "trainer", "student", "client", "customer", "patient", "volunteer", "attendee", "participant", "author", "assignee", "owner", "manager", "lead", "guest", "tenant", "player", "coach", "mentor", "speaker", "applicant", "user", "writer", "freelancer", "donor", "therapist", "doctor", "nurse", "driver", "musician", "artist", "host", "tutor", "technician", "photographer", "interviewer", "reviewer", "editor", "performer", "singer", "worker", "contractor", "bandmate", "interviewee", "subscriber", "resident", "visitor", "rider", "runner", "clinician", "practitioner", "stylist", "barber", "chef", "cook", "farmer", "handler")
}

func orgLike(text string) bool {
	return hasWord(text, "company", "companies", "organization", "organisation", "org", "account", "vendor", "supplier", "partner", "brand", "agency", "business", "firm", "sponsor", "publisher", "manufacturer", "distributor", "employer", "studio", "venue")
}

func lastWord(text string) string {
	words := strings.Fields(label("", text))
	if len(words) == 0 {
		return ""
	}
	return words[len(words)-1]
}

func personTitle(text string) bool { return personLike(lastWord(text)) }
func orgTitle(text string) bool    { return orgLike(lastWord(text)) }

func personField(name, display string) bool {
	if personTitle(name) || personTitle(display) {
		return true
	}
	semantic := name + " " + display
	return hasWord(semantic, "name") && (personLike(semantic) || hasWord(semantic, "full"))
}

func orgField(name, display string) bool {
	return orgTitle(name) || orgTitle(display)
}

func personName(i int) string {
	parts := strings.Fields(names[i%len(names)])
	// Combining the same vocabulary keeps larger previews distinct without
	// turning people into numbered labels.
	last := strings.Fields(names[(i/len(names)+i)%len(names)])[1]
	if i < len(names)*len(names) {
		return parts[0] + " " + last
	}
	cycle := i/(len(names)*len(names)) - 1
	return fmt.Sprintf("%s %c.%c. %s", parts[0], 'A'+cycle/26, 'A'+cycle%26, last)
}

func orgName(i int) string {
	stem := orgStems[i%len(orgStems)]
	suffix := orgSuffixes[(i/len(orgStems)+i)%len(orgSuffixes)]
	if i < len(orgStems)*len(orgSuffixes) {
		return stem + " " + suffix
	}
	cycle := i/(len(orgStems)*len(orgSuffixes)) - 1
	return fmt.Sprintf("%s %s %s", stem, letters(cycle), suffix)
}

// letters is a counter spelt in capitals, for previews past every vocabulary.
func letters(n int) string {
	return string([]byte{byte('A' + n/26%26), byte('A' + n%26)})
}

// slug keeps the letters and digits of text, joined by sep.
func slug(text, sep string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	return strings.Join(words, sep)
}

// orgDomain is a reserved example domain (RFC 2606) shaped like the company.
func orgDomain(i int) string {
	return slug(strings.Fields(orgName(i))[0], "") + ".example"
}

func emailFor(person string, i int) string {
	return slug(person, ".") + "@" + orgDomain(i)
}

// textHints maps field vocabulary to a generator; the first match wins.
var textHints = []struct {
	words []string
	value func(i int) string
}{
	{[]string{"city", "town", "location", "region"}, func(i int) string { return cities[i%len(cities)] }},
	{[]string{"role", "position", "job"}, func(i int) string { return roles[i%len(roles)] }},
	{[]string{"company", "organization", "organisation", "employer", "vendor", "brand", "agency"}, orgName},
	{[]string{"email"}, func(i int) string { return emailFor(personName(i), i) }},
	{[]string{"website", "url", "link", "site"}, func(i int) string { return "https://www." + orgDomain(i) }},
	{[]string{"phone", "mobile"}, func(i int) string { return fmt.Sprintf("+1 416 555 %04d", i+100) }},
	{[]string{"address", "street"}, func(i int) string {
		return fmt.Sprintf("%d %s Street, %s", 12+i*7, orgStems[i%len(orgStems)], cities[i%len(cities)])
	}},
}

func textValue(typ, name string, schema *spec.RecordType, f *spec.Field, i int) string {
	semantic := name + " " + f.Label
	person := personName(i)
	if hasWord(semantic, "first", "given") && hasWord(semantic, "name") {
		return strings.Fields(person)[0]
	}
	if hasWord(semantic, "last", "family", "surname") && hasWord(semantic, "name", "surname") {
		return strings.Fields(person)[len(strings.Fields(person))-1]
	}
	if personField(name, f.Label) {
		return person
	}
	if orgField(name, f.Label) {
		return orgName(i)
	}
	if hasWord(semantic, "sku", "code", "serial", "identifier") {
		return fmt.Sprintf("%s-%04d", strings.ToUpper(typ), i+1)
	}
	for _, hint := range textHints {
		if hasWord(semantic, hint.words...) {
			return hint.value(i)
		}
	}
	if hasWord(semantic, "number", "tag", "reference", "badge", "plate", "ticket", "invoice") {
		return fmt.Sprintf("%s-%04d", strings.ToUpper(slug(typ, "-")), i+1)
	}
	return sentence(notes[i%len(notes)])
}

// noteValue is one or two sentences about the record, naming its title.
func noteValue(title string, i int) string {
	if title == "" {
		return notes[i%len(notes)]
	}
	return title + ": " + lowerFirst(notes[i%len(notes)])
}

func sentence(text string) string {
	return strings.TrimSuffix(text, ".")
}

func lowerFirst(text string) string {
	if text == "" {
		return text
	}
	r := []rune(text)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func upperFirst(text string) string {
	if text == "" {
		return text
	}
	r := []rune(text)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
