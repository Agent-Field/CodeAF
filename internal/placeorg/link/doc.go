// Package link is the links-and-lenses stage. It gives the multi-home
// minority a shortcut into a second place, and it records views over a
// place's flat list. A view is never a place: this stage returns no op
// that creates a node.
//
// Extra homes are earned, not capped. A chat is scored against the
// finest places (the deepest nodes that survived, which are the leaf
// communities when any fine community survived), at its start anchor and,
// when SetEnd supplied end vectors, at its end anchor too. A place within
// Delta of that anchor's best and at or above Floor is earned. Homes are
// counted by distinct branch: leaves under one parent are one home. The
// place the chat lives in, its fit when it has none, and every ancestor of
// it are set aside. Each link records its score and the bar it cleared,
// which is what the homes invariant checks, and Homes gives the histogram. Floor
// and Delta come from State.Thresholds for the tree's space when that
// space was calibrated, and otherwise from the reference figures 0.45
// and 0.03.
//
// Views come from three places. A demoted group, handed in on Demoted
// because apply has already parked those chats and removed the node, becomes
// one view on its parent. A top-level place with at least monthPlaceChats
// chats gets a by-month view. An axis stored on State.Axes adds the views
// that axis names. The lens axis is carried on Op.Into, because an Op has
// no axis field of its own: "month", "group", "anchor" and "kind" are the
// spellings Lens.Axis documents. Op.Name is the view's name and Op.Members
// are the chats in it. A by-month view's months are computed from
// Chat.Created by whoever serves the view; this stage does not invent a
// month for a chat with no date.
package link
