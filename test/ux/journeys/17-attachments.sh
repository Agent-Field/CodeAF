#!/usr/bin/env bash
# J17 · Attach things (cheap variant)
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J17 Attach things — a mentioned file is copied at mention (CAS) and survives the source changing'

# The composer captures path-shaped tokens with known media extensions and
# turns them into chips, so the cheap variant is a real (tiny) PNG.
img="$UX_RUN/dot.png"
python3 - "$img" <<'PY'
import base64, sys
# 1x1 PNG, the smallest honest image there is.
sys.stdout.buffer  # noqa
open(sys.argv[1], "wb").write(base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="))
PY
record 'file mentioned' "$img ($(wc -c < "$img" | tr -d ' ') bytes)"

before_cas="$(find "$UX_STATE/cas" -type f 2>/dev/null | wc -l | tr -d ' ')"
record 'CAS objects before' "$before_cas"

expect_nodes 1
since="$(mark)"
tmux send-keys -t "$UX_SESSION" -l -- "tell me in one word what colour this image is: $img"
sleep 2
snap chip
press Enter
note "said: tell me in one word what colour this image is: $img"

sleep 25
snap answered

assert_journal "select count(*) from messages where seq > $since and role='user' and attachments != '[]'" \
  'the mention is journaled as an attachment on the user message' 60
record 'attachments recorded' "$(journal "select attachments from messages where seq > $since and role='user' order by seq limit 1")"

after_cas="$(find "$UX_STATE/cas" -type f 2>/dev/null | wc -l | tr -d ' ')"
record 'CAS objects after' "$after_cas"
if [ "$after_cas" -gt "$before_cas" ]; then
  _check yes 'the file was copied into the CAS at mention time' \
    'a new content-addressed object' "$((after_cas - before_cas)) new object(s)"
else
  _check no 'the file was copied into the CAS at mention time' \
    'a new content-addressed object' "still $after_cas objects"
fi

# Survives the source changing: the copy is content, not a pointer.
rm -f "$img"
still="$(find "$UX_STATE/cas" -type f 2>/dev/null | wc -l | tr -d ' ')"
[ "$still" = "$after_cas" ] \
  && _check yes 'the copy outlives its source' 'CAS unchanged after deleting the original' "$still objects" \
  || _check no 'the copy outlives its source' 'CAS unchanged after deleting the original' "$still objects"

assert_journal "select count(*) from messages where seq > $since and role in ('agent','system')" \
  'the mention got a reply, not silence'

dump_turn "$since"
finish
