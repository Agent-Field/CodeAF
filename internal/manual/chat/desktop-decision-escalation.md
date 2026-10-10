# Desktop decision escalation

## Why does a place pass a question to a parent?

The engine's decision-routing helper starts with the chat's active place. A
place in learning mode or below its confidence threshold passes the question
up the place graph. Nearer parents are considered first, with the first stored
parent winning a tie. A place can decide only in deciding mode and at or above
its effective confidence threshold. The helper uses the existing place settings
and the caller's real confidence evidence; it generates no answers itself.

Routing stops after three parent hops from the chat's original places. Each
place is considered once, so shared parents and cycles cannot cause a loop.
When no place can decide, the result is that the person must answer. Always ask
or ask mode also sends the question to the person.

This is an engine routing helper. It does not by itself connect desktop question
cards to automatic answers, write receipts, or change a place's learning mode.

## Which place decides when my chat is in two places?

The engine's decision-routing helper starts at the nearest common ancestor of
all the chat's active places. It minimises the furthest distance from any of
those places, then the total distance. A tie follows the first membership's
parent order. If that ancestor cannot decide, routing continues through its
parents within the original three-hop limit. No shared ancestor within that
limit means the person must answer. An unplaced chat also goes to the person.

Archived and missing memberships do not decide. Archived ancestors are skipped
as decision owners but their parents can still be reached. Missing confidence
or an invalid mode never authorises a decision; an evidence lookup error is
returned to the caller without selecting a place. This helper requires the
question integration caller to supply evidence and deliver the answer.
