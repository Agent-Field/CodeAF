package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/router"
)

// A REFUSAL BELONGS TO THE CREDENTIAL THAT WAS REFUSED.
//
// Route health holds a provider back after a call on it was refused: its key
// (auth) or its account's credit (payment). Until now only a later call that
// answered could lift that, and a seat the person pinned — or the one model for
// every task — is never sent to a route health holds back, so the lift could
// never come: reconnecting, or topping up, changed nothing. Three FACTS lift it
// now, and nothing else does (no timer, no flag the app sets, no success that
// did not happen):
//
//   - A DIFFERENT CREDENTIAL THAN THE ONE REFUSED. A call takes the print of
//     the credential it is sent with when it is sent ([CrewDispatchFor]); a
//     print is sixteen hex digits of a hash, never the credential. A refusal
//     written with a print that is not the credential in force now — a new
//     key, a fresh sign-in — says nothing about it. The same credential
//     written again is the same print, and its refusal stands.
//   - A CREDENTIAL CHANGED AFTER AN UNPRINTED REFUSAL. A refusal written
//     before prints existed, or by a caller that took no dispatch fact, does
//     not say which credential it refused. It is lifted only when the
//     credential in force was seen to change after it was written
//     ([crewCredentialsSeen]): the first print this install records for a
//     provider is a baseline, never a change, so a credential nobody touched
//     stays held back.
//   - A POSITIVE CREDIT READING TAKEN AFTER A PAYMENT REFUSAL. An account read
//     as having credit, at a time after it refused for want of it
//     ([CreditsLowAt]'s record for the key in force, or [CrewAccounts] with its
//     ReadAt). An older reading, or one of none, keeps it held back.
//
// All three are durable and bounded: prints ride the router's own log, which
// is already bounded by its window; the changes are one small record of a
// print and a time per provider; the credit reading is the record the balance
// read already keeps.

// crewCredentialPrint is the print of one connection's credential in force,
// "" for a connection that has none.
func crewCredentialPrint(profileDir string, service modelsource.Connected) string {
	id := strings.ToLower(strings.TrimSpace(service.Source.ID))
	secret := strings.TrimSpace(service.Key)
	if id == "codex" {
		// A sign-in's credential is the account and its refresh token: a new
		// sign-in, or a refresh that succeeded, is a different credential.
		home := service.Home
		if home == "" {
			home = profileDir
		}
		tokens, err := codexauth.Load(home)
		if err != nil {
			return ""
		}
		secret = tokens.AccountID + "\x00" + tokens.RefreshToken
	}
	return provider.CredentialDigest(CrewCredentialDomain(id), secret)
}

// CrewCredentialDomain is the domain a provider's crew credential is printed
// under. An adapter that digests the bearer it actually wrote into a request
// with it ([provider.Client.ObserveRequestCredential]) reports the same print
// [CrewDispatchOf] takes from a connection carrying that key.
func CrewCredentialDomain(id string) string {
	return "crew-credential\x00" + strings.ToLower(strings.TrimSpace(id))
}

// CrewDispatch is the fact a crew call is sent under: the print of the
// credential it carries. A caller takes it as the call is SENT and hands it to
// [LogCrewRouteSent] with the call's outcome, so a credential changed while the
// call was in flight is never blamed for the old one's refusal.
type CrewDispatch struct {
	Cred string
}

// CrewDispatchFor is the dispatch fact of a call about to be sent to provider
// with the credential in force now.
func CrewDispatchFor(profileDir, provider string) CrewDispatch {
	return CrewDispatch{Cred: crewCredentialPrintAt(profileDir, provider)}
}

// CrewDispatchOf is the dispatch fact of a call about to be sent through one
// resolved connection — the one its client holds, which is the credential the
// request will carry even when the settings have moved since it was built.
func CrewDispatchOf(profileDir string, service modelsource.Connected) CrewDispatch {
	return CrewDispatch{Cred: crewCredentialPrint(profileDir, service)}
}

// crewCredentialPrintAt is the print of a provider's credential in force.
func crewCredentialPrintAt(profileDir, provider string) string {
	service, ok := ResolveSources(profileDir, APIKeyAt(profileDir), DefaultBaseURL).ByID(provider)
	if !ok {
		return ""
	}
	return crewCredentialPrint(profileDir, service)
}

// crewCredentialRefusal is a refusal kind that belongs to a credential.
func crewCredentialRefusal(kind string) bool { return kind == "auth" || kind == "payment" }

