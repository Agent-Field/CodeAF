// Package audit stores the append-only account of calls and projects selected
// entries into Activity. Append assigns the sequence and server time. List is
// for workspace Owners and Admins; Activity includes only apps the member can
// open, plus space-level lines. Both use newest-first sequence cursors, a
// default page of 100 and a maximum of 500.
//
// Tracks append through all.Audit.Append after completing their action. Caps
// records permission decisions separately and omits allowed reads. Activity
// text describes the completed action; a permission decision alone should not
// announce that work happened. The shared action vocabulary and text patterns
// follow boards 06 (spaces and people) and 12 (app settings and lifecycle):
//
//   - records.create: "@mona created Annual renewal in CRM". A bulk import uses
//     one line: "@santosh imported 300 deals into CRM from HubSpot".
//   - records.update: "@mona updated Annual renewal in CRM".
//   - records.delete: "@mona deleted Annual renewal from CRM".
//   - app.apply: "Mona shaped CRM · v12". View opens versions/v12.
//   - app.restore: "@mona restored CRM to v11 · v13". Restore makes a new version.
//   - role.set: "@mona made @raj a User of CRM".
//   - member.invite: "@mona invited @raj to acme".
//   - member.remove: "@mona removed @raj from acme".
//   - member.role: "@santosh made @mona an Admin of acme".
//   - trigger.pause: "@mona paused Deal quiet 7 days". A resumed trigger names
//     the same trigger with "resumed".
//   - approval.decide: "@mona approved Send revised quote" or "@mona declined
//     Send revised quote".
//   - app.move: "@santosh moved Dashboards to acme".
//   - app.archive: "@mona archived CRM".
//
// Activity Undo is data: a normal bridge method, app and params to call when a
// person chooses Undo. The audit package never executes it. The called service
// checks permissions and records the new action as another entry. Only include
// Undo when the owning service can perform it; View is an optional open target.
// Every committed Activity line is published on space/activity and, when it has
// an app, app/<id>/activity. The gateway must filter space/activity for each
// subscriber using their current app roles, just as Activity does for queries.
package audit
