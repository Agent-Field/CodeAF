// STUB(connect-amp): replaced by the owner branch on merge.
package session

// ResolveConnectKey answers one EventConnectAsk that arrived with NeedsKey set:
// the person pasted a key, or they backed out and the key is empty.
//
// It is the key half of [Agent.ResolveConnect]. A non-empty key is an approval
// carrying the secret with it; an empty key is a decline, and is remembered
// nowhere. Either way the attempt ends in exactly one EventConnectDone, and no
// EventConnectAuth is sent — there is no browser trip in this flow.
func (a *Agent) ResolveConnectKey(id string, key string) {}
