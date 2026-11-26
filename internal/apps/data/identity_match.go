package data

import (
	"math"
	"slices"
	"strings"
	"unicode"
)

// IdentityFields names the mapped fields allowed to contribute match evidence.
// ExternalID is an authoritative subject ID within the explicit namespace.
type IdentityFields struct {
	Name         string `json:"name,omitempty"`
	Email        string `json:"email,omitempty"`
	Organization string `json:"organization,omitempty"`
	Phone        string `json:"phone,omitempty"`
	Domain       string `json:"domain,omitempty"`
	ExternalID   string `json:"external_id,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
}

func (f *IdentityFields) bind(t *Type) error {
	if (f.ExternalID == "") != (f.Namespace == "") || len(f.Namespace) > 200 {
		return errorf(CodeInvalid, "an external subject ID needs an authoritative namespace")
	}
	for _, name := range []*string{&f.Name, &f.Email, &f.Organization, &f.Phone, &f.Domain, &f.ExternalID} {
		if *name == "" {
			continue
		}
		field := t.field(*name)
		if field == nil || field.Hidden {
			return fieldErr(*name, "identity matching needs a visible common type field")
		}
		*name = field.ID
	}
	return nil
}

type MatchDecision string

const (
	MatchSeparate MatchDecision = "separate"
	MatchReview   MatchDecision = "review"
	MatchLink     MatchDecision = "link"

	matchReviewThreshold = 55
	matchLinkThreshold   = 96
	matchRuleVersion     = 1
)

// MatchSubject is evidence in the space's trusted native visibility domain.
// Only a trusted source can attest a verified work address.
type MatchSubject struct {
	Name, Email, Organization, Phone, Domain, ExternalID, Namespace string
	VerifiedWorkEmail                                               bool
}

// MatchEvidence records the rule and signals without copying the source values.
type MatchEvidence struct {
	Method   string        `json:"method,omitempty"`
	Version  int           `json:"version"`
	Score    int           `json:"score"`
	Signals  []string      `json:"signals"`
	Decision MatchDecision `json:"decision"`
}

func ScoreIdentity(common CommonTypeKey, left, right MatchSubject) MatchEvidence {
	out := MatchEvidence{Version: matchRuleVersion, Decision: MatchSeparate, Signals: []string{}}
	add := func(score int, signal string) {
		out.Score += score
		out.Signals = append(out.Signals, signal)
	}
	a, b := normalizedEmail(left.Email), normalizedEmail(right.Email)
	sameNamespace := equalPresent(left.Namespace, right.Namespace)
	sameOrg := equalPresent(foldIdentity(left.Organization), foldIdentity(right.Organization))
	sameEmail := equalPresent(a, b)
	sharedEmail := sameEmail && sharedMailbox(a)
	verifiedEmail := sameEmail && !sharedEmail && sameOrg && left.VerifiedWorkEmail && right.VerifiedWorkEmail
	switch {
	case sameNamespace && equalPresent(left.ExternalID, right.ExternalID):
		out.Method = "external_id"
		add(100, "authoritative external ID")
	case common == CommonOrganization && equalPresent(normalizedDomain(left.Domain), normalizedDomain(right.Domain)):
		out.Method = "domain"
		add(100, "organization domain")
	case common == CommonPerson && verifiedEmail:
		out.Method = "work_email"
		add(100, "verified work email")
	default:
		if common == CommonPerson && sameEmail {
			add(matchReviewThreshold, "same email")
		}
		aName, bName := foldIdentity(left.Name), foldIdentity(right.Name)
		similarity := nameSimilarity(aName, bName)
		if similarity >= 0.75 {
			add(int(math.Round(40*similarity)), "similar name")
		}
		if sameOrg {
			add(30, "same organization")
		}
		if equalPresent(normalizedPhone(left.Phone, ""), normalizedPhone(right.Phone, "")) {
			add(30, "same phone")
		}
	}
	out.Score = min(out.Score, 100)
	switch {
	case out.Score < matchReviewThreshold:
	case sharedEmail || common == CommonPerson && a != "" && b != "" && a != b ||
		sameNamespace && left.ExternalID != "" && right.ExternalID != "" && left.ExternalID != right.ExternalID:
		out.Decision = MatchReview
		out.Signals = append(out.Signals, "conflicting or shared identity key")
	case common == CommonPerson && sameEmail && !verifiedEmail && out.Method == "":
		out.Decision = MatchReview
	case out.Score >= matchLinkThreshold:
		out.Decision = MatchLink
	default:
		out.Decision = MatchReview
	}
	return out
}

func equalPresent(a, b string) bool { return a != "" && a == b }

func sharedMailbox(email string) bool {
	local, _, _ := strings.Cut(email, "@")
	local, _, _ = strings.Cut(local, "+")
	return slices.Contains([]string{"admin", "billing", "contact", "hello", "help", "info", "office", "sales", "support", "team"}, local)
}

func foldIdentity(s string) string {
	if len(s) > 256 {
		return ""
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func nameSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	x, y := []rune(a), []rune(b)
	n := max(len(x), len(y))
	if min(len(x), len(y))*4 < n*3 {
		return 0
	}
	row := make([]int, len(y)+1)
	for j := range row {
		row[j] = j
	}
	for i, r := range x {
		prev := row[0]
		row[0] = i + 1
		for j, s := range y {
			cost := 1
			if r == s {
				cost = 0
			}
			next := min(prev+cost, row[j]+1, row[j+1]+1)
			prev, row[j+1] = row[j+1], next
		}
	}
	return 1 - float64(row[len(y)])/float64(n)
}

func (f IdentityFields) subject(t *Type, r *record) MatchSubject {
	value := func(id string) string {
		if field := t.byID[id]; field != nil {
			v, _ := r.Data[field.Name].(string)
			return v
		}
		return ""
	}
	return MatchSubject{Name: value(f.Name), Email: value(f.Email), Organization: value(f.Organization),
		Phone: value(f.Phone), Domain: value(f.Domain), ExternalID: value(f.ExternalID), Namespace: f.Namespace}
}

func personConflict(t *Type, a, b *record) bool {
	for _, name := range t.Order {
		av, bv := a.Data[name], b.Data[name]
		ak, bk := a.Meta.By[name], b.Meta.By[name]
		if (personValue(ak) && (bv != nil || bk != "") || personValue(bk) && (av != nil || ak != "")) &&
			canonicalJSON(av) != canonicalJSON(bv) {
			return true
		}
	}
	return false
}

func personValue(kind string) bool { return kind == ByPerson || kind == ByImport }
