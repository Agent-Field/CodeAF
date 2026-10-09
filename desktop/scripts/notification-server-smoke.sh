#!/usr/bin/env bash
# A notification click against a real freedesktop notification server, not the
# stub in src-tauri/tests/notification_click_smoke.rs. It runs dunst on a private
# session bus and a virtual display, so it never touches the desktop you are
# sitting at: codeaf posts a grouped notification, dunst draws it, xdotool
# clicks the drawn notification with the pointer, and the test checks that the
# click opened the question still waiting. Screenshots before and after the
# click are written to OUT, which must be outside the source tree.
#
#     scripts/notification-server-smoke.sh /path/to/dunst /tmp/notify-evidence
#
# dunst only draws in X11 here. It proves codeaf against a real server's
# protocol and rendering; it is not GNOME Shell or KDE Plasma, and not Wayland.
set -euo pipefail

DUNST=${1:?usage: notification-server-smoke.sh DUNST OUT}
OUT=${2:?usage: notification-server-smoke.sh DUNST OUT}
HERE=$(cd "$(dirname "$0")/.." && pwd)
case "$(cd "$(dirname "$OUT")" 2>/dev/null && pwd)/" in
  "$HERE"/*) echo "OUT must be outside the source tree" >&2; exit 2 ;;
esac
mkdir -p "$OUT"

if [ -z "${CODEAF_SMOKE_INSIDE:-}" ]; then
  exec dbus-run-session -- xvfb-run -a -s "-screen 0 1280x800x24" \
    env CODEAF_SMOKE_INSIDE=1 "$0" "$DUNST" "$OUT"
fi

conf="$OUT/dunstrc"
cat >"$conf" <<'EOF'
[global]
    width = 420
    origin = top-right
    offset = 30x30
    font = Sans 11
    mouse_left_click = do_action, close_current
[urgency_normal]
    timeout = 0
EOF

"$DUNST" -conf "$conf" >"$OUT/dunst.log" 2>&1 &
dunst_pid=$!
trap 'kill "$dunst_pid" 2>/dev/null || true' EXIT
for _ in $(seq 50); do
  gdbus call --session --dest org.freedesktop.DBus --object-path /org/freedesktop/DBus \
    --method org.freedesktop.DBus.NameHasOwner org.freedesktop.Notifications 2>/dev/null | grep -q true && break
  sleep 0.1
done
gdbus call --session --dest org.freedesktop.Notifications --object-path /org/freedesktop/Notifications \
  --method org.freedesktop.Notifications.GetServerInformation | tee "$OUT/server.txt"

log="$OUT/smoke.log"
(cd "$HERE/src-tauri" && CODEAF_NOTIFY_SMOKE=real cargo test --test notification_click_smoke -- --nocapture) >"$log" 2>&1 &
smoke=$!
for _ in $(seq 600); do grep -q '^READY' "$log" && break; kill -0 "$smoke" 2>/dev/null || break; sleep 0.5; done
grep -q '^READY' "$log" || { cat "$log"; exit 1; }

# The window dunst draws the notification in, found by its class, clicked at its centre.
win=""
for _ in $(seq 40); do win=$(xdotool search --onlyvisible --class Dunst 2>/dev/null | head -1 || true); [ -n "$win" ] && break; sleep 0.25; done
[ -n "$win" ] || { echo "dunst drew no notification window" >&2; exit 1; }
import -window root "$OUT/before-click.png"
eval "$(xdotool getwindowgeometry --shell "$win")"
xdotool mousemove $((X + WIDTH / 2)) $((Y + HEIGHT / 2)) click 1
for _ in $(seq 120); do grep -q '^CLICKED' "$log" && break; kill -0 "$smoke" 2>/dev/null || break; sleep 0.25; done
grep -q '^CLICKED' "$log" && import -window root "$OUT/after-click.png"
wait "$smoke" && status=0 || status=$?
grep -E '^(PASS|FAIL|notification_click_smoke)' "$log"
exit "$status"
