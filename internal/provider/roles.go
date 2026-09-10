package provider

import (
	"context"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── THE ROLE ON THE CONTEXT ─────────────────────────────────────────────────
//
// `internal/lane`'s roles.go holds the table — what a role's second is worth,
// what quality bar it needs, how many calls it expects to make, and whether a
// person is reading its stream. This file is the two lines that carry a role
// from the caller to the funnel.
//
// IT IS A CONTEXT VALUE AND NOT AN OPTION for the reason the routing intent it
// replaces is one: a role belongs to the ERRAND and not to the request, so it
// has to survive being passed through a completer wrapper, a retry, a relax
// rung and a hedge arm without anybody re-stating it. Every one of those has
// been a place a knob was lost before.
//
// A CALL THAT NAMES NO ROLE IS LEGAL AND CONSERVATIVE. It reads as
// [lane.RoleUnknown] — a hidden background errand — because the failure that
// matters is the other way round: a side errand that claimed a person was
// waiting would buy speed with somebody's money and would take the status line
// away from the answer they are actually reading.

type roleContextKey struct{}

// WithRole says who the calls made under ctx are for.
func WithRole(ctx context.Context, role lanes.Role) context.Context {
	if role == "" {
		return ctx
	}
	return context.WithValue(ctx, roleContextKey{}, role)
}

// RoleFrom is the role in force for ctx, [lane.RoleUnknown] when none was said.
func RoleFrom(ctx context.Context) lanes.Role {
	role, _ := ctx.Value(roleContextKey{}).(lanes.Role)
	return role
}

// roleIntent is the old two-valued knob, derived from the role rather than set
// beside it.
//
// [RoutingIntent] is kept because it is what `provider.sort` is built from and
// what a dozen call sites still say, but it is now a READING of the role rather
// than a second opinion about the same fact. Where both are present the role
// wins: it is the more specific claim, and it is the one the table can explain.
func roleIntent(ctx context.Context) (RoutingIntent, bool) {
	role := RoleFrom(ctx)
	if !role.Known() {
		return IntentInteractive, false
	}
	if role.Facts().Interactive {
		return IntentInteractive, true
	}
	return IntentBackground, true
}

// ── THE CONVERSATION ON THE CONTEXT ─────────────────────────────────────────
//
// The role above says WHAT KIND of errand a call is; this says WHOSE. They are
// two stamps rather than one because they answer different questions and change
// at different moments: a conversation runs a talk turn, a naming errand and
// four task nodes, and all six belong to the same person sitting in front of
// the same window.
//
// IT EXISTS FOR ONE READER: an engine that is a SEPARATE PROCESS from the
// surface (internal/enginehost). In one process a phase reader is the one
// window there is, so nothing has to be asked. A host runs many conversations
// down many connections and registers ONE reader for all of them, so every
// piece of news has to say which connection it belongs to or every window would
// draw every other window's clock.
//
// IT IS A CONTEXT VALUE FOR THE ROLE'S REASON: it belongs to the errand, so it
// survives a completer wrapper, a retry, a relax rung and a hedge arm without
// anybody re-stating it. A call that names none is legal — it reads as the
// empty string — and news that names no conversation is news a host cannot
// place, which it drops rather than fans out to everybody.

type sessionContextKey struct{}

// WithSession says which conversation the calls made under ctx belong to.
func WithSession(ctx context.Context, key string) context.Context {
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionContextKey{}, key)
}

// SessionFrom is the conversation in force for ctx, empty when none was said.
func SessionFrom(ctx context.Context) string {
	key, _ := ctx.Value(sessionContextKey{}).(string)
	return key
}
