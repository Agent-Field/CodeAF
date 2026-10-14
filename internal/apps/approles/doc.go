// Package approles stores explicit per-app membership and the Everyone default.
// NewWithOptions accepts Options.DBPath to share b1's space.db before it exists;
// otherwise New selects ../space.db if present and approles.db in the apps root.
//
// AppMembers carries Everyone separately, before People on the wire. People includes
// the workspace roster with effective roles; len(People) is the Everyone row's count.
// The maker is retained separately so removing an override does not change authorship.
//
// AskEditor uses Audit.Append, never Approvals: send/run approvals are a different
// workflow. Audit detail holds EditorRequest, and space/waiting carries the intended
// member with Count 1 as a notification, not a total Inbox count. The Inbox consumer
// must project these requests and filter waiting events by member. AppSummary has
// no shaped_by in the current shared seam, so selection falls back to an Editor;
// appInfo reads that wire field when Store exposes it. The gateway does not put the
// caller in Role's context, so no-row Personal fallback uses Store's app space.
//
// Role changes commit before the independent Audit.Append and publish immediately.
// An audit failure reaches the caller, but cannot roll back the committed role.
// Global apps.list/apps.create and Viewer agent.send need the gateway's method
// owners to register caps.AllowWorkspaceApps and caps.AllowWorkspaceAgent. Their
// existing nil permissions fall back to wave-1 role checks, which deny those calls;
// a deny-only registry rule cannot grant them, and this lane does not own gateway.
// Gateway currently checks subscriptions only at sub/unsub time; it must recheck
// roles before forwarding events for revocation to stop existing subscriptions.
//
// b1 must pass WorkspaceRoleTarget.Owners from the same transaction that changes
// membership. It can transfer ownership by promoting the successor before demoting
// the former Owner; the intermediate count remains private to that transaction.
package approles