// crewHeldAgainstNow is the history without the refusals that no longer hold:
// one written with a credential that is not the one in force, one unprinted
// and older than a change of credential, and a payment refusal older than a
// reading that found credit. Everything else — every success, every other kind
// of failure, every refusal of the credential in force — is kept as it was.
func crewHeldAgainstNow(profileDir string, history []router.CrewRouteOutcome, now time.Time) []router.CrewRouteOutcome {
	var facts crewCredentialFacts
	asked := false
	out := history
	copied := false
	for i, o := range history {
		drop := false
		if crewCredentialRefusal(o.Kind) {
			if !asked {
				facts, asked = crewCredentialFactsFor(profileDir, history, now), true
			}
			provider := strings.ToLower(o.Provider)
			seen := facts.seen[provider]
			// A refusal with no time it was made cannot be older than
			// anything: only its print, which needs no time, can lift it.
			dated := !o.At.IsZero()
			switch {
			case o.Cred != "" && facts.prints[provider] != "" && o.Cred != facts.prints[provider]:
				drop = true
			case o.Cred == "" && dated && !seen.Since.IsZero() && o.At.Before(seen.Since):
				drop = true
			}
			if at, ok := facts.credit[provider]; ok && dated && o.Kind == "payment" && at.After(o.At) {
				drop = true
			}
		}
		if drop && !copied {
			out, copied = append([]router.CrewRouteOutcome(nil), history[:i]...), true
		}
		if copied && !drop {
			out = append(out, o)
		}
	}
	return out
}

// CrewRefusalApplies is whether one route failure still holds against the
// route now, by the same facts route health reads ([crewHeldAgainstNow]): an
// auth or payment refusal does not when a known credential in force is not the one
// it refused, when the credential changed after an unprinted one, or, for
// payment, when credit was read after it. Every other kind of failure — a rate
// limit, a route gone, a timeout — still applies: no fact here says otherwise.
// A failure with no time it was made is lifted only by its print. It reads only
// the profile's facts about failure's own provider, and keeps nothing.
func CrewRefusalApplies(profileDir string, failure router.CrewRouteOutcome) bool {
	if !crewCredentialRefusal(failure.Kind) {
		return true
	}
	return len(crewHeldAgainstNow(profileDir, []router.CrewRouteOutcome{failure}, time.Now())) == 1
}

// crewCredentialFacts are, for each provider a refusal in history names, the
// print of its credential in force, when that credential was first seen in
// force, and when its account was last read as having credit.
type crewCredentialFacts struct {
	prints map[string]string
	seen   map[string]crewCredentialSeen
	credit map[string]time.Time
}

// crewCredentialFactsFor reads the facts. Only the providers a refusal names
// are asked, and a change of their credential is recorded on the way.
func crewCredentialFactsFor(profileDir string, history []router.CrewRouteOutcome, now time.Time) crewCredentialFacts {
	facts := crewCredentialFacts{prints: map[string]string{}, credit: map[string]time.Time{}}
	set := ResolveSources(profileDir, APIKeyAt(profileDir), DefaultBaseURL)
	var providers []string
	for _, o := range history {
		provider := strings.ToLower(o.Provider)
		if !crewCredentialRefusal(o.Kind) {
			continue
		}
		if _, asked := facts.prints[provider]; asked {
			continue
		}
		providers = append(providers, provider)
		facts.prints[provider] = ""
		if service, ok := set.ByID(provider); ok {
			facts.prints[provider] = crewCredentialPrint(profileDir, service)
		}
		if provider == modelsource.DefaultID {
			if r := creditsAt(profileDir); r.Known && !r.Low && !r.Expired && r.Key == CreditsKeyPrint(APIKeyAt(profileDir)) && !r.ReadAt.IsZero() {
				facts.credit[provider] = r.ReadAt
			}
		}
		if CrewAccounts != nil {
			// A balance with no time it was read is no evidence about a
			// refusal: it could be the balance from before it.
			if state, ok := CrewAccounts(profileDir, provider); ok && state.CreditKnown && state.CreditUSD > 0 && !state.ReadAt.IsZero() {
				if at, held := facts.credit[provider]; !held || state.ReadAt.After(at) {
					facts.credit[provider] = state.ReadAt
				}
			}
		}
	}
	facts.seen = crewCredentialsNoted(profileDir, facts.prints, now)
	return facts
}

