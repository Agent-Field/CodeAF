// STUB(connect-amp): replaced by the owner branch on merge.
package connect

import "context"

// ConnectKey connects one service from a key the person pasted, rather than
// from a browser trip: the key is verified, the account it belongs to is asked
// for, and the pair is stored exactly as [Manager.BeginAuth]'s result is.
//
// It reaches the network, so a surface calls it from a command and never from
// its model loop.
func (m *Manager) ConnectKey(ctx context.Context, id string, key string) (Status, error) {
	return Status{}, nil
}
