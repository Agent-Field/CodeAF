// Package sessionhost supervises private hosted chats on a Linux volume.
// A persisted session owns a dedicated process identity, and only a helper
// already running under that identity may write inside its private directory.
// Each attachment gets a short-lived bridge to the engine, whose independent
// lifetime lets a chat continue while its client is away.
package sessionhost