// THE CREDENTIAL CHANGES THIS INSTALL HAS SEEN, one entry per provider: the
// print in force and when it was first seen in force. The first entry for a
// provider is a BASELINE (Since zero) — the credential was already there, and
// nothing says it changed. A later reading of another print is a change, dated
// when it was seen. It is written as a credential is written — a key pasted, a
// connection's row, a sign-in, a key the app hands over ([crewCredentialWrite])
// — and read where route health already reads the refusals it would lift, never
// on a timer. It holds no credential. Bounded by [crewCredentialsMax] providers.

// crewCredentialsFile is where the changes are kept, beside the profile.
const crewCredentialsFile = "crew-credentials.json"

// crewCredentialsMax bounds the record: more providers than any install
// connects. The oldest-changed entries are dropped past it.
const crewCredentialsMax = 64

// crewCredentialSeen is one provider's entry.
type crewCredentialSeen struct {
	Print string    `json:"print"`
	Since time.Time `json:"since,omitzero"`
}

// crewCredentialsMu serializes this process's read-and-write of the record.
var crewCredentialsMu sync.Mutex

// crewCredentialsSeen records the credentials in force now for providers and
// answers the record.
func crewCredentialsSeen(profileDir string, providers []string, now time.Time) map[string]crewCredentialSeen {
	prints := map[string]string{}
	set := ResolveSources(profileDir, APIKeyAt(profileDir), DefaultBaseURL)
	for _, provider := range providers {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			continue
		}
		prints[provider] = ""
		if service, ok := set.ByID(provider); ok {
			prints[provider] = crewCredentialPrint(profileDir, service)
		}
	}
	return crewCredentialsNoted(profileDir, prints, now)
}

// crewCredentialWrite runs write — a change of the profile's credentials — and
// records what it changed, so a reconnect is a fact the moment it is made, not
// only once something reads route health: the prints in force before are
// recorded first (as the baseline, where none was), then those after, and any
// that moved is a change dated now. Writing the same credential again records
// nothing.
func crewCredentialWrite(profileDir string, write func() error) error {
	crewCredentialsNoted(profileDir, crewCredentialPrintsNow(profileDir), time.Time{})
	err := write()
	crewCredentialsNoted(profileDir, crewCredentialPrintsNow(profileDir), time.Now())
	return err
}

// crewCredentialPrintsNow is the print of every connected provider's
// credential in force.
func crewCredentialPrintsNow(profileDir string) map[string]string {
	prints := map[string]string{}
	for _, service := range ResolveSources(profileDir, APIKeyAt(profileDir), DefaultBaseURL).All() {
		if print := crewCredentialPrint(profileDir, service); print != "" {
			prints[strings.ToLower(strings.TrimSpace(service.Source.ID))] = print
		}
	}
	return prints
}

// crewCredentialsNoted folds prints read now into the record, writes it when
// anything moved, and answers it. A provider with no credential now is not
// written: disconnecting is not a new credential, and connecting the refused
// one again must not look like one.
func crewCredentialsNoted(profileDir string, prints map[string]string, now time.Time) map[string]crewCredentialSeen {
	crewCredentialsMu.Lock()
	defer crewCredentialsMu.Unlock()
	path := ProfilePath(profileDir, crewCredentialsFile)
	record := map[string]crewCredentialSeen{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &record)
	}
	moved := false
	for provider, print := range prints {
		if print == "" {
			continue
		}
		was, held := record[provider]
		switch {
		case !held:
			record[provider] = crewCredentialSeen{Print: print}
			moved = true
		case was.Print != print:
			record[provider] = crewCredentialSeen{Print: print, Since: now.UTC()}
			moved = true
		}
	}
	if moved {
		for len(record) > crewCredentialsMax {
			oldest := ""
			for provider, seen := range record {
				if oldest == "" || seen.Since.Before(record[oldest].Since) {
					oldest = provider
				}
			}
			delete(record, oldest)
		}
		writeCrewCredentials(path, record)
	}
	return record
}

// writeCrewCredentials replaces the record atomically. A write that fails
// leaves the old record, which only ever holds more back, never less.
func writeCrewCredentials(path string, record map[string]crewCredentialSeen) {
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".crew-credentials-*.json")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return
	}
	if tmp.Close() != nil {
		return
	}
	_ = os.Rename(tmp.Name(), path)
}
