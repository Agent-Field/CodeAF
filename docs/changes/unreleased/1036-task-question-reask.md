---
kind: fixed
title: a task-start question you answered is not asked again when you come back to the tab
pr: 1036
surface: [chat, engine]
invalidates:
  - "A turn's backlog was replayed VERBATIM to whoever attached next
    (session's [eventHub.attach]), cards included. A card is the one event in a
    turn that is not a report of something that happened but a QUESTION about
    something that has not, so a person who approved a task, looked at another
    tab and came back was asked to approve it again — on every return, for as
    long as that turn ran — and because a question takes the keyboard where it is
    drawn (#919), the conversation underneath it could not be scrolled while it
    stood. The replay now consults the answer: `eventHub.attach` is handed what
    is still being asked ([Agent.stillAskedLocked], the banked words read under
    a.mu at the moment of the attach) and leaves out a card whose question is no
    longer one. The BACKLOG is unchanged and is still a faithful log of the turn;
    what is filtered is the replay. `questionAsked` names the question a card
    put in front of somebody, in the lanes' own keys, and it covers consent, a
    task proposal, a standing card, a harness offer and a connect ask alike — so
    a permission granted mid-turn is not re-asked on a re-attach either."
  - "`TaskNotice.Withdrawn` was the ONLY outcome a proposal's card ever restated:
    a proposal that was ANSWERED said nothing on the turn's stream, and what the
    person's answer did lived only in the window that pressed the key.
    `TaskNotice.Decided` is the other ending, sent by [taskWait.decided] the
    moment anybody settles the proposal — the person, another window, or the
    countdown — from the one place both roads pass through. A card that states
    its own outcome is not asking anything, so the replay carries it in the open
    card's place and a window arriving late draws the assignment with the answer
    under it. The CLOCK's own wording (`approved · the clock`) belongs to the
    window that ran the clock and is not restated; a replayed card says
    `approved`, which is what is true afterwards."
  - "tui3 spelled the settled proposal's verdict inside [app.taskAnswered] and
    nowhere else. `taskVerdict` is that one reading of approved-and-redirect, and
    both the window whose key answered and the window replaying a decision take
    it; `taskAnswerWord` reads the answer's label off the proposal's own options
    rather than spelling one a second time."
---

The two symptoms the owner met were one defect. A card that will not leave the
conversation is also a card that owns the viewport: the question was re-raised by
the replay, nothing would ever close it because the engine had already recorded
the answer, and every key aimed at the conversation went to it instead.

Leaving the settled card out of the replay altogether was the first attempt and
was not enough — the surface reads a `propose_task` result with no card as a call
that was REFUSED ([app.refuseFormingCard]), so coming back said
`not started · the call was refused` over work that was running. The engine
states the outcome instead, and the replay has something true to carry.
