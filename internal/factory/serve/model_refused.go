package serve

import (
	"log"
	"sync"
	"time"
)

// hostedRefusalEvery is how long the operator's log stays quiet about a key the
// provider keeps refusing. A dead key refuses every call of every chat, and a
// line per call would bury the one line that says what is wrong.
const hostedRefusalEvery = 30 * time.Minute

// keyRefusals remembers what the operator has already been told about keys the
// model's provider refused, so that a key which is dead is named once and not
// on every call. It holds no key: it holds a time per person and one for the
// organization.
type keyRefusals struct {
	mu  sync.Mutex
	org time.Time            // zero when the organization's key is not known to be refused
	own map[string]time.Time // person to when they were last named
	// logf is the operator's log. Tests replace it.
	logf func(format string, args ...any)
}

func (k *keyRefusals) logLine(format string, args ...any) {
	if k.logf != nil {
		k.logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// refused records that the provider refused a key with the given status, and
// writes ONE operator line the first time, and again only after
// hostedRefusalEvery or after the key has worked once. The line names the cause
// and the setting, never the key. own says whose key it was: a person's own
// sealed key, or the organization's.
func (k *keyRefusals) refused(now time.Time, own bool, person string, status int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if own {
		if k.own == nil {
			k.own = make(map[string]time.Time)
		}
		if last, ok := k.own[person]; ok && now.Sub(last) < hostedRefusalEvery {
			return
		}
		k.own[person] = now
		k.logLine("hosted chats: the model provider refused the own model key of person %s (HTTP %d); their chats pause with a plain sentence until they replace or remove it", person, status)
		return
	}
	if !k.org.IsZero() && now.Sub(k.org) < hostedRefusalEvery {
		return
	}
	k.org = now
	k.logLine("hosted chats: the model provider refused the organization's model key (HTTP %d); every chat that uses it pauses with a plain sentence until the key is replaced (CODEAF_HOSTED_MODEL_KEY, or the organization's keys); this line is repeated at most every %s", status, hostedRefusalEvery)
}

// accepted records that a key worked, so a later refusal is a new outage and
// gets its own line.
func (k *keyRefusals) accepted(own bool, person string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if own {
		delete(k.own, person)
		return
	}
	k.org = time.Time{}
}
