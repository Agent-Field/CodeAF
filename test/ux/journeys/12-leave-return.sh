#!/usr/bin/env bash
# J12 · Leave and return
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J12 Leave and return — close the terminal, come back: the same thread resumes and a brief covers what landed'

expect_nodes 0

before_messages="$(journal "select count(*) from messages")"
record 'messages before leaving' "$before_messages"
snap before-leaving

# Leaving means the window dies. The store does not.
ux_kill "$UX_SESSION"
sleep 5

assert_cmd 'the journal survived the terminal closing' test -f "$UX_DB"
since="$(mark)"

ux_launch "$UX_SESSION"
sleep 6
snap returned

assert_screen 'aforge' 'aforge came back up' 60

# The one thing that must be true on return: the conversation is still there.
assert_screen 'heron|haiku|river|stretch|water|learned' \
  'the prior conversation is on screen — the thread resumed, it did not restart' 45

assert_journal_is "select count(*) from messages where seq <= $since" "$before_messages" \
  'no prior message was dropped by the restart'

session_now="$(journal "select session_id from messages where session_id != '' order by seq desc limit 1")"
sessions="$(journal "select count(distinct session_id) from messages where session_id != ''")"
record 'session id in use' "$session_now"
record 'distinct sessions in the journal' "$sessions"

# Resumption is either the same session id continuing, or a new one attached
# with a brief. Both are the journey; the report says which happened.
say "are you still there?"
sleep 10
after_session="$(journal "select session_id from messages where role='user' order by seq desc limit 1")"
record 'session after returning' "$after_session"

if [ "$after_session" = "$session_now" ]; then
  _check yes 'the same session resumed' 'session id unchanged across the restart' "$after_session"
else
  brief="$(journal "select count(*) from messages where seq > $since and body like 'While you were away%'")"
  if [ "${brief:-0}" != "0" ]; then
    _check yes 'a new session attached with an arrival brief' 'a "While you were away" line' "$brief brief(s)"
  else
    _check no 'the return either resumes the session or brings a brief' \
      'same session id, or a "While you were away" brief' "new session $after_session with no brief"
  fi
fi

assert_journal "select count(*) from messages where seq > $since and role='agent'" \
  'the returned window answers — the brain came back, not just the frame' 120
dump_turn "$since"
finish
