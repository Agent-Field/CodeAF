#!/usr/bin/env python3
"""Drive the real `codeaf chat` TUI inside one tmux server, on whichever machine this runs on.

It runs ON the machine whose screen it reads, so a keystroke and the poll that watches its effect share
one clock and one process: no ssh hop sits inside a measured interval. Every timestamp is that
machine's own wall clock in ms; the orchestrator converts B's to A's with a measured offset.
Adapted from scripts/measure/bstep.py, which reads the same home screen, with two changes: a take
works from a chat that is running elsewhere (the card says "running on <machine>"), and the same file
serves both machines (VD_TMUX names the tmux server).

  tuidrive.py cap                      print the pane
  tuidrive.py home                     go to the home screen; JSON rows
  tuidrive.py rows                     JSON list of the home sessions rows
  tuidrive.py send <prompt> [secs]     type a prompt, wait for the chat to go busy then idle; JSON timeline
  tuidrive.py take [hint]              take the first remote row (or the one naming hint); JSON timeline
  tuidrive.py open-local               open this machine's own first chat from home
  tuidrive.py watch-new <secs> <ms>    JSONL: the instant each new row appears on the home screen
"""
import json
import os
import re
import subprocess
import sys
import time

SESS = os.environ.get("VD_TMUX", "vdb")
ANSI = re.compile(r"\x1b\[[0-9;]*m")


def now_ms():
    return int(time.time() * 1000)


def tmux(*args):
    return subprocess.run(["tmux", "-L", SESS, *args], capture_output=True, text=True).stdout


def cap(colour=False):
    return tmux("capture-pane", "-p", *(["-e"] if colour else []), "-t", SESS)


def key(*keys):
    tmux("send-keys", "-t", SESS, *keys)


def plain(line):
    return ANSI.sub("", line)


def wait_for(pred, timeout=20.0, step=0.015):
    end = time.time() + timeout
    while time.time() < end:
        text = cap()
        if pred(text):
            return now_ms(), text
        time.sleep(step)
    return None, cap()


def on_home(text):
    return any(l.strip().startswith("sessions") for l in text.splitlines())


def home_rows(text):
    """The text of each row under the `sessions` header, up to the next section."""
    out, seen = [], False
    for raw in text.splitlines():
        p = plain(raw).rstrip()
        if p.strip().startswith("sessions"):
            seen = True
            continue
        if not seen:
            continue
        if not p.strip() or p.lstrip().startswith("─") or "type to search" in p or p.strip().startswith(("spend", "projects")):
            break
        out.append(p)
    return out


def is_remote(row):
    """A chat on this machine carries a dim bullet before its name; a chat on another machine has none.
    The status sentence cannot tell them apart, because a long name is cut before the sentence is drawn."""
    return not row.lstrip().startswith("·")


def go_home():
    """Space-space is the footer's way home from a chat; at home it is not sent."""
    for _ in range(4):
        if on_home(cap()):
            return True
        key("Space", "Space")
        time.sleep(0.8)
    return on_home(cap())


def selected_row():
    """The cursor row's text, found by the background the sessions panel gives it."""
    for raw in cap(True).splitlines():
        p = plain(raw).strip()
        if "48;5;235m" in raw and p and not p.startswith("sessions"):
            return p
    return ""


def select(pred, limit=20):
    for direction in ("Down", "Up"):
        for _ in range(limit):
            sel = selected_row()
            if sel and pred(sel):
                return sel
            key(direction)
            time.sleep(0.12)
    return ""


# A chat that left a dev server running says `1 job working` in its footer for as long as the server lives, though the
# turn is over: the word `idle` never comes back, so a running job alone must not keep a finished turn from ending.
JOBS_ONLY = re.compile(r"\d+ jobs? +working$")


def status_idle(text):
    """The footer's right-hand word, `idle` once the chat waits for the next prompt (or only background jobs run)."""
    return any(l.startswith("─") and (l.rstrip(" ─").endswith("idle") or JOBS_ONLY.search(l.rstrip(" ─")))
               for l in map(plain, text.splitlines()))


def finished_after(text, token):
    """Whether the turn that began with a prompt naming token has ended: its `worked` line is on screen
    below the prompt and the footer says idle. The screen scrolls, so a count of lines would not do."""
    lines = [plain(l) for l in text.splitlines()]
    foot = next((i for i in range(len(lines) - 1, -1, -1) if lines[i].startswith("─ ")), len(lines))
    seen = [i for i, l in enumerate(lines[:foot]) if token in l]
    return bool(seen) and status_idle(text) and any("▸ worked" in l for l in lines[seen[-1]:foot])


def fresh_word(prompt, screen):
    """A word of the prompt that is not on screen yet, so the screen can tell this turn from the last."""
    words = [w for w in re.findall(r"[A-Za-z0-9][A-Za-z0-9._-]{4,}", prompt) if w not in screen]
    dashed = [w for w in words if "-" in w]   # the probes name themselves with a dashed word; prose words also sit in the footer tips
    return (dashed or words or [prompt[:30]])[-1]


def send(prompt, secs):
    token = fresh_word(prompt, cap())
    t = {"typed_ms": now_ms(), "token": token}
    key("-l", prompt)
    time.sleep(0.3)
    key("Enter")
    t["enter_ms"] = now_ms()
    t["idle_ms"], text = wait_for(lambda x: finished_after(x, token), secs)
    t["pane"] = text
    return t


def title_of(row):
    return re.sub(r"\s{2,}.*", "", row.strip()).strip("·▸ ")


def local_titles(text):
    return {title_of(r) for r in home_rows(text) if not is_remote(r)}


def chat_open(text):
    """A chat is open when the tab strip under the header names it with a close mark."""
    return "×" in "\n".join(text.splitlines()[:4]) and not on_home(text)


def take(hint):
    """Continue the first remote chat (or the one naming hint) here.

    The home screen opens the chat by itself the moment the take ends, and raises the resume card in
    it when the chat left something behind. So the take ends when the chat is on screen; the card is
    read there, and answered with esc (`not now`) so a later prompt is typed into the chat and not the card. When
    the chat does not open by itself, it is found again as the one local row that was not there before
    (a chat is renamed once its first turn is named) and opened with enter."""
    t = {}
    go_home()
    before = local_titles(cap())
    row = select(lambda s: is_remote(s) and (hint == "-" or hint.lower() in s.lower()))
    if not row:
        return {"error": "no remote row", "pane": cap()}
    t["row"] = row
    key("Enter")
    t["card_ms"], card = wait_for(lambda x: "Continue this chat here?" in x, 10)
    t["card"] = [plain(l).strip(" │") for l in card.splitlines() if "durable turn" in l]
    key("1")
    time.sleep(0.15)
    t["confirm_ms"] = now_ms()
    key("Enter")
    t["taken_ms"], pane = wait_for(lambda x: "Continue this chat here?" not in x and (chat_open(x) or bool(local_titles(x) - before)), 3600, 0.02)
    t["listed_ms"] = t["taken_ms"]   # the take is over: the chat is open, or listed on home
    t["auto_opened"] = chat_open(pane)
    if not chat_open(pane):
        new = sorted(local_titles(pane) - before)
        if not new:
            return dict(t, error="no new local row after take", pane_after_take=cap())
        t["row_after_take"] = new[0]
        select(lambda s: title_of(s) == new[0], limit=10)
        t["open_key_ms"] = now_ms()
        key("Enter")
        t["taken_ms"], pane = wait_for(chat_open, 120)
    t["open_ms"] = t["taken_ms"]
    t["first_screen"] = pane
    t["resume_card_ms"], shown = wait_for(lambda x: "not now" in x and "set up" in x, 4)
    t["resume_card"] = shown if t["resume_card_ms"] else ""
    t["ready_ms"], _ = wait_for(lambda x: "›" in x and "type to search" not in x, 60)
    if t["resume_card_ms"]:
        # The card's own `not now`. An escape sent in the instant the card is drawn is not read by it (a person cannot
        # type that fast), so the key is sent again until the card is gone.
        for _ in range(4):
            time.sleep(0.7)
            key("Escape")
            if wait_for(lambda x: "esc not now" not in x, 2)[0]:
                break
    t["screen_after"] = cap()
    return t


def open_local():
    """Open this machine's own first chat from the home screen (the `here` rows, not `new conversation`)."""
    go_home()
    row = select(lambda s: not is_remote(s) and "new conversation" not in s)
    if not row:
        return {"error": "no local chat row", "pane": cap()}
    t = {"row": row, "open_key_ms": now_ms()}
    key("Enter")
    t["open_ms"], first = wait_for(lambda x: "×" in "\n".join(x.splitlines()[:4]), 60)
    t["ready_ms"], _ = wait_for(lambda x: "›" in x and "type to search" not in x, 60)
    t["screen"] = cap()
    return t


def watch_new(seconds, step_ms):
    """Print the instant a title gains a row on the home screen. Titles repeat (two chats started from
    the same kind of prompt are named alike), so rows are counted, not looked up."""
    from collections import Counter
    end = time.time() + seconds
    last = None
    while time.time() < end:
        rows = home_rows(cap())
        now = Counter(title_of(r) for r in rows)
        if last is not None:
            for ttl, n in now.items():
                if ttl != "new conversation" and n > last.get(ttl, 0):
                    print(json.dumps({"ms": now_ms(), "new": ttl, "rows": len(rows)}), flush=True)
        last = now
        time.sleep(step_ms / 1000)


if __name__ == "__main__":
    cmd, args = sys.argv[1], sys.argv[2:]
    if cmd == "cap":
        print(cap())
    elif cmd == "home":
        print(json.dumps({"home": go_home(), "rows": home_rows(cap())}))
    elif cmd == "rows":
        print(json.dumps(home_rows(cap())))
    elif cmd == "send":
        print(json.dumps(send(args[0], float(args[1]) if len(args) > 1 else 180)))
    elif cmd == "take":
        print(json.dumps(take(args[0] if args else "-")))
    elif cmd == "open-local":
        print(json.dumps(open_local()))
    elif cmd == "watch-new":
        watch_new(float(args[0]), float(args[1]))
